package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func controlTestActor() domain.AdaptiveActor {
	return domain.AdaptiveActor{Kind: "USER", ID: "local-user"}
}

// fakeTerminator records kill requests without proving anything stopped.
type fakeTerminator struct {
	failures map[domain.SessionID]error
	requests []domain.SessionID
}

func (f *fakeTerminator) Kill(_ context.Context, id domain.SessionID) error {
	f.requests = append(f.requests, id)
	if err, ok := f.failures[id]; ok {
		return err
	}
	return nil
}

func controlServiceTest(t *testing.T) *Manager {
	t.Helper()
	store := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{ID: "project", Path: "/tmp/project", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return New(store)
}

func TestControlServiceMapsStoreErrorsToEnvelopes(t *testing.T) {
	ctx := context.Background()
	manager := controlServiceTest(t)
	if _, err := manager.SetControl(ctx, controlTestActor(), "ghost", StateInput{State: "paused", Reason: "No project"}); codeOf(err) != "PROJECT_NOT_FOUND" {
		t.Fatalf("unknown project: %v", err)
	}
	if _, err := manager.SetControl(ctx, controlTestActor(), "project", StateInput{State: "hibernating", Reason: "Bad state"}); codeOf(err) != "INVALID_CONTROL_INPUT" {
		t.Fatalf("unknown state: %v", err)
	}
	if _, err := manager.SetControl(ctx, domain.AdaptiveActor{Kind: "AGENT_MANAGER", ID: "mgr"}, "project", StateInput{State: "paused", Reason: "Manager self-pause"}); codeOf(err) != "INVALID_CONTROL_INPUT" {
		t.Fatalf("manager actor: %v", err)
	}
	view, err := manager.SetControl(ctx, controlTestActor(), "project", StateInput{State: "paused", Reason: "Evening maintenance"})
	if err != nil || view.Control.State != domain.ProjectPaused || view.EffectiveState != domain.ProjectPaused {
		t.Fatalf("pause: %+v %v", view, err)
	}
	if _, err := manager.SetControl(ctx, controlTestActor(), "project", StateInput{State: "draining", Reason: "Skip pause"}); codeOf(err) != "PROJECT_CONTROL_CONFLICT" {
		t.Fatalf("illegal transition: %v", err)
	}
	read, err := manager.Control(ctx, "project")
	if err != nil || read.Control.State != domain.ProjectPaused {
		t.Fatalf("read: %+v %v", read, err)
	}
	if _, err := manager.DryRun(ctx, "project", domain.DryRunRequest{}); codeOf(err) != "INVALID_DRY_RUN_INPUT" {
		t.Fatalf("empty dry run: %v", err)
	}
	if _, err := manager.List(ctx, "project", "", 0); codeOf(err) != "INVALID_NEEDS_HUMAN_PAGE" {
		t.Fatalf("bad page: %v", err)
	}
}

func TestControlServiceNeedsHumanLifecycle(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/tmp/project", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "manual", Requirement: "Human checks", EvidenceKind: "manual"}}}
	if _, err := store.CreateAdaptiveTask(ctx, "task-1", "project", domain.TaskDefinition{Title: "Blocked work", Brief: "Needs sign-off", MaxAttempts: 2}, &criteria, domain.TaskMutation{Actor: controlTestActor(), Reason: "Plan"}); err != nil {
		t.Fatal(err)
	}
	manager := New(store)
	raised, err := manager.Raise(ctx, controlTestActor(), "task-1", NeedsHumanInput{ReasonCode: "approval_required", Detail: "Needs a release sign-off"})
	if err != nil || raised.TaskID != "task-1" || raised.ID == "" {
		t.Fatalf("raise: %+v %v", raised, err)
	}
	if _, err := manager.Raise(ctx, controlTestActor(), "task-1", NeedsHumanInput{ReasonCode: "approval_required", Detail: "Second"}); codeOf(err) != "NEEDS_HUMAN_CONFLICT" {
		t.Fatalf("duplicate raise: %v", err)
	}
	items, err := manager.List(ctx, "project", "", 10)
	if err != nil || len(items) != 1 || items[0].ID != raised.ID {
		t.Fatalf("list: %+v %v", items, err)
	}
	resolved, err := manager.Resolve(ctx, controlTestActor(), "task-1", ResolveInput{Resolution: "Approved for this release"})
	if err != nil || resolved.Resolution == nil || resolved.Resolution.Resolution != "Approved for this release" {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
	if _, err := manager.Resolve(ctx, controlTestActor(), "task-1", ResolveInput{Resolution: "Again"}); codeOf(err) != "NEEDS_HUMAN_NOT_FOUND" {
		t.Fatalf("double resolve: %v", err)
	}
	if _, err := manager.Raise(ctx, controlTestActor(), "ghost", NeedsHumanInput{ReasonCode: "approval_required", Detail: "No task"}); codeOf(err) != "TASK_NOT_FOUND" {
		t.Fatalf("unknown task: %v", err)
	}
}

func TestControlServiceCancelAllReportsTerminations(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/tmp/project", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "manual", Requirement: "Human checks", EvidenceKind: "manual"}}}
	if _, err := store.CreateAdaptiveTask(ctx, "task-1", "project", domain.TaskDefinition{Title: "Held work", Brief: "Leased", MaxAttempts: 2}, &criteria, domain.TaskMutation{Actor: controlTestActor(), Reason: "Plan"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateAdaptiveTask(ctx, "task-2", "project", domain.TaskDefinition{Title: "Free work", Brief: "Unleased", MaxAttempts: 2}, nil, domain.TaskMutation{Actor: controlTestActor(), Reason: "Plan"}); err != nil {
		t.Fatal(err)
	}
	reservation := domain.TaskReservation{ID: "attempt-1", TaskID: "task-1", LaunchIntentID: "launch-1", HolderID: "scheduler", Mutation: domain.TaskMutation{Actor: controlTestActor(), Reason: "Dispatch", ExpectedRevision: 1}, Now: time.Now().UTC(), TTL: time.Minute}
	_, lease, err := store.ReserveTask(ctx, reservation)
	if err != nil {
		t.Fatal(err)
	}
	// A real configured dispatch seeds the session and links it to the
	// attempt, so the termination candidate set is exercised end to end.
	entry, err := store.CreateRegistryEntry(ctx, "control-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Bounded", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, Instructions: "Do bounded work", MaxParallelWorkers: 2, SessionMode: domain.SessionModeTUI, Config: domain.AgentConfig{Permissions: domain.PermissionModeAuto}}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Configure"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.GetRegistryVersion(ctx, entry.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := domain.WorkerConfiguration{SchemaVersion: 1, AgentType: domain.WorkerDefinitionRef{ID: entry.ID, Version: 1, Name: entry.Metadata.Name, ContentHash: version.ContentHash}, Selection: domain.WorkerSelection{AgentTypeID: entry.ID}, Effective: *version.Definition.AgentType, Origin: domain.RegistryUser, ActorID: "human", SystemPrompt: "Fixed project instructions", CreatedAt: time.Now().UTC()}
	snapshot.ContentHash = snapshot.Hash()
	rec := domain.SessionRecord{ProjectID: "project", Kind: domain.KindWorker, Harness: snapshot.Effective.Harness, Mode: snapshot.Effective.SessionMode, CreatedAt: time.Now().UTC()}
	rec.Metadata = domain.SessionMetadata{Permissions: snapshot.Effective.Config.Permissions}
	seed, _, err := store.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	terminator := &fakeTerminator{failures: map[domain.SessionID]error{seed.ID: errors.New("session refused termination")}}
	manager := New(store, WithTerminator(terminator))
	result, err := manager.CancelWork(ctx, controlTestActor(), "project", CancelInput{Scope: "all", Reason: "Stop everything"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Result.Cancelled) != 2 || len(result.Result.Retained) != 0 {
		t.Fatalf("cancel-all result: %+v", result.Result)
	}
	if !result.KillService || len(result.Terminations) != 1 || result.Terminations[0].SessionID != seed.ID || result.Terminations[0].Error == "" {
		t.Fatalf("terminations not reported honestly: %+v", result.Terminations)
	}
	if len(terminator.requests) != 1 || terminator.requests[0] != seed.ID {
		t.Fatalf("kill requests: %+v", terminator.requests)
	}
	// Without a terminator the report says so instead of inventing results.
	bare, err := New(store).CancelWork(ctx, controlTestActor(), "project", CancelInput{Scope: "pending", Reason: "Nothing pending"})
	if err != nil || bare.KillService || len(bare.Terminations) != 0 {
		t.Fatalf("bare cancel: %+v %v", bare, err)
	}
	if _, err := manager.CancelWork(ctx, controlTestActor(), "ghost", CancelInput{Scope: "pending", Reason: "No project"}); codeOf(err) != "PROJECT_NOT_FOUND" {
		t.Fatalf("unknown project: %v", err)
	}
}

func codeOf(err error) string {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}
