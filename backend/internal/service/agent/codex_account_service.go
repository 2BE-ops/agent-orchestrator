package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Service integration.
func (s *Service) structuredCodexAuthentication(ctx context.Context, agentID string, purpose domain.AgentReadinessPurpose) (domain.AgentAuthenticationObservation, bool) {
	if agentID != string(domain.HarnessCodex) || s.codexAccounts == nil || s.codexAccounts.factory == nil {
		return domain.AgentAuthenticationObservation{}, false
	}
	if err := s.WaitCodexAccountStoreReady(ctx); err != nil {
		// Account management is optional for ordinary Codex use. When AO's
		// local account store cannot answer safely, fall back to the native
		// readiness path instead of treating that as proof Codex is signed out.
		return domain.AgentAuthenticationObservation{}, false
	}
	if purpose == domain.AgentReadinessPurposeLaunch {
		s.codexAccounts.mu.Lock()
		verified := s.codexAccounts.reconciliation.Status == domain.CodexDeviceReconciliationVerified
		s.codexAccounts.mu.Unlock()
		if !verified {
			return domain.AgentAuthenticationObservation{}, false
		}
	}
	id := s.codexAccounts.activeAccountID()
	if id == "" {
		s.codexAccounts.mu.Lock()
		reconciled := s.codexAccounts.reconciliation.Status == domain.CodexDeviceReconciliationVerified
		credentialPresent := s.codexAccounts.deviceCredentialPresent
		s.codexAccounts.mu.Unlock()
		if !reconciled || credentialPresent {
			return uncheckedAuthentication(), true
		}
		return successfulAuthentication(s.codexAccounts.now(), domain.AgentAuthenticationUnauthorized, domain.AgentReadinessReasonUnauthorized, "Sign in to Codex or add an account in Settings."), true
	}
	record, ok := s.codexAccounts.catalog.record(id)
	if !ok {
		return failedAuthentication(s.codexAccounts.now(), domain.AgentReadinessReasonAuthCheckInconclusive, "The active Codex account is unavailable."), true
	}
	result, err := s.codexAccounts.ensureAuthentication(ctx, record, purpose)
	if err != nil {
		return failedAuthentication(s.codexAccounts.now(), domain.AgentReadinessReasonAuthCheckFailed, "Authentication check failed."), true
	}
	record, ok = s.codexAccounts.catalog.record(id)
	if !ok {
		return domain.AgentAuthenticationObservation{}, false
	}
	if purpose == domain.AgentReadinessPurposeLaunch && result.State == domain.AgentAuthenticationAuthorized && record.Snapshot.AuthMethod == domain.CodexAuthMethodChatGPT {
		capabilities := s.codexAccounts.detectCapabilities(ctx)
		if capabilities.CapacityRead.State != domain.CodexCapabilitySupported {
			return domain.AgentAuthenticationObservation{}, false
		}
		// Launch readiness uses a protected provider call. account/read is only
		// local metadata discovery and cannot prove that the server accepts the
		// stored tokens.
		s.codexAccounts.capacity.invalidate(record.Snapshot.ID, false)
		_, capacityErr := s.codexAccounts.capacity.ensureOne(ctx, record, capabilities, true)
		if capacityErr != nil {
			return domain.AgentAuthenticationObservation{}, false
		}
		latest, ok := s.codexAccounts.catalog.record(record.Snapshot.ID)
		if !ok {
			return domain.AgentAuthenticationObservation{}, false
		}
		verified, reauthenticationRequired := s.codexAccounts.authenticationVerification(record.Snapshot.ID)
		if reauthenticationRequired {
			return latest.Snapshot.Authentication, true
		}
		if !verified {
			// Offline, timeout, and provider failures are not evidence that the
			// account is signed out. Let native launch readiness remain advisory.
			return domain.AgentAuthenticationObservation{}, false
		}
		return latest.Snapshot.Authentication, true
	}
	return result, true
}

// CachedCodexAccounts returns the current in-memory view without native work.
func (s *Service) CachedCodexAccounts(ctx context.Context) (CodexAccounts, error) {
	if err := ctx.Err(); err != nil {
		return CodexAccounts{}, err
	}
	if s.codexAccounts == nil {
		return CodexAccounts{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	if err := s.WaitCodexAccountStoreReady(ctx); err != nil {
		return CodexAccounts{}, err
	}
	result := s.codexAccounts.cached()
	if s.codexSwitches != nil {
		if sw, ok, err := s.codexSwitches.GetActiveCodexAccountSwitch(ctx); err == nil && ok {
			result.CurrentSwitch = &sw
		}
	}
	return result, nil
}

// EnsureCodexAccounts rediscovers requested accounts and refreshes eligible observations.
func (s *Service) EnsureCodexAccounts(ctx context.Context, ids []string, includeUsage, forceAuthentication, forceDeviceReconciliation bool) (CodexAccounts, error) {
	if s.codexAccounts == nil {
		return CodexAccounts{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	if s.codexSwitches != nil && s.codexSwitches.CodexAccountSwitchInProgress() {
		if sw, ok, err := s.codexSwitches.GetActiveCodexAccountSwitch(ctx); err == nil && ok {
			result := s.codexAccounts.cached()
			result.CurrentSwitch = &sw
			return result, nil
		}
		return s.codexAccounts.cached(), nil
	}
	if err := s.WaitCodexAccountStoreReady(ctx); err != nil {
		return CodexAccounts{}, err
	}
	// Reconciliation is local-only, so every Settings refresh can cheaply wait
	// for the current device credential to be associated before any Codex client
	// is opened. This prevents a recently removed or externally changed global
	// auth.json from being checked through the last-known active account slot.
	// A local reconciliation failure must not hide the saved catalog; the
	// response carries its safe, retryable reconciliation state instead.
	_ = s.codexAccounts.reconcileGlobalWithPolicy(ctx, forceDeviceReconciliation)
	installation, err := s.readiness.EnsureInstallation(ctx, []string{string(domain.HarnessCodex)}, domain.AgentReadinessPurposeDisplay)
	if err != nil {
		return CodexAccounts{}, err
	}
	result, err := s.codexAccounts.ensure(ctx, ids, includeUsage, forceAuthentication, installation[0].Installation.State)
	if err == nil && s.codexSwitches != nil {
		if sw, ok, switchErr := s.codexSwitches.GetActiveCodexAccountSwitch(ctx); switchErr == nil && ok {
			result.CurrentSwitch = &sw
		}
	}
	return result, err
}

// ConsumeCodexAccountResetCredit redeems one provider-reported usage-limit
// reset and returns the refreshed cached account view. The provider chooses the
// credit; opaque credit identifiers never cross the daemon boundary.
func (s *Service) ConsumeCodexAccountResetCredit(ctx context.Context, accountID, idempotencyKey string) (CodexAccounts, error) {
	if s.codexAccounts == nil {
		return CodexAccounts{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	if s.codexSwitches != nil && s.codexSwitches.CodexAccountSwitchInProgress() {
		return CodexAccounts{}, apierr.Conflict("CODEX_ACCOUNT_SWITCH_IN_PROGRESS", "Wait for the Codex account switch to finish before using a reset", nil)
	}
	if err := s.WaitCodexAccountStoreReady(ctx); err != nil {
		return CodexAccounts{}, err
	}
	if err := s.codexAccounts.consumeResetCredit(ctx, accountID, idempotencyKey); err != nil {
		return CodexAccounts{}, err
	}
	return s.CachedCodexAccounts(ctx)
}

// SubscribeCodexAccounts returns cached state followed by latest-wins updates.
func (s *Service) SubscribeCodexAccounts(ctx context.Context) (<-chan CodexAccounts, error) {
	if s.codexAccounts == nil {
		return nil, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	if err := s.WaitCodexAccountStoreReady(ctx); err != nil {
		return nil, err
	}
	source := s.codexAccounts.subscribe(ctx)
	out := make(chan CodexAccounts, 1)
	go func() {
		defer close(out)
		for snapshot := range source {
			if s.codexSwitches != nil {
				if sw, ok, err := s.codexSwitches.GetActiveCodexAccountSwitch(ctx); err == nil && ok {
					snapshot.CurrentSwitch = &sw
				}
			}
			select {
			case out <- snapshot:
			default:
				select {
				case <-out:
				default:
				}
				select {
				case out <- snapshot:
				default:
				}
			}
		}
	}()
	return out, nil
}

// PublishCodexAccounts notifies subscribers after externally owned switch changes.
func (s *Service) PublishCodexAccounts() {
	if s.codexAccounts != nil {
		s.codexAccounts.publish()
	}
}

// SetCodexAccountLoginTerminalOpener wires the trusted shell-terminal boundary.
func (s *Service) SetCodexAccountLoginTerminalOpener(opener codexAccountLoginTerminalService) {
	if s.codexAccounts != nil {
		s.codexAccounts.terminal = opener
	}
}

func (s *Service) prepareCodexAccountLogin(ctx context.Context) error {
	if s.codexAccounts == nil {
		return apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	if s.codexSwitches != nil && s.codexSwitches.CodexAccountSwitchInProgress() {
		return apierr.Conflict("CODEX_ACCOUNT_SWITCH_IN_PROGRESS", "A Codex account switch is already in progress", nil)
	}
	if err := s.WaitCodexAccountStoreReady(ctx); err != nil {
		return err
	}
	if err := s.requireCodexAccountInstallation(ctx); err != nil {
		return err
	}
	capabilities := s.codexAccounts.detectCapabilities(ctx)
	switch capabilities.NativeLogin.State {
	case domain.CodexCapabilityUnsupported:
		return apierr.NotImplemented("CODEX_ACCOUNT_MANAGEMENT_UNSUPPORTED", "This Codex version does not support account management")
	case domain.CodexCapabilityUnknown:
		return apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management capability could not be verified")
	default:
		return nil
	}
}

// OpenCodexAccountLoginTerminal starts one private native-login operation.
func (s *Service) OpenCodexAccountLoginTerminal(ctx context.Context) (CodexAccountLoginTerminalStart, error) {
	if err := s.prepareCodexAccountLogin(ctx); err != nil {
		return CodexAccountLoginTerminalStart{}, err
	}
	return s.codexAccounts.openLoginTerminal(ctx, "")
}

// OpenCodexAccountReauthenticationTerminal starts native sign-in for one
// retained account slot. A locally validated credential replaces that slot
// instead of creating a duplicate account.
func (s *Service) OpenCodexAccountReauthenticationTerminal(ctx context.Context, accountID string) (CodexAccountLoginTerminalStart, error) {
	if s.codexAccounts == nil {
		return CodexAccountLoginTerminalStart{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	if s.codexSwitches != nil && s.codexSwitches.CodexAccountSwitchInProgress() {
		return CodexAccountLoginTerminalStart{}, apierr.Conflict("CODEX_ACCOUNT_SWITCH_IN_PROGRESS", "A Codex account switch is already in progress", nil)
	}
	if err := s.WaitCodexAccountStoreReady(ctx); err != nil {
		return CodexAccountLoginTerminalStart{}, err
	}
	accountID = strings.TrimSpace(accountID)
	if s.codexAccounts.activeAccountID() == accountID {
		if err := s.EnsureCodexDeviceAccountReconciled(ctx); err != nil {
			return CodexAccountLoginTerminalStart{}, err
		}
	}
	if err := s.requireCodexAccountInstallation(ctx); err != nil {
		return CodexAccountLoginTerminalStart{}, err
	}
	capabilities := s.codexAccounts.detectCapabilities(ctx)
	if capabilities.NativeLogin.State != domain.CodexCapabilitySupported {
		return CodexAccountLoginTerminalStart{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management capability could not be verified")
	}
	return s.codexAccounts.openLoginTerminal(ctx, accountID)
}

// LogoutCodexAccount removes one AO-saved credential while retaining the
// account card. Active-account logout also clears the device-global file-backed
// credential after exact structured identity confirmation.
func (s *Service) LogoutCodexAccount(ctx context.Context, accountID string) (CodexAccounts, error) {
	if s.codexAccounts == nil || s.codexAccounts.factory == nil {
		return CodexAccounts{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	if s.codexSwitches != nil && s.codexSwitches.CodexAccountSwitchInProgress() {
		return CodexAccounts{}, apierr.Conflict("CODEX_ACCOUNT_SWITCH_IN_PROGRESS", "A Codex account switch is already in progress", nil)
	}
	if s.CodexAccountLoginInProgress() {
		return CodexAccounts{}, apierr.Conflict("CODEX_ACCOUNT_LOGIN_IN_PROGRESS", "Finish or close the Codex account login before logging out", nil)
	}
	if err := s.WaitCodexAccountStoreReady(ctx); err != nil {
		return CodexAccounts{}, err
	}
	accountID = strings.TrimSpace(accountID)
	if s.codexAccounts.activeAccountID() == accountID {
		if err := s.EnsureCodexDeviceAccountReconciled(ctx); err != nil {
			return CodexAccounts{}, err
		}
	}
	if err := s.codexAccounts.logout(ctx, accountID); err != nil {
		return CodexAccounts{}, err
	}
	s.readiness.Invalidate(string(domain.HarnessCodex), readinessInvalidateAuthentication)
	return s.CachedCodexAccounts(ctx)
}

// DeleteCodexAccount permanently removes one inactive signed-out account slot.
func (s *Service) DeleteCodexAccount(ctx context.Context, accountID string) (CodexAccounts, error) {
	if s.codexAccounts == nil {
		return CodexAccounts{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	if s.codexSwitches != nil && s.codexSwitches.CodexAccountSwitchInProgress() {
		return CodexAccounts{}, apierr.Conflict("CODEX_ACCOUNT_SWITCH_IN_PROGRESS", "A Codex account switch is already in progress", nil)
	}
	if s.CodexAccountLoginInProgress() {
		return CodexAccounts{}, apierr.Conflict("CODEX_ACCOUNT_LOGIN_IN_PROGRESS", "Finish or close the Codex account login before deleting an account", nil)
	}
	if err := s.WaitCodexAccountStoreReady(ctx); err != nil {
		return CodexAccounts{}, err
	}
	if err := s.codexAccounts.deleteAccount(ctx, strings.TrimSpace(accountID)); err != nil {
		return CodexAccounts{}, err
	}
	return s.CachedCodexAccounts(ctx)
}

// VerifyCodexAccountLogin completes a pending login once its local credential
// has been safely validated and committed.
func (s *Service) VerifyCodexAccountLogin(ctx context.Context, operationID string) (domain.CodexAccountLoginOperation, error) {
	if s.codexAccounts == nil {
		return domain.CodexAccountLoginOperation{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	result, err := s.codexAccounts.verifyLogin(ctx, strings.TrimSpace(operationID))
	if err == nil && result.Status == domain.CodexAccountLoginCompleted && result.Account != nil {
		if result.Account.Active && s.readiness != nil {
			s.readiness.Invalidate(string(domain.HarnessCodex), readinessInvalidateAuthentication)
		}
		// Credential commit is complete. Provider-backed authentication, capacity,
		// and usage warming happens independently and cannot roll the login back.
		accountID := result.Account.ID
		go func() {
			_, _ = s.EnsureCodexAccounts(s.codexAccounts.ctx, []string{accountID}, true, true, true)
		}()
	}
	return result, err
}

// CancelCodexAccountLogin destroys a pending login and its credential staging.
func (s *Service) CancelCodexAccountLogin(ctx context.Context, operationID string) (domain.CodexAccountLoginOperation, error) {
	if s.codexAccounts == nil {
		return domain.CodexAccountLoginOperation{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	return s.codexAccounts.cancelLogin(ctx, strings.TrimSpace(operationID))
}
func (s *Service) requireCodexAccountInstallation(ctx context.Context) error {
	observations, err := s.readiness.EnsureInstallation(ctx, []string{string(domain.HarnessCodex)}, domain.AgentReadinessPurposeDisplay)
	if err != nil {
		return err
	}
	if observations[0].Installation.State == domain.AgentInstallationNotInstalled && observations[0].Installation.Freshness == domain.AgentReadinessFresh {
		return apierr.NotImplemented("CODEX_ACCOUNT_MANAGEMENT_UNSUPPORTED", "Codex is not installed")
	}
	return nil
}

// InvalidateCodexAccountAuthentication invalidates the globally active account.
func (s *Service) InvalidateCodexAccountAuthentication() {
	if s.codexAccounts == nil {
		return
	}
	id := s.codexAccounts.activeAccountID()
	if id != "" {
		s.codexAccounts.invalidate(id)
	}
	s.readiness.Invalidate(string(domain.HarnessCodex), readinessInvalidateAuthentication)
	go func() { _ = s.codexAccounts.reconcileGlobal(s.codexAccounts.ctx) }()
}

// ObserveActiveCodexAccountCapacity attributes a provider event to the active account.
func (s *Service) ObserveActiveCodexAccountCapacity(observation ports.CodexCapacityObservation) {
	if s.codexAccounts == nil {
		return
	}
	id := s.codexAccounts.activeAccountID()
	if id != "" {
		s.codexAccounts.capacity.updateFromEvent(id, observation)
	}
}

// WarmCodexAccounts starts asynchronous local-store initialization, local
// device reconciliation, and then saved-account observation warming.
func (s *Service) WarmCodexAccounts() {
	if s.codexAccounts == nil {
		return
	}
	go func() {
		if err := s.codexAccounts.waitAccountStore(s.codexAccounts.ctx); err != nil {
			return
		}
		// Reconciliation is local-only and establishes the safe home for the device
		// account before any Codex process is opened for authentication or capacity.
		// A local reconciliation failure still leaves inactive saved accounts
		// eligible for their isolated checks below.
		_ = s.codexAccounts.reconcileGlobal(s.codexAccounts.ctx)
		capabilities := s.codexAccounts.detectCapabilities(s.codexAccounts.ctx)
		records, err := s.codexAccounts.catalog.recordsFor(nil)
		if err != nil {
			return
		}
		if capabilities.AccountRead.State == domain.CodexCapabilitySupported {
			for _, record := range records {
				if record.Snapshot.Status == domain.CodexAccountStatusValid {
					_, _ = s.codexAccounts.ensureAuthentication(s.codexAccounts.ctx, record, domain.AgentReadinessPurposeDisplay)
				}
			}
		}
		_ = s.codexAccounts.capacity.ensure(s.codexAccounts.ctx, records, capabilities, false)
	}()
}

// WaitCodexAccountStoreReady waits only for AO-owned local account state. It
// never starts Codex or inspects the device-global credential.
func (s *Service) WaitCodexAccountStoreReady(ctx context.Context) error {
	if s.codexAccounts == nil {
		return apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	err := s.codexAccounts.waitAccountStore(ctx)
	if err == nil {
		return nil
	}
	var failure *codexAccountStoreFailure
	if !errors.As(err, &failure) {
		return err
	}
	return apierr.New(apierr.KindUnavailable, "CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account setup did not complete", map[string]any{
		"reasonCode": failure.reason, "retryable": failure.retryable,
	})
}

// EnsureCodexDeviceAccountReconciled conclusively identifies the canonical
// device account before an operation is allowed to mutate it.
func (s *Service) EnsureCodexDeviceAccountReconciled(ctx context.Context) error {
	if err := s.WaitCodexAccountStoreReady(ctx); err != nil {
		return err
	}
	err := s.codexAccounts.reconcileGlobal(ctx)
	s.codexAccounts.mu.Lock()
	state := s.codexAccounts.reconciliation
	s.codexAccounts.mu.Unlock()
	if err == nil && state.Status == domain.CodexDeviceReconciliationVerified {
		return nil
	}
	reason, retryable := state.ReasonCode, state.Retryable
	if err != nil {
		failure := classifyDeviceReconciliationFailure(err)
		reason, retryable = failure.reason, failure.retryable
	}
	if strings.TrimSpace(reason) == "" {
		reason = "account_reconciliation_unavailable"
	}
	return apierr.New(apierr.KindUnavailable, "CODEX_DEVICE_ACCOUNT_UNVERIFIED", "The device Codex account could not be verified", map[string]any{
		"reasonCode": reason, "retryable": retryable,
	})
}

// BeginCodexAccountMutation gives Session Manager exclusive ownership of the
// credential mutation path for the complete lifetime of a global switch.
func (s *Service) BeginCodexAccountMutation(ctx context.Context) error {
	if s.codexAccounts == nil {
		return apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	_, err := s.codexAccounts.acquireAccountMutation(ctx)
	return err
}

// EndCodexAccountMutation releases the switch-owned credential mutation path.
func (s *Service) EndCodexAccountMutation() {
	if s.codexAccounts == nil {
		return
	}
	select {
	case s.codexAccounts.mutations <- struct{}{}:
	default:
	}
}

// CurrentCodexActiveAccount returns the current durable active-account pointer.
func (s *Service) CurrentCodexActiveAccount() domain.CodexActiveAccount {
	if s.codexAccounts == nil {
		return domain.CodexActiveAccount{}
	}
	s.codexAccounts.mu.Lock()
	defer s.codexAccounts.mu.Unlock()
	return s.codexAccounts.active
}

// CurrentCodexAccountSwitchSource reports what currently occupies the device
// credential store without treating the durable pointer as proof.
func (s *Service) CurrentCodexAccountSwitchSource() domain.CodexAccountSwitchSource {
	if s.codexAccounts == nil {
		return domain.CodexAccountSwitchSource{Kind: domain.CodexAccountSwitchSourceNone}
	}
	s.codexAccounts.mu.Lock()
	defer s.codexAccounts.mu.Unlock()
	source := domain.CodexAccountSwitchSource{Kind: domain.CodexAccountSwitchSourceNone, Revision: s.codexAccounts.active.Revision}
	if s.codexAccounts.reconciliation.ActiveAccountVerified && s.codexAccounts.active.AccountID != "" {
		source.Kind = domain.CodexAccountSwitchSourceManaged
		source.AccountID = s.codexAccounts.active.AccountID
	} else if s.codexAccounts.deviceCredentialPresent {
		source.Kind = domain.CodexAccountSwitchSourceDevice
	}
	return source
}

// CodexAccountLoginInProgress reports whether a native login is still open.
func (s *Service) CodexAccountLoginInProgress() bool {
	if s.codexAccounts == nil {
		return false
	}
	s.codexAccounts.mu.Lock()
	defer s.codexAccounts.mu.Unlock()
	return s.codexAccounts.login != nil && !terminalLoginStatus(s.codexAccounts.login.snapshot.Status)
}

// PrepareCodexAccountForSwitch validates an inactive target using only its
// private credential file. Authentication and capacity are display concerns
// and must never decide whether a local credential can be installed.
func (s *Service) PrepareCodexAccountForSwitch(ctx context.Context, accountID string) error {
	if s.codexAccounts == nil {
		return apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	if s.CodexAccountLoginInProgress() {
		return apierr.Conflict("CODEX_ACCOUNT_LOGIN_IN_PROGRESS", "Finish or close the Codex account login before switching accounts", nil)
	}
	if err := s.codexAccounts.validateGlobalCredentialStore(); err != nil {
		return apierr.NotImplemented("CODEX_GLOBAL_CREDENTIAL_STORE_UNSUPPORTED", "Device-global Codex account switching requires file-backed credentials")
	}
	record, ok := s.codexAccounts.catalog.record(strings.TrimSpace(accountID))
	if !ok || record.Snapshot.Status != domain.CodexAccountStatusValid {
		return apierr.NotFound("CODEX_ACCOUNT_NOT_FOUND", "Codex account not found")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	credentialPath := filepath.Join(record.Home, codexCredentialFilename)
	credential, admitted, credentialErr := readCodexFileState(credentialPath, false)
	if credentialErr != nil {
		s.codexAccounts.requireReauthentication(record.Snapshot.ID)
		return apierr.Conflict("CODEX_ACCOUNT_REAUTHENTICATION_REQUIRED", "Sign in again before switching to this Codex account", nil)
	}
	latestCredential, latest, latestErr := readCodexFileState(credentialPath, false)
	if latestErr != nil || !sameCodexFileState(admitted, latest) || !bytes.Equal(credential, latestCredential) {
		return apierr.Conflict("CODEX_ACCOUNT_IDENTITY_CHANGED", "The Codex account changed while preparing the switch. Try again", nil)
	}
	if !localCredentialIdentifiesRecord(record, latestCredential) {
		return apierr.Conflict("CODEX_ACCOUNT_IDENTITY_CHANGED", "The saved Codex account no longer matches its credential. Sign in again", nil)
	}
	_ = s.codexAccounts.catalog.updateCredentialIdentity(record.Snapshot.ID, latestCredential)
	return nil
}

// ConfirmCodexAccountSwitchTarget confirms that the current device credential
// belongs to the target and commits the active pointer. It is local and safe
// while the provider is unavailable.
func (s *Service) ConfirmCodexAccountSwitchTarget(ctx context.Context, accountID string) error {
	if s.codexAccounts == nil {
		return apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable")
	}
	accountID = strings.TrimSpace(accountID)
	record, ok := s.codexAccounts.catalog.record(accountID)
	if !ok || record.Snapshot.Status != domain.CodexAccountStatusValid {
		return apierr.NotFound("CODEX_ACCOUNT_NOT_FOUND", "Codex account not found")
	}
	if err := s.codexAccounts.validateGlobalCredentialStore(); err != nil {
		return apierr.NotImplemented("CODEX_GLOBAL_CREDENTIAL_STORE_UNSUPPORTED", "Device-global Codex account switching requires file-backed credentials")
	}
	globalPath := s.codexAccounts.globalCredentialPath()
	globalCredential, admitted, credentialErr := readCodexFileState(globalPath, false)
	latestCredential, latest, latestErr := readCodexFileState(globalPath, false)
	if credentialErr != nil || latestErr != nil || !sameCodexFileState(admitted, latest) ||
		!bytes.Equal(globalCredential, latestCredential) || !localCredentialIdentifiesRecord(record, latestCredential) {
		return apierr.Conflict("CODEX_GLOBAL_ACCOUNT_CHANGED", "The device Codex account changed", nil)
	}
	if s.CurrentCodexActiveAccount().AccountID != accountID {
		if err := s.codexAccounts.setActivePointer(ctx, accountID); err != nil {
			return apierr.Unavailable("CODEX_ACCOUNT_SWITCH_ACTIVATION_UNCONFIRMED", "The active Codex account state could not be updated")
		}
	}
	_ = writePrivateFileAtomic(filepath.Join(record.Home, codexCredentialFilename), globalCredential)
	s.codexAccounts.setManagedGlobal(accountID)
	s.codexAccounts.invalidateCredentialEvidence(accountID)
	s.readiness.Invalidate(string(domain.HarnessCodex), readinessInvalidateAuthentication)
	return nil
}

// CheckpointAndActivateCodexAccount journals and locally confirms a credential
// activation. It never starts Codex or contacts the provider.
func (s *Service) CheckpointAndActivateCodexAccount(ctx context.Context, sourceKind domain.CodexAccountSwitchSourceKind, switchID, targetID string, expectedRevision int64) (domain.CodexActiveAccount, error) {
	notCommitted := func(err error) (domain.CodexActiveAccount, error) {
		return domain.CodexActiveAccount{}, errors.Join(ports.ErrCodexAccountSwitchNotCommitted, err)
	}
	if s.codexAccounts == nil {
		return notCommitted(apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account management is unavailable"))
	}
	if !isCanonicalUUIDv4(strings.TrimSpace(switchID)) {
		return notCommitted(apierr.Invalid("INVALID_CODEX_ACCOUNT_ID", "Invalid Codex account switch identifier", nil))
	}
	current := s.CurrentCodexActiveAccount()
	if current.Revision != expectedRevision {
		return notCommitted(apierr.Conflict("CODEX_ACCOUNT_REVISION_CONFLICT", "The active Codex account changed", nil))
	}
	stagingDir := filepath.Join(s.codexAccounts.switchStagingRoot, switchID)
	if err := ensurePrivateDirectory(stagingDir); err != nil {
		return notCommitted(apierr.Unavailable("CODEX_ACCOUNT_SWITCH_ACTIVATION_UNCONFIRMED", "The Codex credential switch could not be staged"))
	}
	if sourceKind != domain.CodexAccountSwitchSourceManaged && sourceKind != domain.CodexAccountSwitchSourceDevice && sourceKind != domain.CodexAccountSwitchSourceNone {
		return notCommitted(apierr.Invalid("INVALID_CODEX_ACCOUNT_SWITCH_SOURCE", "Invalid Codex account switch source", nil))
	}
	if sourceKind != domain.CodexAccountSwitchSourceNone && s.codexAccounts.validateGlobalCredentialStore() != nil {
		return notCommitted(apierr.NotImplemented("CODEX_GLOBAL_CREDENTIAL_STORE_UNSUPPORTED", "Device-global Codex account switching requires file-backed credentials"))
	}
	globalCredential, globalState, err := readCodexFileState(s.codexAccounts.globalCredentialPath(), sourceKind == domain.CodexAccountSwitchSourceNone)
	if err != nil {
		return notCommitted(apierr.NotImplemented("CODEX_GLOBAL_CREDENTIAL_STORE_UNSUPPORTED", "Device-global Codex account switching requires file-backed credentials"))
	}
	if sourceKind == domain.CodexAccountSwitchSourceManaged {
		record, ok := s.codexAccounts.catalog.record(current.AccountID)
		if !ok {
			return notCommitted(apierr.Conflict("CODEX_GLOBAL_ACCOUNT_CHANGED", "The device Codex account changed before switching", nil))
		}
		latestGlobal, latestState, latestErr := readCodexFileState(s.codexAccounts.globalCredentialPath(), false)
		if latestErr != nil || !sameCodexFileState(globalState, latestState) || !bytes.Equal(globalCredential, latestGlobal) || !localCredentialIdentifiesRecord(record, latestGlobal) {
			return notCommitted(apierr.Conflict("CODEX_GLOBAL_ACCOUNT_CHANGED", "The device Codex account changed before switching", nil))
		}
		globalCredential = latestGlobal
		if err := writePrivateFileAtomic(filepath.Join(record.Home, codexCredentialFilename), globalCredential); err != nil {
			return notCommitted(apierr.Unavailable("CODEX_ACCOUNT_SWITCH_ACTIVATION_UNCONFIRMED", "The active Codex credential could not be checkpointed"))
		}
	} else if sourceKind == domain.CodexAccountSwitchSourceDevice {
		if !globalState.exists {
			return notCommitted(ports.ErrCodexGlobalAccountChanged)
		}
	} else if sourceKind == domain.CodexAccountSwitchSourceNone && globalState.exists {
		return notCommitted(ports.ErrCodexGlobalAccountChanged)
	}
	target, ok := s.codexAccounts.catalog.record(strings.TrimSpace(targetID))
	if !ok || target.Snapshot.Status != domain.CodexAccountStatusValid {
		return notCommitted(apierr.NotFound("CODEX_ACCOUNT_NOT_FOUND", "Codex account not found"))
	}
	targetCredential, targetState, targetErr := readCodexFileState(filepath.Join(target.Home, codexCredentialFilename), false)
	latestTarget, latestTargetState, latestTargetErr := readCodexFileState(filepath.Join(target.Home, codexCredentialFilename), false)
	if targetErr != nil || latestTargetErr != nil || !sameCodexFileState(targetState, latestTargetState) || !bytes.Equal(targetCredential, latestTarget) || !localCredentialIdentifiesRecord(target, latestTarget) {
		s.codexAccounts.requireReauthentication(target.Snapshot.ID)
		return notCommitted(apierr.Conflict("CODEX_ACCOUNT_REAUTHENTICATION_REQUIRED", "Sign in again before switching to this Codex account", nil))
	}
	if err := writePrivateFileAtomic(filepath.Join(stagingDir, "target-auth.json"), latestTarget); err != nil {
		return notCommitted(apierr.Unavailable("CODEX_ACCOUNT_SWITCH_ACTIVATION_UNCONFIRMED", "The selected Codex credential could not be staged"))
	}
	expectedGlobal := globalCredential
	if sourceKind == domain.CodexAccountSwitchSourceNone {
		expectedGlobal = []byte{}
	}
	active, err := s.codexAccounts.activateFromCredentialLocked(ctx, strings.TrimSpace(targetID), expectedRevision, filepath.Join(stagingDir, "target-auth.json"), expectedGlobal)
	if err == nil {
		s.readiness.Invalidate(string(domain.HarnessCodex), readinessInvalidateAuthentication)
	}
	return active, err
}

// CleanupCodexAccountSwitch removes private switch staging after the durable
// switch has reached a locally confirmed terminal phase.
func (s *Service) CleanupCodexAccountSwitch(_ context.Context, switchID string) error {
	if s.codexAccounts == nil || !isCanonicalUUIDv4(strings.TrimSpace(switchID)) {
		return nil
	}
	return os.RemoveAll(filepath.Join(s.codexAccounts.switchStagingRoot, switchID))
}

var _ ports.CodexAccountCredentialManager = (*Service)(nil)

// CodexAccountSwitchInProgress reports whether the credential coordinator owns
// the device-global mutation gate.
func (s *Service) CodexAccountSwitchInProgress() bool {
	return s.codexSwitches != nil && s.codexSwitches.CodexAccountSwitchInProgress()
}

// StartCodexAccountSwitch starts a durable device credential switch.
func (s *Service) StartCodexAccountSwitch(ctx context.Context, cfg ports.CodexAccountSwitchConfig) (domain.CodexAccountSwitch, error) {
	if s.codexSwitches == nil {
		return domain.CodexAccountSwitch{}, apierr.Unavailable("CODEX_ACCOUNT_MANAGEMENT_UNAVAILABLE", "Codex account switching is unavailable")
	}
	return s.codexSwitches.StartCodexAccountSwitch(ctx, cfg)
}

// ReconcileCodexAccountSwitches settles any durable switch journal from the
// current local device credential before session startup is admitted.
func (s *Service) ReconcileCodexAccountSwitches(ctx context.Context) error {
	if s.codexSwitches == nil {
		return nil
	}
	return s.codexSwitches.ReconcileCodexAccountSwitches(ctx)
}

// WaitCodexAccountSwitchWorkers drains credential workers during shutdown.
func (s *Service) WaitCodexAccountSwitchWorkers(ctx context.Context) error {
	if s.codexSwitches == nil {
		return nil
	}
	return s.codexSwitches.Wait(ctx)
}
