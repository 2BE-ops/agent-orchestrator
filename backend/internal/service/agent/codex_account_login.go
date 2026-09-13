package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/shellterm"
)

func (m *codexAccountManager) openLoginTerminal(ctx context.Context, targetAccountID string) (CodexAccountLoginTerminalStart, error) {
	release, err := m.acquireAccountMutation(ctx)
	if err != nil {
		return CodexAccountLoginTerminalStart{}, err
	}
	defer release()
	if m.terminal == nil || m.executable == nil {
		return CodexAccountLoginTerminalStart{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex login terminal is unavailable")
	}
	targetAccountID = strings.TrimSpace(targetAccountID)
	_, deviceState, deviceErr := readCodexFileState(m.globalCredentialPath(), true)
	if deviceErr != nil {
		// Normal Add remains available when the device store cannot be inspected,
		// but it must never auto-activate based on an unsafe absence assumption.
		deviceState.exists = true
	}
	if targetAccountID != "" {
		record, ok := m.catalog.record(targetAccountID)
		if !ok || (record.Snapshot.Status != domain.CodexAccountStatusValid && record.Snapshot.Status != domain.CodexAccountStatusSignedOut) {
			return CodexAccountLoginTerminalStart{}, apierr.NotFound("CODEX_ACCOUNT_NOT_FOUND", "Codex account not found")
		}
	}
	id := m.newID()
	now := m.now()
	snapshot := domain.CodexAccountLoginOperation{OperationID: id, AccountID: targetAccountID, Status: domain.CodexAccountLoginPending, ReasonCode: domain.CodexAccountLoginReasonPending, Reason: "Waiting for Codex sign-in.", ExpiresAt: now.Add(codexAccountLoginLifetime)}
	m.mu.Lock()
	if m.login != nil && !terminalLoginStatus(m.login.snapshot.Status) {
		m.mu.Unlock()
		return CodexAccountLoginTerminalStart{}, apierr.Conflict("CODEX_ACCOUNT_LOGIN_IN_PROGRESS", "A Codex account login is already in progress", nil)
	}
	previous := m.login
	m.login = &accountLoginOperation{snapshot: snapshot, targetAccountID: targetAccountID, deviceState: deviceState}
	m.mu.Unlock()
	if previous != nil {
		m.cleanupLoginFiles(previous)
	}
	pendingDir, home, err := createPendingCredentialHome(m.pendingRoot, id)
	if err != nil {
		m.clearLoginReservation(id)
		return CodexAccountLoginTerminalStart{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex login could not be prepared")
	}
	executable, err := m.executable()
	if err != nil || strings.TrimSpace(executable) == "" {
		_ = os.RemoveAll(pendingDir)
		m.clearLoginReservation(id)
		return CodexAccountLoginTerminalStart{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex login terminal is unavailable")
	}
	title := "Add Codex account"
	if targetAccountID != "" {
		title = "Sign in to Codex account"
	}
	terminal, err := m.terminal.OpenCommandTerminal(ctx, shellterm.OpenCommandTerminalInput{Argv: []string{executable, "codex-login"}, Env: map[string]string{"CODEX_HOME": home}, WorkingDir: home, Title: title})
	if err != nil {
		_ = os.RemoveAll(pendingDir)
		m.clearLoginReservation(id)
		return CodexAccountLoginTerminalStart{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex login terminal could not be opened")
	}
	m.mu.Lock()
	if m.login == nil || m.login.snapshot.OperationID != id {
		m.mu.Unlock()
		_ = m.terminal.CloseShellTerminal(context.WithoutCancel(ctx), terminal.HandleID)
		_ = os.RemoveAll(pendingDir)
		return CodexAccountLoginTerminalStart{}, apierr.Conflict("CODEX_ACCOUNT_LOGIN_IN_PROGRESS", "A Codex account login changed concurrently", nil)
	}
	m.login.pendingDir, m.login.home, m.login.terminalHandle = pendingDir, home, terminal.HandleID
	m.login.terminalTitle, m.login.terminalCreated = terminal.Title, terminal.CreatedAt
	m.mu.Unlock()
	go m.expireLogin(m.ctx, id, snapshot.ExpiresAt)
	m.publish()
	return CodexAccountLoginTerminalStart{Operation: snapshot, ShellTerminal: terminal}, nil
}

func (m *codexAccountManager) clearLoginReservation(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.login != nil && m.login.snapshot.OperationID == id {
		m.login = nil
	}
}

func (m *codexAccountManager) cleanupLoginFiles(op *accountLoginOperation) {
	if op == nil {
		return
	}
	if op.terminalHandle != "" && m.terminal != nil {
		_ = m.terminal.CloseShellTerminal(context.Background(), op.terminalHandle)
	}
	if op.pendingDir != "" {
		_ = os.RemoveAll(op.pendingDir)
	}
}

func terminalLoginStatus(status domain.CodexAccountLoginStatus) bool {
	return status == domain.CodexAccountLoginCompleted || status == domain.CodexAccountLoginCancelled || status == domain.CodexAccountLoginExpired || status == domain.CodexAccountLoginFailed
}

func (m *codexAccountManager) verifyLogin(ctx context.Context, operationID string) (domain.CodexAccountLoginOperation, error) {
	m.mu.Lock()
	op := m.login
	if op == nil || op.snapshot.OperationID != operationID {
		m.mu.Unlock()
		return domain.CodexAccountLoginOperation{}, apierr.NotFound("CODEX_ACCOUNT_LOGIN_NOT_FOUND", "Codex account login operation not found")
	}
	if terminalLoginStatus(op.snapshot.Status) {
		result := op.snapshot
		m.mu.Unlock()
		return result, nil
	}
	if op.closing || op.committing {
		result := op.snapshot
		m.mu.Unlock()
		return result, nil
	}
	if op.snapshot.Status == domain.CodexAccountLoginVerifying {
		result := op.snapshot
		m.mu.Unlock()
		return result, nil
	}
	op.snapshot.Status = domain.CodexAccountLoginVerifying
	op.snapshot.Reason = "Verifying the Codex account."
	home, pendingDir, terminalHandle := op.home, op.pendingDir, op.terminalHandle
	m.mu.Unlock()
	m.publish()
	pendingPath := filepath.Join(home, codexCredentialFilename)
	pendingCredential, admitted, credentialErr := readCodexFileState(pendingPath, false)
	if credentialErr != nil {
		return m.finishLogin(operationID, domain.CodexAccountLoginUnauthorized, domain.CodexAccountLoginReasonUnauthorized, "Codex is still signed out.", nil), nil
	}
	identity, identityErr := parseCodexCredentialIdentity(pendingCredential)
	latestCredential, latest, latestErr := readCodexFileState(pendingPath, false)
	if identityErr != nil || latestErr != nil || !sameCodexFileState(admitted, latest) || !bytes.Equal(pendingCredential, latestCredential) {
		return m.finishLoginRetryable(operationID), nil
	}
	observation := ports.CodexAccountObservation{Authentication: domain.AgentAuthenticationUnknown, Method: identity.Method}
	exclusive, exclusiveErr := m.acquireGlobalMutation(ctx)
	if exclusiveErr != nil {
		return domain.CodexAccountLoginOperation{}, exclusiveErr
	}
	if exclusive != nil {
		defer exclusive.Release()
	}
	releaseMutation, mutationErr := m.acquireAccountMutation(ctx)
	if mutationErr != nil {
		return domain.CodexAccountLoginOperation{}, mutationErr
	}
	defer releaseMutation()
	m.mu.Lock()
	op = m.login
	if op == nil || op.snapshot.OperationID != operationID || terminalLoginStatus(op.snapshot.Status) || op.closing {
		var result domain.CodexAccountLoginOperation
		if op != nil && op.snapshot.OperationID == operationID {
			result = op.snapshot
		}
		m.mu.Unlock()
		return result, nil
	}
	op.committing = true
	op.commitDone = make(chan struct{})
	commitDone := op.commitDone
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.login != nil && m.login.snapshot.OperationID == operationID && m.login.commitDone == commitDone {
			m.login.committing = false
			close(commitDone)
			m.login.commitDone = nil
		}
		m.mu.Unlock()
	}()
	targetAccountID := op.targetAccountID
	var record codexAccountRecord
	if targetAccountID != "" {
		target, targetFound := m.catalog.record(targetAccountID)
		if !targetFound || !loginCredentialIdentifiesRecord(target, identity, pendingCredential) {
			return m.finishLogin(operationID, domain.CodexAccountLoginFailed, domain.CodexAccountLoginReasonFailed, "Sign in with the same Codex account to replace its credentials.", nil), nil
		}
		credential := pendingCredential
		m.mu.Lock()
		active := m.active
		m.mu.Unlock()
		if active.AccountID == targetAccountID {
			expectedGlobal, verifyErr := m.activeAccountCredentialLocked(target)
			if verifyErr != nil {
				reason := "The device Codex account could not be confirmed. Try again."
				if errors.Is(verifyErr, ports.ErrCodexGlobalAccountChanged) {
					reason = "The device Codex account changed. Try again."
				}
				return m.finishLogin(operationID, domain.CodexAccountLoginFailed, domain.CodexAccountLoginReasonFailed, reason, nil), nil
			}
			if _, activateErr := m.activateFromCredentialLocked(m.ctx, targetAccountID, active.Revision, filepath.Join(home, codexCredentialFilename), expectedGlobal); activateErr != nil {
				return m.finishLogin(operationID, domain.CodexAccountLoginFailed, domain.CodexAccountLoginReasonFailed, "The account was verified but could not be activated.", nil), nil
			}
			record, _ = m.catalog.record(targetAccountID)
		} else {
			var replaceErr error
			record, replaceErr = m.catalog.replaceCredential(targetAccountID, credential, observation)
			if replaceErr != nil {
				return m.finishLogin(operationID, domain.CodexAccountLoginFailed, domain.CodexAccountLoginReasonFailed, "The verified Codex account could not be saved.", nil), nil
			}
		}
		_ = os.RemoveAll(pendingDir)
		m.clearReauthenticationRequired(targetAccountID)
		m.catalog.updateSnapshot(targetAccountID, func(snapshot *domain.CodexAccountSnapshot) {
			snapshot.Authentication = uncheckedAuthentication()
			if observation.Method != domain.CodexAuthMethodUnknown {
				snapshot.AuthMethod = observation.Method
			}
			if observation.Email != nil {
				snapshot.AccountEmail = observation.Email
			}
			snapshot.Label = accountLabel(snapshot.ID, snapshot.AuthMethod, snapshot.AccountEmail)
		})
	} else {
		activationCredentialPath := filepath.Join(home, codexCredentialFilename)
		if existing, found := m.matchCredentialAccount(pendingCredential, identity); found {
			var replaceErr error
			record, replaceErr = m.catalog.replaceCredential(existing.Snapshot.ID, pendingCredential, observation)
			if replaceErr != nil {
				return m.finishLogin(operationID, domain.CodexAccountLoginFailed, domain.CodexAccountLoginReasonFailed, "The verified Codex account could not be saved.", nil), nil
			}
			_ = os.RemoveAll(pendingDir)
			m.clearReauthenticationRequired(existing.Snapshot.ID)
			activationCredentialPath = filepath.Join(record.Home, codexCredentialFilename)
		} else {
			var err error
			record, err = m.catalog.commitPending(pendingDir, observation)
			if err != nil {
				return m.finishLogin(operationID, domain.CodexAccountLoginFailed, domain.CodexAccountLoginReasonFailed, "The verified Codex account could not be saved.", nil), nil
			}
			activationCredentialPath = filepath.Join(record.Home, codexCredentialFilename)
		}
		m.catalog.updateSnapshot(record.Snapshot.ID, func(s *domain.CodexAccountSnapshot) {
			s.Authentication = uncheckedAuthentication()
			if observation.Method != domain.CodexAuthMethodUnknown {
				s.AuthMethod = observation.Method
			}
			if observation.Email != nil {
				s.AccountEmail = observation.Email
			}
			s.Label = accountLabel(s.ID, s.AuthMethod, s.AccountEmail)
		})
		m.mu.Lock()
		activateFirst := !op.deviceState.exists
		expectedRevision := m.active.Revision
		m.mu.Unlock()
		if activateFirst {
			// First-account convenience is only safe while the device store is
			// still empty. The empty expected value is an explicit compare-and-
			// swap guard, so an external login that appeared during the terminal
			// flow is never overwritten.
			_, activationErr := m.activateFromCredentialLocked(m.ctx, record.Snapshot.ID, expectedRevision, activationCredentialPath, []byte{})
			if errors.Is(activationErr, ports.ErrCodexGlobalAccountChanged) {
				// An external login appeared after Add started. The account is still
				// safely saved; leave the device untouched and let reconciliation
				// project the newly authoritative device account.
				m.requestGlobalReconciliationIfNeeded()
				activateFirst = false
			} else if activationErr != nil {
				result := m.finishLogin(operationID, domain.CodexAccountLoginFailed, domain.CodexAccountLoginReasonFailed, "The account was saved but could not be activated.", &record.Snapshot)
				if m.terminal != nil && terminalHandle != "" {
					_ = m.terminal.CloseShellTerminal(context.WithoutCancel(ctx), terminalHandle)
				}
				return result, nil
			}
			if activateFirst {
				m.setManagedGlobal(record.Snapshot.ID)
			}
		}
	}
	m.invalidateCredentialEvidence(record.Snapshot.ID)
	latestRecord, _ := m.catalog.record(record.Snapshot.ID)
	snapshot := latestRecord.Snapshot
	snapshot.Active = snapshot.ID == m.activeAccountID()
	snapshot.Capacity = m.capacity.snapshot(snapshot.ID)
	reason := "Codex account added."
	if targetAccountID != "" {
		reason = "Codex account signed in."
	}
	result := m.finishLogin(operationID, domain.CodexAccountLoginCompleted, domain.CodexAccountLoginReasonCompleted, reason, &snapshot)
	if m.terminal != nil && terminalHandle != "" {
		_ = m.terminal.CloseShellTerminal(context.WithoutCancel(ctx), terminalHandle)
	}
	return result, nil
}

// activeAccountCredentialLocked captures the exact device bytes an active
// re-login may replace. Identity is established locally and provider failures
// cannot block the commit.
func (m *codexAccountManager) activeAccountCredentialLocked(record codexAccountRecord) ([]byte, error) {
	globalPath := m.globalCredentialPath()
	credential, admitted, err := readCodexFileState(globalPath, false)
	if err != nil {
		return nil, err
	}
	latestCredential, latest, latestErr := readCodexFileState(globalPath, false)
	if latestErr != nil {
		return nil, latestErr
	}
	if !sameCodexFileState(admitted, latest) || !bytes.Equal(credential, latestCredential) {
		return nil, ports.ErrCodexGlobalAccountChanged
	}
	identity, identityErr := parseCodexCredentialIdentity(latestCredential)
	matched, match := m.matchGlobalCredentialForReconciliation(latestCredential, identity, identityErr)
	if match != codexCredentialMatchManaged || matched.Snapshot.ID != record.Snapshot.ID {
		return nil, ports.ErrCodexGlobalAccountChanged
	}
	return latestCredential, nil
}

func loginCredentialIdentifiesRecord(record codexAccountRecord, identity codexCredentialIdentity, credential []byte) bool {
	if record.ProviderAccountID != "" {
		return identity.Method == domain.CodexAuthMethodChatGPT && identity.ProviderAccountID == record.ProviderAccountID
	}
	if identity.Method == domain.CodexAuthMethodAPIKey {
		return record.Snapshot.AuthMethod == domain.CodexAuthMethodAPIKey
	}
	return localCredentialIdentifiesRecord(record, credential)
}

func (m *codexAccountManager) matchCredentialAccount(credential []byte, identity codexCredentialIdentity) (codexAccountRecord, bool) {
	record, match := m.matchGlobalCredentialForReconciliation(credential, identity, nil)
	if match == codexCredentialMatchManaged {
		return record, true
	}
	return codexAccountRecord{}, false
}

func (m *codexAccountManager) finishLoginRetryable(id string) domain.CodexAccountLoginOperation {
	return m.finishLogin(id, domain.CodexAccountLoginRetryable, domain.CodexAccountLoginReasonFailed, "The Codex credential is not ready yet. Try again.", nil)
}
func (m *codexAccountManager) finishLogin(id string, status domain.CodexAccountLoginStatus, code, reason string, account *domain.CodexAccountSnapshot) domain.CodexAccountLoginOperation {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.login == nil || m.login.snapshot.OperationID != id {
		return domain.CodexAccountLoginOperation{}
	}
	if m.login.closing && status != domain.CodexAccountLoginCancelled && status != domain.CodexAccountLoginExpired {
		return m.login.snapshot
	}
	if terminalLoginStatus(m.login.snapshot.Status) && m.login.snapshot.Status != status {
		return m.login.snapshot
	}
	m.login.snapshot.Status, m.login.snapshot.ReasonCode, m.login.snapshot.Reason, m.login.snapshot.Account = status, code, reason, account
	if account != nil {
		m.login.snapshot.AccountID = account.ID
	}
	result := m.login.snapshot
	go m.publish()
	return result
}

func (m *codexAccountManager) cancelLogin(ctx context.Context, operationID string) (domain.CodexAccountLoginOperation, error) {
	for {
		m.mu.Lock()
		op := m.login
		if op == nil || op.snapshot.OperationID != operationID {
			m.mu.Unlock()
			return domain.CodexAccountLoginOperation{}, apierr.NotFound("CODEX_ACCOUNT_LOGIN_NOT_FOUND", "Codex account login operation not found")
		}
		if terminalLoginStatus(op.snapshot.Status) {
			result := op.snapshot
			m.mu.Unlock()
			return result, nil
		}
		if op.committing && op.commitDone != nil {
			done := op.commitDone
			m.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return domain.CodexAccountLoginOperation{}, ctx.Err()
			}
		}
		if op.closing {
			result := op.snapshot
			m.mu.Unlock()
			return result, nil
		}
		op.closing = true
		handle, pending := op.terminalHandle, op.pendingDir
		m.mu.Unlock()
		if handle != "" && m.terminal != nil {
			if err := m.terminal.CloseShellTerminal(ctx, handle); err != nil {
				m.mu.Lock()
				if m.login != nil && m.login.snapshot.OperationID == operationID {
					m.login.closing = false
					m.login.snapshot.Status = domain.CodexAccountLoginRetryable
					m.login.snapshot.ReasonCode = domain.CodexAccountLoginReasonFailed
					m.login.snapshot.Reason = "Codex login terminal could not be closed."
				}
				m.mu.Unlock()
				m.publish()
				return domain.CodexAccountLoginOperation{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex login terminal could not be closed")
			}
		}
		_ = os.RemoveAll(pending)
		return m.finishLogin(operationID, domain.CodexAccountLoginCancelled, domain.CodexAccountLoginReasonCancelled, "Codex account login was cancelled.", nil), nil
	}
}

func (m *codexAccountManager) expireLogin(ctx context.Context, id string, at time.Time) {
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-m.ctx.Done():
		return
	}
	for {
		m.mu.Lock()
		op := m.login
		if op == nil || op.snapshot.OperationID != id || terminalLoginStatus(op.snapshot.Status) || op.closing {
			m.mu.Unlock()
			return
		}
		if op.committing && op.commitDone != nil {
			done := op.commitDone
			m.mu.Unlock()
			select {
			case <-done:
				continue
			case <-m.ctx.Done():
				return
			}
		}
		op.closing = true
		pending, handle := op.pendingDir, op.terminalHandle
		m.mu.Unlock()
		if pending == "" {
			return
		}
		if handle != "" && m.terminal != nil {
			_ = m.terminal.CloseShellTerminal(ctx, handle)
		}
		_ = os.RemoveAll(pending)
		m.finishLogin(id, domain.CodexAccountLoginExpired, domain.CodexAccountLoginReasonExpired, "Codex account login expired.", nil)
		return
	}
}
