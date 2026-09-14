package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type codexAccountSwitchCoordinator struct {
	credentials                     ports.CodexAccountCredentialManager
	store                           ports.CodexAccountSwitchStore
	codexOperationGate              ports.CodexOperationGate
	codexAccountSwitchMu            sync.Mutex
	codexAccountSwitchWorkerRunning bool
	codexAccountSwitchLease         ports.CodexOperationLease
	backgroundContext               context.Context
	workers                         sync.WaitGroup
	workersMu                       sync.Mutex
	workersClosed                   bool
	clock                           func() time.Time
	publish                         func()
}

const codexAccountSwitchDurableBoundaryWait = 5 * time.Second

func codexAccountSwitchDurableContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), codexAccountSwitchDurableBoundaryWait)
}

func newCodexAccountSwitchCoordinator(
	ctx context.Context,
	credentials ports.CodexAccountCredentialManager,
	store ports.CodexAccountSwitchStore,
	gate ports.CodexOperationGate,
	clock func() time.Time,
	publish func(),
) *codexAccountSwitchCoordinator {
	if ctx == nil {
		ctx = context.Background()
	}
	if clock == nil {
		clock = time.Now
	}
	return &codexAccountSwitchCoordinator{
		credentials: credentials, store: store, codexOperationGate: gate,
		backgroundContext: ctx, clock: clock, publish: publish,
	}
}

func (m *codexAccountSwitchCoordinator) publishCodexAccountSwitchChanged() {
	if m.publish != nil {
		m.publish()
	}
}

func codexAccountSwitchFingerprint(target string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("v4\x00%s", target)))
	return "v4:" + hex.EncodeToString(sum[:])
}

func (m *codexAccountSwitchCoordinator) codexAccountSwitchDependencies() (ports.CodexAccountCredentialManager, ports.CodexAccountSwitchStore, error) {
	credentials := m.credentials
	if credentials == nil {
		return nil, nil, errors.New("codex account credential manager is unavailable")
	}
	store := m.store
	if store == nil {
		return nil, nil, errors.New("codex account switch store is unavailable")
	}
	return credentials, store, nil
}

func (m *codexAccountSwitchCoordinator) acquireCodexAccountSwitchGate(ctx context.Context) error {
	lease, err := m.codexOperationGate.AcquireExclusive(ctx)
	if err != nil {
		return err
	}
	m.codexAccountSwitchMu.Lock()
	defer m.codexAccountSwitchMu.Unlock()
	if m.codexAccountSwitchWorkerRunning || m.codexAccountSwitchLease != nil {
		lease.Release()
		return ports.ErrCodexAccountSwitchInProgress
	}
	m.codexAccountSwitchLease = lease
	m.codexAccountSwitchWorkerRunning = true
	return nil
}

func (m *codexAccountSwitchCoordinator) finishCodexAccountSwitchWorker() {
	m.codexAccountSwitchMu.Lock()
	m.codexAccountSwitchWorkerRunning = false
	release := m.codexAccountSwitchLease
	m.codexAccountSwitchLease = nil
	m.codexAccountSwitchMu.Unlock()
	if release != nil {
		release.Release()
	}
}

func (m *codexAccountSwitchCoordinator) finishCodexAccountSwitchMutation(credentials ports.CodexAccountCredentialManager) {
	m.finishCodexAccountSwitchWorker()
	credentials.EndCodexAccountMutation()
}

func (m *codexAccountSwitchCoordinator) codexAccountSwitchIsActive() bool {
	return m.codexOperationGate != nil && m.codexOperationGate.ExclusivePendingOrHeld()
}

// CodexAccountSwitchInProgress is the daemon-wide credential admission fence.
func (m *codexAccountSwitchCoordinator) CodexAccountSwitchInProgress() bool {
	return m.codexAccountSwitchIsActive()
}

// StartCodexAccountSwitch admits and starts one account-service-owned global switch.
// Existing controllers are deliberately outside this transaction: the operation
// changes and verifies the device credential only.
func (m *codexAccountSwitchCoordinator) StartCodexAccountSwitch(ctx context.Context, cfg ports.CodexAccountSwitchConfig) (domain.CodexAccountSwitch, error) {
	cfg.TargetAccountID = strings.TrimSpace(cfg.TargetAccountID)
	cfg.IdempotencyKey = strings.TrimSpace(cfg.IdempotencyKey)
	if cfg.IdempotencyKey == "" {
		return domain.CodexAccountSwitch{}, errors.New("idempotency key is required")
	}
	credentials, store, err := m.codexAccountSwitchDependencies()
	if err != nil {
		return domain.CodexAccountSwitch{}, err
	}
	fingerprint := codexAccountSwitchFingerprint(cfg.TargetAccountID)
	if existing, ok, readErr := store.GetCodexAccountSwitchByIdempotency(ctx, cfg.IdempotencyKey); readErr != nil {
		return domain.CodexAccountSwitch{}, readErr
	} else if ok {
		if existing.TargetAccountID != cfg.TargetAccountID {
			return existing, ports.ErrCodexAccountSwitchIdempotencyConflict
		}
		return existing, nil
	}
	if _, active, readErr := store.GetActiveCodexAccountSwitch(ctx); readErr != nil {
		return domain.CodexAccountSwitch{}, readErr
	} else if active {
		return domain.CodexAccountSwitch{}, ports.ErrCodexAccountSwitchInProgress
	}
	if err := credentials.WaitCodexAccountStoreReady(ctx); err != nil {
		return domain.CodexAccountSwitch{}, err
	}
	if err := m.acquireCodexAccountSwitchGate(ctx); err != nil {
		return domain.CodexAccountSwitch{}, err
	}
	releaseSwitchGate := true
	defer func() {
		if releaseSwitchGate {
			m.finishCodexAccountSwitchWorker()
		}
	}()
	if err := credentials.BeginCodexAccountMutation(ctx); err != nil {
		return domain.CodexAccountSwitch{}, err
	}
	releaseMutation := true
	defer func() {
		if releaseMutation {
			credentials.EndCodexAccountMutation()
		}
	}()
	if err := credentials.CleanupInactiveCodexAccountSwitches(ctx, ""); err != nil {
		return domain.CodexAccountSwitch{}, err
	}

	switchID := uuid.NewString()
	source, err := credentials.PrepareCodexAccountForSwitch(ctx, switchID, cfg.TargetAccountID)
	if err != nil {
		return domain.CodexAccountSwitch{}, err
	}
	if source.Kind == domain.CodexAccountSwitchSourceManaged && source.AccountID == cfg.TargetAccountID {
		_ = credentials.CleanupCodexAccountSwitch(ctx, switchID)
		return domain.CodexAccountSwitch{}, ports.ErrCodexAccountAlreadyActive
	}
	now := m.clock()
	sw := domain.CodexAccountSwitch{
		ID: switchID, SourceKind: source.Kind, SourceAccountID: source.AccountID,
		TargetAccountID: cfg.TargetAccountID, Phase: domain.CodexAccountSwitchRequested,
		IdempotencyKey: cfg.IdempotencyKey, RequestFingerprint: fingerprint,
		CreatedAt: now, UpdatedAt: now,
	}
	created, inserted, err := store.CreateCodexAccountSwitch(ctx, sw)
	if err != nil {
		_ = credentials.CleanupCodexAccountSwitch(ctx, switchID)
		return domain.CodexAccountSwitch{}, err
	}
	sw = created
	if !inserted {
		_ = credentials.CleanupCodexAccountSwitch(ctx, switchID)
		return sw, nil
	}

	if !m.startWorker(func() {
		m.runCodexAccountSwitch(m.backgroundContext, credentials, store, sw)
	}) {
		// Shutdown raced with admission before the credential mutation worker
		// could start. Terminally cancel this pre-mutation journal now so it does
		// not leave a fake recovery state for the next daemon.
		settleCtx, cancel := codexAccountSwitchDurableContext(ctx)
		sw.FailureCode = "switch_cancelled_before_mutation"
		m.failAndCleanupCodexAccountSwitch(settleCtx, credentials, store, &sw)
		cancel()
		return sw, context.Canceled
	}
	releaseMutation = false
	releaseSwitchGate = false
	return sw, nil
}

func (m *codexAccountSwitchCoordinator) runCodexAccountSwitch(ctx context.Context, credentials ports.CodexAccountCredentialManager, store ports.CodexAccountSwitchStore, sw domain.CodexAccountSwitch) {
	defer func() {
		m.finishCodexAccountSwitchMutation(credentials)
		if sw.Phase.Terminal() {
			// The device credential is the source of truth. Reconcile after every
			// terminal outcome so an external login is matched or imported.
			_ = credentials.EnsureCodexDeviceAccountReconciled(m.backgroundContext)
		}
	}()
	for attempt := 0; attempt < 3 && !sw.Phase.Terminal(); attempt++ {
		m.dispatchCodexAccountSwitch(ctx, credentials, store, &sw)
		if sw.Phase.Terminal() {
			break
		}
		current, found, err := store.GetCodexAccountSwitch(ctx, sw.ID)
		if err != nil || !found {
			continue
		}
		sw = current
	}
}

// runCodexAccountSwitchRecovery never resumes an interrupted user request.
// It only recognizes an already-installed target; every other local state
// terminally cancels the old journal and lets reconciliation adopt reality.
func (m *codexAccountSwitchCoordinator) runCodexAccountSwitchRecovery(ctx context.Context, credentials ports.CodexAccountCredentialManager, store ports.CodexAccountSwitchStore, sw domain.CodexAccountSwitch) {
	defer func() {
		m.finishCodexAccountSwitchMutation(credentials)
		_ = credentials.EnsureCodexDeviceAccountReconciled(m.backgroundContext)
	}()
	m.settleCodexAccountSwitch(ctx, credentials, store, &sw, "interrupted_switch_cancelled")
}

func (m *codexAccountSwitchCoordinator) dispatchCodexAccountSwitch(ctx context.Context, credentials ports.CodexAccountCredentialManager, store ports.CodexAccountSwitchStore, sw *domain.CodexAccountSwitch) {
	// Switches created before source_kind was introduced are managed-account
	// switches. Keep that compatibility at the credential coordinator boundary.
	if sw.SourceKind == "" {
		sw.SourceKind = domain.CodexAccountSwitchSourceManaged
	}
	for {
		switch sw.Phase {
		case domain.CodexAccountSwitchRequested:
			if m.advanceCodexAccountSwitch(ctx, store, sw, domain.CodexAccountSwitchCheckpointCredential, "") != nil {
				return
			}
		case domain.CodexAccountSwitchCheckpointCredential:
			if m.advanceCodexAccountSwitch(ctx, store, sw, domain.CodexAccountSwitchActivatingAccount, "") != nil {
				return
			}
		case domain.CodexAccountSwitchActivatingAccount:
			if err := credentials.ConfirmCodexAccountSwitchTarget(ctx, sw.ID, sw.TargetAccountID); err != nil {
				if err := credentials.ActivatePreparedCodexAccountSwitch(ctx, sw.SourceKind, sw.ID, sw.TargetAccountID); err != nil {
					if errors.Is(err, ports.ErrCodexAccountSwitchNotCommitted) {
						sw.FailureCode = "activation_failed"
						m.failAndCleanupCodexAccountSwitch(ctx, credentials, store, sw)
						return
					}
					m.settleCodexAccountSwitch(ctx, credentials, store, sw, "activation_failed")
					return
				}
			}
			committedAt := m.clock()
			sw.CredentialsCommittedAt = &committedAt
			m.completeCodexAccountSwitch(ctx, credentials, store, sw)
			return
		case domain.CodexAccountSwitchRecoveryRequired:
			// Compatibility for a switch journal written by an older build. There
			// is no user-driven recovery anymore: settle it from the current local
			// credential, then let normal reconciliation adopt that credential.
			m.settleCodexAccountSwitch(ctx, credentials, store, sw, "legacy_switch_interrupted")
			return
		case domain.CodexAccountSwitchCompleted, domain.CodexAccountSwitchFailed:
			return
		default:
			sw.FailureCode = "switch_state_unavailable"
			m.failAndCleanupCodexAccountSwitch(ctx, credentials, store, sw)
			return
		}
	}
}

// settleCodexAccountSwitch resolves an interrupted operation from local state.
// If the target is installed, the switch completes. Otherwise the switch ends
// and ordinary reconciliation adopts whatever credential the device now has.
func (m *codexAccountSwitchCoordinator) settleCodexAccountSwitch(ctx context.Context, credentials ports.CodexAccountCredentialManager, store ports.CodexAccountSwitchStore, sw *domain.CodexAccountSwitch, failureCode string) {
	if err := credentials.ConfirmCodexAccountSwitchTarget(ctx, sw.ID, sw.TargetAccountID); err == nil {
		if sw.CredentialsCommittedAt == nil {
			committedAt := m.clock()
			sw.CredentialsCommittedAt = &committedAt
		}
		m.completeCodexAccountSwitch(ctx, credentials, store, sw)
		return
	}
	sw.FailureCode = failureCode
	m.failAndCleanupCodexAccountSwitch(ctx, credentials, store, sw)
}

func (m *codexAccountSwitchCoordinator) completeCodexAccountSwitch(ctx context.Context, credentials ports.CodexAccountCredentialManager, store ports.CodexAccountSwitchStore, sw *domain.CodexAccountSwitch) {
	completed := m.clock()
	sw.CompletedAt = &completed
	if m.advanceCodexAccountSwitch(ctx, store, sw, domain.CodexAccountSwitchCompleted, "") == nil {
		_ = credentials.CleanupCodexAccountSwitch(ctx, sw.ID)
	}
}

func (m *codexAccountSwitchCoordinator) failAndCleanupCodexAccountSwitch(ctx context.Context, credentials ports.CodexAccountCredentialManager, store ports.CodexAccountSwitchStore, sw *domain.CodexAccountSwitch) {
	completed := m.clock()
	sw.CompletedAt = &completed
	if m.advanceCodexAccountSwitch(ctx, store, sw, domain.CodexAccountSwitchFailed, sw.FailureCode) == nil {
		_ = credentials.CleanupCodexAccountSwitch(ctx, sw.ID)
	}
}

func (m *codexAccountSwitchCoordinator) advanceCodexAccountSwitch(ctx context.Context, store ports.CodexAccountSwitchStore, sw *domain.CodexAccountSwitch, next domain.CodexAccountSwitchPhase, code string) error {
	expected := sw.Phase
	candidate := *sw
	candidate.Phase, candidate.FailureCode, candidate.UpdatedAt = next, code, m.clock()
	ok, err := store.UpdateCodexAccountSwitch(ctx, candidate, expected)
	if err == nil && ok {
		*sw = candidate
		m.publishCodexAccountSwitchChanged()
		return nil
	}
	settleCtx, cancel := codexAccountSwitchDurableContext(ctx)
	defer cancel()
	current, found, readErr := store.GetCodexAccountSwitch(settleCtx, sw.ID)
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	if found && current.Phase == candidate.Phase && current.FailureCode == candidate.FailureCode {
		*sw = current
		return nil
	}
	if found {
		*sw = current
	}
	if err != nil {
		return err
	}
	return errors.New("codex account switch changed concurrently")
}

// GetActiveCodexAccountSwitch returns the sole nonterminal switch when present.
func (m *codexAccountSwitchCoordinator) GetActiveCodexAccountSwitch(ctx context.Context) (domain.CodexAccountSwitch, bool, error) {
	_, store, err := m.codexAccountSwitchDependencies()
	if err != nil {
		return domain.CodexAccountSwitch{}, false, err
	}
	sw, ok, err := store.GetActiveCodexAccountSwitch(ctx)
	if err != nil || !ok {
		return sw, ok, err
	}
	return sw, true, nil
}

// ReconcileCodexAccountSwitches starts the best-effort recovery supervisor.
// Account-switch recovery must never prevent the rest of the daemon starting.
func (m *codexAccountSwitchCoordinator) ReconcileCodexAccountSwitches(ctx context.Context) error {
	credentials, store, err := m.codexAccountSwitchDependencies()
	if err != nil {
		return nil //nolint:nilerr // account switching is optional when its feature wiring is absent.
	}
	sw, ok, err := store.GetActiveCodexAccountSwitch(ctx)
	if err != nil {
		m.startCodexAccountSwitchRecoveryRetry(credentials, store)
		return err
	}
	if !ok {
		_ = credentials.CleanupInactiveCodexAccountSwitches(ctx, "")
		return nil
	}
	if err := m.startCodexAccountSwitchRecovery(ctx, credentials, store, sw); err != nil {
		m.startCodexAccountSwitchRecoveryRetry(credentials, store)
		return err
	}
	return nil
}

func (m *codexAccountSwitchCoordinator) startCodexAccountSwitchRecovery(ctx context.Context, credentials ports.CodexAccountCredentialManager, store ports.CodexAccountSwitchStore, sw domain.CodexAccountSwitch) error {
	if err := credentials.WaitCodexAccountStoreReady(ctx); err != nil {
		return err
	}
	if err := m.acquireCodexAccountSwitchGate(ctx); err != nil {
		return err
	}
	if err := credentials.BeginCodexAccountMutation(ctx); err != nil {
		m.finishCodexAccountSwitchWorker()
		return err
	}
	if err := credentials.CleanupInactiveCodexAccountSwitches(ctx, sw.ID); err != nil {
		credentials.EndCodexAccountMutation()
		m.finishCodexAccountSwitchWorker()
		return err
	}
	if !m.startWorker(func() {
		m.runCodexAccountSwitchRecovery(m.backgroundContext, credentials, store, sw)
	}) {
		credentials.EndCodexAccountMutation()
		m.finishCodexAccountSwitchWorker()
		return context.Canceled
	}
	return nil
}

func (m *codexAccountSwitchCoordinator) startCodexAccountSwitchRecoveryRetry(credentials ports.CodexAccountCredentialManager, store ports.CodexAccountSwitchStore) {
	_ = m.startWorker(func() {
		delay := time.Second
		for {
			select {
			case <-m.backgroundContext.Done():
				return
			case <-time.After(delay):
			}
			sw, ok, err := store.GetActiveCodexAccountSwitch(m.backgroundContext)
			if err != nil {
				if delay < 30*time.Second {
					delay *= 2
				}
				continue
			}
			if !ok {
				return
			}
			if err := m.startCodexAccountSwitchRecovery(m.backgroundContext, credentials, store, sw); err != nil {
				if errors.Is(err, ports.ErrCodexAccountSwitchInProgress) {
					return
				}
				continue
			}
			return
		}
	})
}

func (m *codexAccountSwitchCoordinator) startWorker(run func()) bool {
	m.workersMu.Lock()
	defer m.workersMu.Unlock()
	if m.workersClosed {
		return false
	}
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		run()
	}()
	return true
}

func (m *codexAccountSwitchCoordinator) Wait(ctx context.Context) error {
	m.workersMu.Lock()
	m.workersClosed = true
	m.workersMu.Unlock()
	done := make(chan struct{})
	go func() {
		m.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
