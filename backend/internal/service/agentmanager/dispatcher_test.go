package agentmanager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type inboxNative struct {
	store          *sqlite.Store
	starts         int
	ready          bool
	startErr       error
	readyErr       error
	blockedProject domain.ProjectID
	inputs         []domain.AgentManagerContext
	onSend         func(domain.AgentManagerContext)
	observation    ports.AgentManagerTransportResult
}

func (n *inboxNative) Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	n.starts++
	if n.startErr != nil {
		return domain.SessionRecord{}, 0, 0, n.startErr
	}
	configuration, err := n.store.GetAgentManagerConfiguration(ctx, cfg.ProjectID, cfg.ManagerController.ConfigurationVersion)
	if err != nil {
		return domain.SessionRecord{}, 0, 0, err
	}
	version, err := n.store.GetRegistryVersion(ctx, configuration.ControllerType.ID, configuration.ControllerType.Version)
	if err != nil {
		return domain.SessionRecord{}, 0, 0, err
	}
	snapshot := domain.WorkerConfiguration{SchemaVersion: 1, AgentType: configuration.ControllerType, Selection: *cfg.WorkerSelection, Effective: *version.Definition.AgentType, Origin: domain.RegistryUser, ActorID: configuration.Actor.ID, SystemPrompt: "Pinned routing instructions", CreatedAt: time.Now().UTC()}
	snapshot.ContentHash = snapshot.Hash()
	rec, _, err := n.store.CreateAgentManagerSession(ctx, *cfg.ManagerController, domain.SessionRecord{ProjectID: cfg.ProjectID, Kind: domain.KindAgentManager, Harness: snapshot.Effective.Harness, Mode: snapshot.Effective.SessionMode, Metadata: domain.SessionMetadata{Permissions: snapshot.Effective.Config.Permissions}, CreatedAt: time.Now().UTC()}, snapshot, time.Now().UTC())
	if err != nil {
		return rec, 0, 0, err
	}
	op := domain.AgentManagerExecutionOperation{ID: "native-" + string(rec.ID), ControllerID: cfg.ManagerController.ID, SessionID: rec.ID, SourceOwner: rec.ControllerOwner(), Kind: "dispatch", CreatedAt: time.Now().UTC()}
	if _, err := n.store.BeginAgentManagerExecution(ctx, op); err != nil {
		return rec, 0, 0, err
	}
	rec.Metadata.ControllerGeneration = op.ID
	if err := n.store.UpdateSession(ctx, rec); err != nil {
		return rec, 0, 0, err
	}
	err = n.store.ResolveAgentManagerExecution(ctx, domain.AgentManagerExecutionResolution{OperationID: op.ID, ObservedOwner: rec.ControllerOwner(), Outcome: "connected", Reason: "Observed test native controller", CreatedAt: time.Now().UTC()})
	return rec, 0, 0, err
}

func (n *inboxNative) AgentManagerTargetReady(ctx context.Context, id domain.SessionID) (bool, error) {
	if n.blockedProject != "" {
		rec, _, err := n.store.GetSession(ctx, id)
		if err != nil {
			return false, err
		}
		if rec.ProjectID == n.blockedProject {
			return false, nil
		}
	}
	return n.ready, n.readyErr
}

func (n *inboxNative) DeliverAgentManagerContext(_ context.Context, _ domain.AgentManagerDelivery, sealed domain.AgentManagerContext) ports.AgentManagerTransportResult {
	n.inputs = append(n.inputs, sealed)
	if n.onSend != nil {
		n.onSend(sealed)
	}
	return n.observation
}

func inboxFixture(t *testing.T, clearance domain.ContextClass) (*InboxDispatcher, *inboxNative, *sqlite.Store) {
	t.Helper()
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	if _, err := s.CreateRegistryEntry(ctx, "manager-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Manager", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeChat, Config: domain.AgentConfig{Permissions: domain.PermissionModeAuto}, MaxParallelWorkers: 1, MaxContextClass: clearance}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Configure routing"}); err != nil {
		t.Fatal(err)
	}
	inboxProject(t, s, "project")
	native := &inboxNative{store: s, ready: true, observation: ports.AgentManagerTransportResult{State: "handed_off", Reason: "Native accepted input"}}
	d, err := NewInboxDispatcher(s, NewWithRuntime(s, native), native, domain.AgentManagerToolPaths{SchemaVersion: 1, Executable: "ao fixture.exe", RunFile: "isolated running.json"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return d, native, s
}

func inboxProject(t *testing.T, s *sqlite.Store, id domain.ProjectID) {
	t.Helper()
	ctx := context.Background()
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: string(id), Path: t.TempDir(), Kind: domain.ProjectKindScratch, RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfigureAgentManager(ctx, id, domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: "manager-type", AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Enable routing"}); err != nil {
		t.Fatal(err)
	}
}

func inboxRequest(t *testing.T, s *sqlite.Store, project domain.ProjectID, id string, class domain.ContextClass, engagement string) domain.AgentManagerRequest {
	t.Helper()
	ctx := context.Background()
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "checked", Requirement: "Independent verification", EvidenceKind: "manual"}}}
	if _, err := s.CreateAdaptiveTask(ctx, id, project, domain.TaskDefinition{Title: "Route bounded work", Brief: "Exact task " + id, Classification: class, EngagementID: engagement, MaxAttempts: 1}, &criteria, domain.TaskMutation{Actor: actor, Reason: "Plan task"}); err != nil {
		t.Fatal(err)
	}
	request, _, err := s.EnqueueAgentManagerRequest(ctx, domain.AgentManagerEnqueue{ID: id + "-request", ProjectID: project, TaskID: id, TaskRevision: 1, ConfigurationVersion: 1, Actor: actor, Reason: "Choose worker", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestManagerInboxStartsOnceAndSealsToolsBeforeNativeOutput(t *testing.T) {
	ctx := context.Background()
	d, native, s := inboxFixture(t, domain.ContextMission)
	request := inboxRequest(t, s, "project", "first", domain.ContextEngagement, "client-a")
	native.ready = false
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if native.starts != 1 || len(native.inputs) != 0 {
		t.Fatal("readiness consumed a send or failed to admit native Manager")
	}
	contexts, err := s.ListAgentManagerContexts(ctx, "project", request.ID)
	if err != nil || len(contexts) != 0 {
		t.Fatalf("unready controller consumed context: %+v %v", contexts, err)
	}
	native.ready = true
	native.onSend = func(sealed domain.AgentManagerContext) {
		retained, err := d.controllers.Context(ctx, "project", request.ID, sealed.ID)
		if err != nil || retained.ContentHash != sealed.ContentHash || retained.Tools == nil || !strings.Contains(retained.Prompt, "AO_MANAGER_TOOLS_JSON") {
			t.Fatalf("native input was not sealed with tools: %+v %v", retained, err)
		}
		receipt, err := d.controllers.Propose(ctx, sealed.SessionID, sealed.RequestID, ProposalInput{SourceGeneration: sealed.NativeGeneration, IdempotencyKey: "native-output", Raw: `{"schemaVersion":1,"action":"select_existing","agentTypeId":"candidate","agentTypeVersion":1,"rationale":"Await compatibility assessment","candidates":[]}`})
		if err != nil || !receipt.Created || receipt.Proposal.ContextHash != sealed.ContentHash || receipt.Proposal.Classification != domain.ContextEngagement {
			t.Fatalf("native output raced transport commit: %+v %v", receipt, err)
		}
	}
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if native.starts != 1 || len(native.inputs) != 1 {
		t.Fatal("repeated native input or controller launch")
	}
	deliveries, err := d.controllers.Deliveries(ctx, "project", request.ID)
	if err != nil || len(deliveries) != 1 || deliveries[0].State != "handed_off" {
		t.Fatalf("native outcome lost: %+v %v", deliveries, err)
	}
	if _, err := d.controllers.Context(ctx, "foreign", request.ID, native.inputs[0].ID); err == nil {
		t.Fatal("cross-project native input read")
	}
}

func TestManagerInboxClassRefusalDoesNotStarveEligibleWork(t *testing.T) {
	ctx := context.Background()
	d, native, s := inboxFixture(t, domain.ContextTechnical)
	private := inboxRequest(t, s, "project", "private", domain.ContextMission, "")
	public := inboxRequest(t, s, "project", "public", domain.ContextTechnical, "")
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	resolution, found, err := s.GetAgentManagerRequestResolution(ctx, "project", private.ID)
	if err != nil || !found || resolution.Outcome != "needs_human" {
		t.Fatalf("class boundary silently stalled: %+v %v", resolution, err)
	}
	if len(native.inputs) != 1 || native.inputs[0].RequestID != public.ID || strings.Contains(native.inputs[0].Prompt, "Exact task private") {
		t.Fatal("class rejection leaked input or starved eligible work")
	}
}

func TestManagerInboxRecoversUnknownSendsWithoutRepeatingInput(t *testing.T) {
	ctx := context.Background()
	d, native, s := inboxFixture(t, domain.ContextMission)
	request := inboxRequest(t, s, "project", "unknown", domain.ContextTechnical, "")
	receipt, err := d.controllers.StartController(ctx, domain.AdaptiveActor{Kind: "SYSTEM", ID: "test"}, "project", ControllerStartInput{ID: "existing-controller", ConfigurationVersion: 1, Reason: "Start retained controller"})
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := s.GetSession(ctx, receipt.State.Dispatch.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.BeginAgentManagerDelivery(ctx, domain.AgentManagerContextSeal{ID: "uncertain-context", ProjectID: "project", RequestID: request.ID, SessionID: rec.ID, SourceOwner: rec.ControllerOwner(), Now: time.Now().UTC()}, "uncertain-send"); err != nil {
		t.Fatal(err)
	}
	inboxRequest(t, s, "project", "later", domain.ContextTechnical, "")
	if err := d.reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	deliveries, err := s.ListAgentManagerDeliveries(ctx, "project", request.ID)
	if err != nil || len(deliveries) != 1 || deliveries[0].State != "uncertain" || len(native.inputs) != 0 || native.starts != 1 {
		t.Fatalf("recovery repeated unknown input: %+v %v", deliveries, err)
	}
}

func TestManagerInboxUnknownStartAndReadinessNeverGrantReplacement(t *testing.T) {
	for _, reason := range []string{"start", "readiness"} {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			d, native, s := inboxFixture(t, domain.ContextTechnical)
			request := inboxRequest(t, s, "project", "waiting", domain.ContextTechnical, "")
			if reason == "start" {
				native.startErr = errors.New("unknown native effect")
			} else {
				native.readyErr = errors.New("probe unavailable")
			}
			for range 3 {
				if err := d.dispatch(ctx); err != nil {
					t.Fatal(err)
				}
			}
			contexts, err := s.ListAgentManagerContexts(ctx, "project", request.ID)
			if err != nil || len(contexts) != 0 || len(native.inputs) != 0 || native.starts != 1 {
				t.Fatalf("unknown native state consumed or repeated work: %+v %v", contexts, err)
			}
		})
	}
}

func TestManagerInboxCursorAdvancesPastBlockedProject(t *testing.T) {
	ctx := context.Background()
	d, native, s := inboxFixture(t, domain.ContextTechnical)
	native.blockedProject = "project"
	for i := range 16 {
		inboxRequest(t, s, "project", fmt.Sprintf("blocked-%02d", i), domain.ContextTechnical, "")
	}
	inboxProject(t, s, "other-project")
	other := inboxRequest(t, s, "other-project", "ready", domain.ContextTechnical, "")
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	cursor, err := s.AgentManagerDispatchCursor(ctx)
	if err != nil || cursor != 16 || len(native.inputs) != 0 {
		t.Fatalf("bounded scan: %d %v", cursor, err)
	}
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	cursor, err = s.AgentManagerDispatchCursor(ctx)
	if err != nil || cursor != 0 || len(native.inputs) != 1 || native.inputs[0].RequestID != other.ID {
		t.Fatalf("blocked project starved another: %d %v", cursor, err)
	}
}

func TestManagerInboxSupersededTaskHasNoNativeEffects(t *testing.T) {
	ctx := context.Background()
	d, native, s := inboxFixture(t, domain.ContextTechnical)
	request := inboxRequest(t, s, "project", "replanned", domain.ContextTechnical, "")
	if _, err := s.ReviseAdaptiveTask(ctx, "replanned", domain.TaskDefinition{Title: "New task", Brief: "New intent", MaxAttempts: 1}, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Replan", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	resolution, found, err := s.GetAgentManagerRequestResolution(ctx, "project", request.ID)
	if err != nil || !found || resolution.Outcome != "superseded" || native.starts != 0 {
		t.Fatalf("stale task launched: %+v %v", resolution, err)
	}
}

func TestManagerInboxRetainsObservedOutcomeAfterCancellationOrMalformedTransport(t *testing.T) {
	for _, kind := range []string{"cancelled context", "invalid observation"} {
		t.Run(kind, func(t *testing.T) {
			d, native, s := inboxFixture(t, domain.ContextTechnical)
			request := inboxRequest(t, s, "project", "retained", domain.ContextTechnical, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := "handed_off"
			if kind == "cancelled context" {
				native.onSend = func(domain.AgentManagerContext) { cancel() }
			} else {
				native.observation = ports.AgentManagerTransportResult{State: "unrecognised", Reason: "bad transport"}
				want = "uncertain"
			}
			if err := d.deliver(ctx, request); err != nil {
				t.Fatal(err)
			}
			deliveries, err := s.ListAgentManagerDeliveries(context.Background(), "project", request.ID)
			if err != nil || len(deliveries) != 1 || deliveries[0].State != want {
				t.Fatalf("observation was lost or retryable: %+v %v", deliveries, err)
			}
		})
	}
}
