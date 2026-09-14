package agent

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/codexops"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type coordinatorCredentialFake struct {
	mu          sync.Mutex
	active      domain.CodexActiveAccount
	calls       []string
	activateErr error
}

func (f *coordinatorCredentialFake) call(value string) {
	f.mu.Lock()
	f.calls = append(f.calls, value)
	f.mu.Unlock()
}

func (f *coordinatorCredentialFake) WaitCodexAccountStoreReady(context.Context) error {
	f.call("store")
	return nil
}
func (f *coordinatorCredentialFake) EnsureCodexDeviceAccountReconciled(context.Context) error {
	f.call("reconcile")
	return nil
}
func (f *coordinatorCredentialFake) BeginCodexAccountMutation(context.Context) error {
	f.call("begin")
	return nil
}
func (f *coordinatorCredentialFake) EndCodexAccountMutation() { f.call("end") }
func (f *coordinatorCredentialFake) CurrentCodexActiveAccount() domain.CodexActiveAccount {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}
func (f *coordinatorCredentialFake) CurrentCodexAccountSwitchSource() domain.CodexAccountSwitchSource {
	f.mu.Lock()
	defer f.mu.Unlock()
	return domain.CodexAccountSwitchSource{
		Kind: domain.CodexAccountSwitchSourceManaged, AccountID: f.active.AccountID, Revision: f.active.Revision,
	}
}
func (*coordinatorCredentialFake) CodexAccountLoginInProgress() bool { return false }
func (f *coordinatorCredentialFake) PrepareCodexAccountForSwitch(_ context.Context, id string) error {
	f.call("prepare-target:" + id)
	return nil
}
func (f *coordinatorCredentialFake) ConfirmCodexAccountSwitchTarget(_ context.Context, id string) error {
	f.call("confirm-target:" + id)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active.AccountID != id {
		return errors.New("unexpected active account")
	}
	return nil
}
func (f *coordinatorCredentialFake) CheckpointAndActivateCodexAccount(_ context.Context, _ domain.CodexAccountSwitchSourceKind, _ string, target string, expected int64) (domain.CodexActiveAccount, error) {
	f.call("activate:" + target)
	if f.activateErr != nil {
		return domain.CodexActiveAccount{}, f.activateErr
	}
	f.mu.Lock()
	f.active = domain.CodexActiveAccount{AccountID: target, Revision: expected + 1}
	active := f.active
	f.mu.Unlock()
	return active, nil
}
func (f *coordinatorCredentialFake) CleanupCodexAccountSwitch(context.Context, string) error {
	f.call("cleanup")
	return nil
}

type coordinatorSwitchStoreFake struct {
	mu     sync.Mutex
	record domain.CodexAccountSwitch
}

func (s *coordinatorSwitchStoreFake) CreateCodexAccountSwitch(_ context.Context, record domain.CodexAccountSwitch) (domain.CodexAccountSwitch, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ID != "" {
		return s.record, false, nil
	}
	s.record = record
	return record, true, nil
}
func (s *coordinatorSwitchStoreFake) GetCodexAccountSwitch(_ context.Context, id string) (domain.CodexAccountSwitch, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record, s.record.ID == id, nil
}
func (s *coordinatorSwitchStoreFake) GetCodexAccountSwitchByIdempotency(_ context.Context, key string) (domain.CodexAccountSwitch, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record, s.record.IdempotencyKey == key && key != "", nil
}
func (s *coordinatorSwitchStoreFake) GetActiveCodexAccountSwitch(context.Context) (domain.CodexAccountSwitch, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record, s.record.ID != "" && !s.record.Phase.Terminal(), nil
}
func (s *coordinatorSwitchStoreFake) UpdateCodexAccountSwitch(_ context.Context, record domain.CodexAccountSwitch, expected domain.CodexAccountSwitchPhase) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ID != "" && s.record.Phase != expected {
		return false, nil
	}
	s.record = record
	return true, nil
}

func TestCodexAccountSwitchFingerprintIsVersionedAndStable(t *testing.T) {
	first := codexAccountSwitchFingerprint("account-b", 7)
	if !strings.HasPrefix(first, "v3:") || len(first) != len("v3:")+64 {
		t.Fatalf("fingerprint = %q", first)
	}
	if first != codexAccountSwitchFingerprint("account-b", 7) || first == codexAccountSwitchFingerprint("account-b", 8) {
		t.Fatal("fingerprint is not stable and revision-specific")
	}
}

func TestCodexAccountSwitchCoordinatorCompletesCredentialOnlySwitch(t *testing.T) {
	credentials := &coordinatorCredentialFake{active: domain.CodexActiveAccount{AccountID: "source", Revision: 1}}
	store := &coordinatorSwitchStoreFake{}
	coordinator := newCodexAccountSwitchCoordinator(context.Background(), credentials, store, codexops.NewGate(), time.Now, nil)

	if _, err := coordinator.StartCodexAccountSwitch(context.Background(), ports.CodexAccountSwitchConfig{
		TargetAccountID: "target", ExpectedAccountRevision: 1, IdempotencyKey: "request-1",
	}); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := coordinator.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	phase := store.record.Phase
	store.mu.Unlock()
	if phase != domain.CodexAccountSwitchCompleted {
		t.Fatalf("phase = %q, want completed", phase)
	}
	credentials.mu.Lock()
	calls := append([]string(nil), credentials.calls...)
	credentials.mu.Unlock()
	for _, want := range []string{"store", "reconcile", "begin", "prepare-target:target", "activate:target", "cleanup", "end"} {
		if !slices.Contains(calls, want) {
			t.Fatalf("calls = %v, missing %q", calls, want)
		}
	}
}

func TestCodexAccountSwitchCoordinatorActivatesSavedAccountWhenDeviceCredentialIsMissing(t *testing.T) {
	root := t.TempDir()
	globalHome := filepath.Join(root, "global-codex")
	if err := ensurePrivateDirectory(globalHome); err != nil {
		t.Fatal(err)
	}
	credential := testAPIKeyCredential("saved-target-key")
	factory := &fakeCodexAccountFactory{
		capabilities: supportedCodexAccountCapabilities(),
		open: func(ports.CodexAccountContext) (ports.CodexAccountClient, error) {
			return &fakeCodexAccountClient{read: ports.CodexAccountObservation{
				Authentication: domain.AgentAuthenticationAuthorized,
				Method:         domain.CodexAuthMethodAPIKey,
			}}, nil
		},
	}
	manager := newCodexAccountManager(
		context.Background(),
		filepath.Join(root, "accounts"),
		filepath.Join(root, "pending"),
		filepath.Join(root, "staging"),
		globalHome,
		factory,
		nil,
		nil,
	)
	manager.catalog.newID = func() string { return testAccountID }
	target := commitTestAccountWithCredential(
		t,
		manager.catalog,
		manager.pendingRoot,
		"b60a377d-da68-4a61-86f2-f31f04c571f2",
		credential,
		ports.CodexAccountObservation{
			Authentication: domain.AgentAuthenticationAuthorized,
			Method:         domain.CodexAuthMethodAPIKey,
		},
	)
	manager.mu.Lock()
	manager.accountStoreReady = true
	manager.mu.Unlock()

	service := &Service{codexAccounts: manager, readiness: newReadinessCoordinator(readinessCoordinatorConfig{})}
	store := &coordinatorSwitchStoreFake{}
	coordinator := newCodexAccountSwitchCoordinator(context.Background(), service, store, codexops.NewGate(), time.Now, nil)

	if _, err := coordinator.StartCodexAccountSwitch(context.Background(), ports.CodexAccountSwitchConfig{
		TargetAccountID: target.Snapshot.ID, ExpectedAccountRevision: 0, IdempotencyKey: "use-saved-account",
	}); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := coordinator.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	completed := store.record
	store.mu.Unlock()
	if completed.Phase != domain.CodexAccountSwitchCompleted || completed.SourceKind != domain.CodexAccountSwitchSourceNone {
		t.Fatalf("switch = %#v, want completed switch from no device account", completed)
	}
	active := service.CurrentCodexActiveAccount()
	if active.AccountID != target.Snapshot.ID || active.Revision != 1 {
		t.Fatalf("active account = %#v", active)
	}
	installed, err := readOpaqueCredential(manager.globalCredentialPath())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(installed, credential) {
		t.Fatal("saved credential was not installed as the device credential")
	}
}

func TestCodexAccountSwitchCoordinatorSettlesActivationFailureWithoutRollback(t *testing.T) {
	credentials := &coordinatorCredentialFake{
		active: domain.CodexActiveAccount{AccountID: "source", Revision: 1}, activateErr: errors.New("activate failed"),
	}
	store := &coordinatorSwitchStoreFake{record: domain.CodexAccountSwitch{
		ID: "switch-1", SourceKind: domain.CodexAccountSwitchSourceManaged,
		SourceAccountID: "source", TargetAccountID: "target", ExpectedAccountRevision: 1,
		Phase: domain.CodexAccountSwitchActivatingAccount,
	}}
	coordinator := newCodexAccountSwitchCoordinator(context.Background(), credentials, store, codexops.NewGate(), time.Now, nil)
	sw := store.record
	coordinator.dispatchCodexAccountSwitch(context.Background(), credentials, store, &sw)

	if sw.Phase != domain.CodexAccountSwitchFailed || sw.FailureCode != "activation_failed" {
		t.Fatalf("switch = (%q,%q), want failed activation_failed", sw.Phase, sw.FailureCode)
	}
	credentials.mu.Lock()
	calls := append([]string(nil), credentials.calls...)
	credentials.mu.Unlock()
	if !slices.Contains(calls, "confirm-target:target") {
		t.Fatalf("settlement calls = %v, want local target confirmation", calls)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "restore:") {
			t.Fatalf("settlement tried to restore a stale source credential: %v", calls)
		}
	}
}

func TestCodexAccountSwitchCoordinatorAdoptsExternalDeviceAccountInsteadOfWaitingForRecovery(t *testing.T) {
	fixture := newAPIKeySwitchFixture(t)
	externalCredential := testAPIKeyCredential("external-api-key")
	if err := writeGlobalCredentialAtomic(fixture.manager.globalCredentialPath(), externalCredential); err != nil {
		t.Fatal(err)
	}
	fixture.manager.catalog.newID = func() string { return "f47ac10b-58cc-4372-a567-0e02b2c3d479" }
	store := &coordinatorSwitchStoreFake{record: domain.CodexAccountSwitch{
		ID: "6f8dfc76-8db4-4621-8974-c480093e0d55", SourceKind: domain.CodexAccountSwitchSourceManaged,
		SourceAccountID: fixture.source.Snapshot.ID, TargetAccountID: fixture.target.Snapshot.ID,
		ExpectedAccountRevision: 1, Phase: domain.CodexAccountSwitchRecoveryRequired,
	}}
	coordinator := newCodexAccountSwitchCoordinator(context.Background(), fixture.service, store, codexops.NewGate(), time.Now, nil)

	if err := coordinator.ReconcileCodexAccountSwitches(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := coordinator.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	settled := store.record
	store.mu.Unlock()
	if settled.Phase != domain.CodexAccountSwitchFailed || settled.FailureCode != "legacy_switch_interrupted" {
		t.Fatalf("switch = (%q,%q), want terminal superseded switch", settled.Phase, settled.FailureCode)
	}
	active := fixture.service.CurrentCodexActiveAccount()
	if active.AccountID == fixture.source.Snapshot.ID || active.AccountID == fixture.target.Snapshot.ID || active.AccountID == "" {
		t.Fatalf("external device account was not adopted: %#v", active)
	}
	installed, err := readOpaqueCredential(fixture.manager.globalCredentialPath())
	if err != nil || !slices.Equal(installed, externalCredential) {
		t.Fatalf("external device credential changed: %q, %v", installed, err)
	}
}
