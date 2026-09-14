package agent

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	agentregistry "github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// codexReadinessPropagationFixture wires the Codex account manager together with
// the readiness coordinator the launch/reviewer path uses, so a divergence
// between the two caching layers is observable. The active account is backed by
// the device-global Codex home, and the refresh-capable read can toggle between
// unauthorized and authorized to model an out-of-band sign-in.
type codexReadinessPropagationFixture struct {
	t        *testing.T
	manager  *codexAccountManager
	service  *Service
	active   codexAccountRecord
	mu       sync.Mutex
	signedIn bool
	reads    []codexReadCall
}

func newCodexReadinessPropagationFixture(t *testing.T) *codexReadinessPropagationFixture {
	t.Helper()
	root := t.TempDir()
	globalHome := filepath.Join(root, "global-codex")
	if err := ensurePrivateDirectory(globalHome); err != nil {
		t.Fatal(err)
	}
	state := &fakeCodexAccountStateStore{active: domain.CodexActiveAccount{AccountID: testAccountID, Revision: 1}, found: true}
	manager := newCodexAccountManager(context.Background(),
		filepath.Join(root, "accounts"), filepath.Join(root, "pending"),
		filepath.Join(root, "staging"), globalHome, nil, state, nil)
	ids := []string{testAccountID}
	manager.catalog.newID = func() string { id := ids[0]; ids = ids[1:]; return id }
	manager.newID = func() string { return "b9a4e5c6-4f31-4b1a-9d2a-7b4a4c0f9a11" }
	activeEmail := "active@example.com"
	active := commitTestAccount(t, manager.catalog, manager.pendingRoot, "b60a377d-da68-4a61-86f2-f31f04c571f2", ports.CodexAccountObservation{
		Authentication: domain.AgentAuthenticationAuthorized, Method: domain.CodexAuthMethodChatGPT, Email: &activeEmail,
	})
	activeCredential, err := readOpaqueCredential(filepath.Join(active.Home, codexCredentialFilename))
	if err != nil {
		t.Fatal(err)
	}
	// The active account is backed by the device-global Codex home.
	if err := writeGlobalCredentialAtomic(manager.globalCredentialPath(), activeCredential); err != nil {
		t.Fatal(err)
	}
	manager.active = state.active
	manager.bootstrapped = true
	fixture := &codexReadinessPropagationFixture{t: t, manager: manager, active: active}
	manager.factory = &fakeCodexAccountFactory{capabilities: supportedCodexAccountCapabilities(), open: func(account ports.CodexAccountContext) (ports.CodexAccountClient, error) {
		global := !account.Managed
		return &fakeCodexAccountClient{readFn: func(_ context.Context, refresh bool) (ports.CodexAccountObservation, error) {
			fixture.mu.Lock()
			fixture.reads = append(fixture.reads, codexReadCall{managed: account.Managed, refresh: refresh})
			signedIn := fixture.signedIn
			fixture.mu.Unlock()
			if global && refresh && !signedIn {
				// Codex answers the refresh-capable read with requiresOpenaiAuth
				// (e.g. an expired or revoked refresh token).
				return ports.CodexAccountObservation{Authentication: domain.AgentAuthenticationUnauthorized, Method: domain.CodexAuthMethodUnknown}, nil
			}
			return ports.CodexAccountObservation{Authentication: domain.AgentAuthenticationAuthorized, Method: domain.CodexAuthMethodChatGPT, Email: &activeEmail}, nil
		}}, nil
	}}
	codexAgent := agentregistry.HarnessAgent{
		Harness:  domain.AgentHarness("codex"),
		Manifest: adapters.Manifest{ID: "codex", Name: "Codex"},
		Agent:    fakeAgent{},
	}
	service := &Service{
		codexAccounts: manager,
		readiness:     newReadinessCoordinator(readinessCoordinatorConfig{Agents: []agentregistry.HarnessAgent{codexAgent}}),
	}
	// The readiness coordinator calls this bridge, which reuses the account
	// manager's launch-verified observation. This is the same wiring NewWithDeps
	// establishes in production.
	service.readiness.authenticationCheck = service.structuredCodexAuthentication
	fixture.service = service
	return fixture
}

func (f *codexReadinessPropagationFixture) signIn() {
	f.mu.Lock()
	f.signedIn = true
	f.mu.Unlock()
}

func (f *codexReadinessPropagationFixture) refreshCapableReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, read := range f.reads {
		if read.refresh {
			count++
		}
	}
	return count
}

func (f *codexReadinessPropagationFixture) account(view CodexAccounts, id string) domain.CodexAccountSnapshot {
	f.t.Helper()
	for _, account := range view.Accounts {
		if account.ID == id {
			return account
		}
	}
	f.t.Fatalf("account %q missing from view %#v", id, view.Accounts)
	return domain.CodexAccountSnapshot{}
}

// TestReviewerPreflightUsesStaleUnauthorizedAfterOutOfBandSignIn reproduces
// issue #5054: the Agents page / reviewer preflight reports Codex as
// unauthorized even though the user signed in to Codex out of band and the
// Settings account ensure path confirmed the active account is authorized.
//
// The active account is backed by the device-global Codex home. A prior launch
// readiness check (previous spawn or review trigger) records the account as
// unauthorized because the refresh-capable read reports requiresOpenaiAuth. That
// observation is cached in the readiness coordinator the reviewer preflight
// reuses. The user then signs in to Codex natively (`codex login`), replacing the
// device-global credential. The Settings ensure path detects the changed
// credential, re-verifies the active account, and records it as authorized with a
// fresh verifiedAt. But EnsureCodexAccounts never invalidates the readiness
// coordinator's cached authentication observation, so a review trigger less than
// a minute later is rejected with "agent auth catalog reports reviewer harness
// codex is unauthorized" even though the account is logged in.
func TestReviewerPreflightUsesStaleUnauthorizedAfterOutOfBandSignIn(t *testing.T) {
	fixture := newCodexReadinessPropagationFixture(t)

	// A prior launch/reviewer readiness check observes the active account as
	// unauthorized. The readiness coordinator the reviewer preflight reuses
	// caches that observation.
	initial, err := fixture.service.EnsureAgentReadiness(context.Background(), string(domain.HarnessCodex), domain.AgentReadinessPurposeLaunch)
	if err != nil {
		t.Fatalf("initial launch readiness: %v", err)
	}
	if initial.Authentication.State != domain.AgentAuthenticationUnauthorized {
		t.Fatalf("initial launch authentication = %#v, want unauthorized", initial.Authentication)
	}

	// The user signs in to Codex out of band (native `codex login`), replacing the
	// device-global credential material. The refresh-capable read now succeeds.
	fixture.signIn()
	if err := writeGlobalCredentialAtomic(fixture.manager.globalCredentialPath(), []byte("replacement-opaque-credential")); err != nil {
		t.Fatal(err)
	}

	// The Settings/Agents account ensure path detects the changed credential,
	// re-verifies the active account, and records it as authorized with a fresh
	// verifiedAt. This is the "verifiedAt timestamp immediately before the
	// failure" from the issue triage.
	settings, err := fixture.service.EnsureCodexAccounts(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("EnsureCodexAccounts: %v", err)
	}
	active := fixture.account(settings, fixture.active.Snapshot.ID)
	if active.Authentication.State != domain.AgentAuthenticationAuthorized || active.Authentication.Freshness != domain.AgentReadinessFresh {
		t.Fatalf("Settings reported the active account as unauthorized = %#v", active.Authentication)
	}

	// A review trigger a few seconds later asks the same launch readiness gate the
	// reviewer preflight uses. The readiness coordinator's cached observation was
	// never invalidated by the Settings ensure path, so the reviewer is rejected
	// even though Codex is logged in and Settings confirmed it.
	launch, err := fixture.service.EnsureAgentReadiness(context.Background(), string(domain.HarnessCodex), domain.AgentReadinessPurposeLaunch)
	if err != nil {
		t.Fatalf("review trigger launch readiness: %v", err)
	}
	if launch.Authentication.State != domain.AgentAuthenticationAuthorized {
		t.Fatalf("reviewer preflight used a stale unauthorized observation = %#v; "+
			"the active account is logged in and Settings confirmed it authorized "+
			"(EnsureCodexAccounts never invalidated the readiness coordinator cache)",
			launch.Authentication)
	}
}

// TestEnsureCodexAccountsDoesNotInvalidateReviewerReadinessCache is the isolated
// assertion: after the Settings ensure path re-verifies the active account to
// authorized, the readiness coordinator's cached auth observation must either be
// rechecked or invalidated so the reviewer preflight cannot reuse the stale
// unauthorized result. This pins the failing behavior independently of the
// out-of-band sign-in narrative above.
func TestEnsureCodexAccountsDoesNotInvalidateReviewerReadinessCache(t *testing.T) {
	fixture := newCodexReadinessPropagationFixture(t)

	// Prime the readiness coordinator with an unauthorized launch observation.
	if _, err := fixture.service.EnsureAgentReadiness(context.Background(), string(domain.HarnessCodex), domain.AgentReadinessPurposeLaunch); err != nil {
		t.Fatalf("initial launch readiness: %v", err)
	}
	readsBeforeSettings := fixture.refreshCapableReads()

	// The account recovers and the Settings ensure path confirms it authorized.
	fixture.signIn()
	if err := writeGlobalCredentialAtomic(fixture.manager.globalCredentialPath(), []byte("replacement-opaque-credential")); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.EnsureCodexAccounts(context.Background(), nil, false); err != nil {
		t.Fatalf("EnsureCodexAccounts: %v", err)
	}

	// The reviewer preflight reuses the readiness coordinator. It must not return
	// the stale unauthorized observation the prior launch check cached.
	launch, err := fixture.service.EnsureAgentReadiness(context.Background(), string(domain.HarnessCodex), domain.AgentReadinessPurposeLaunch)
	if err != nil {
		t.Fatalf("review trigger launch readiness: %v", err)
	}
	if launch.Authentication.State == domain.AgentAuthenticationUnauthorized {
		_ = readsBeforeSettings
		t.Fatalf("reviewer preflight returned stale unauthorized after Settings confirmed the account authorized = %#v", launch.Authentication)
	}
}
