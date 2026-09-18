package sessionmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type taskWorkerFixture struct {
	manager  *Manager
	store    *sqlite.Store
	lease    domain.TaskLease
	cfg      ports.SpawnConfig
	runtime  *fakeRuntime
	launcher *recordingLauncher
	native   *workerNative
}

func newTaskWorkerFixture(t *testing.T, mode domain.SessionMode) taskWorkerFixture {
	t.Helper()
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	project := domain.ProjectRecord{ID: "task-project", Path: t.TempDir(), Kind: domain.ProjectKindScratch, RegisteredAt: time.Now().UTC()}
	if err := s.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	native := &workerNative{}
	registry := registrysvc.NewWithNative(s, native)
	entry, err := registry.Create(ctx, domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, domain.RegistryAgentType, registrysvc.CreateInput{Metadata: domain.RegistryMetadata{Name: "Task worker", Enabled: true}, Definition: domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessClaudeCode, SessionMode: mode, Config: domain.AgentConfig{Model: "provider/reviewer", Permissions: domain.PermissionModeAuto}, Instructions: "Perform the bounded task", MaxParallelWorkers: 2}}, Reason: "Author worker"})
	if err != nil {
		t.Fatal(err)
	}
	selection := &domain.WorkerSelection{AgentTypeID: entry.Entry.ID, Version: 1}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "review", Requirement: "Provide verified evidence", EvidenceKind: "review"}}}
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	if _, err := s.CreateAdaptiveTask(ctx, "task", "task-project", domain.TaskDefinition{Title: "Bounded task", Brief: "Follow fixed criteria", MaxAttempts: 3, RequestedWorker: selection}, &criteria, domain.TaskMutation{Actor: actor, Reason: "Plan task"}); err != nil {
		t.Fatal(err)
	}
	_, lease, err := s.ReserveTask(ctx, domain.TaskReservation{ID: "attempt", TaskID: "task", LaunchIntentID: "launch-intent", HolderID: "scheduler", Mutation: domain.TaskMutation{Actor: actor, ExpectedRevision: 1, Reason: "Dispatch task"}, Now: time.Now().UTC(), TTL: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{}
	launcher := &recordingLauncher{}
	messenger := &fakeMessenger{}
	manager := New(Deps{Store: s, Runtime: runtime, Agents: singleAgent{agent: &recordingAgent{}}, Workspace: &fakeWorkspace{path: t.TempDir()}, Messenger: messenger, Lifecycle: lifecycle.New(s, messenger), Chat: launcher, DataDir: t.TempDir(), WorkerConfigurations: registry, LookPath: func(string) (string, error) { return "test-agent", nil }})
	return taskWorkerFixture{manager: manager, store: s, lease: lease, cfg: ports.SpawnConfig{ProjectID: "task-project", Kind: domain.KindWorker, WorkerSelection: selection, TaskLease: &lease.TaskLeaseToken, Prompt: "Do the task"}, runtime: runtime, launcher: launcher, native: native}
}

func TestTaskWorkerLaunchReplayAndReservedRestore(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			f := newTaskWorkerFixture(t, mode)
			rec, _, _, err := f.manager.Spawn(ctx, f.cfg)
			if err != nil {
				t.Fatal(err)
			}
			dispatch, ok, err := f.store.GetTaskWorkerDispatch(ctx, f.lease.AttemptID)
			if err != nil || !ok || dispatch.SessionID != rec.ID {
				t.Fatalf("worker missing task association: %+v %v %v", dispatch, ok, err)
			}
			if _, pending, err := f.store.PendingTaskExecution(ctx, rec.ID); err != nil || pending {
				t.Fatalf("successful launch not resolved: %v %v", pending, err)
			}
			checks, runtimeStarts, chatStarts := f.native.checks, f.runtime.created, len(f.launcher.started)
			f.native.unavailable = true
			replay, _, _, err := f.manager.Spawn(ctx, f.cfg)
			if err != nil || replay.ID != rec.ID || f.runtime.created != runtimeStarts || len(f.launcher.started) != chatStarts || f.native.checks != checks {
				t.Fatalf("dispatch replay performed native work: %+v %v", replay, err)
			}
			f.native.unavailable = false
			rec.IsTerminated = true
			rec.Metadata.AgentSessionID = "native-task"
			if err := f.store.UpdateSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			restored, err := f.manager.RestoreWithMode(ctx, rec.ID)
			if err != nil {
				t.Fatal(err)
			}
			if restored.Session.IsTerminated {
				t.Fatal("reserved restore remained terminated")
			}
			if _, pending, err := f.store.PendingTaskExecution(ctx, rec.ID); err != nil || pending {
				t.Fatalf("successful restore not resolved: %v %v", pending, err)
			}
		})
	}
}

func TestReleasedTaskWorkerCannotRestoreOrReplay(t *testing.T) {
	ctx := context.Background()
	f := newTaskWorkerFixture(t, domain.SessionModeTUI)
	rec, _, _, err := f.manager.Spawn(ctx, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec.IsTerminated = true
	if err := f.store.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	owner := rec.ControllerOwner()
	if err := f.store.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: f.lease.TaskLeaseToken, SessionID: rec.ID, ObservedOwner: &owner, Reason: "Test confirmed termination", Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	starts := f.runtime.created
	if _, err := f.manager.RestoreWithMode(ctx, rec.ID); err == nil {
		t.Fatal("released historical worker restored")
	}
	if _, _, _, err := f.manager.Spawn(ctx, f.cfg); err == nil {
		t.Fatal("released dispatch replay accepted")
	}
	if f.runtime.created != starts {
		t.Fatal("released worker launched native process")
	}
}

func TestTaskWorkerUncertainLaunchRetainsExecutionReservation(t *testing.T) {
	ctx := context.Background()
	f := newTaskWorkerFixture(t, domain.SessionModeTUI)
	f.runtime.createErr = errors.New("runtime launch response lost")
	if _, _, _, err := f.manager.Spawn(ctx, f.cfg); err == nil {
		t.Fatal("launch failure ignored")
	}
	dispatch, ok, err := f.store.GetTaskWorkerDispatch(ctx, f.lease.AttemptID)
	if err != nil || !ok {
		t.Fatalf("failed launch lost association: %+v %v %v", dispatch, ok, err)
	}
	if _, pending, err := f.store.PendingTaskExecution(ctx, dispatch.SessionID); err != nil || !pending {
		t.Fatalf("unknown launch released reservation: %v %v", pending, err)
	}
	if _, active, err := f.store.GetActiveTaskLease(ctx, "task"); err != nil || !active {
		t.Fatalf("failed launch reopened admission: %v %v", active, err)
	}
	starts := f.runtime.created
	if _, _, _, err := f.manager.Spawn(ctx, f.cfg); err != nil {
		t.Fatal(err)
	}
	if f.runtime.created != starts {
		t.Fatal("retry blindly relaunched uncertain execution")
	}
}

func TestTaskWorkerRejectsLegacyOrExpiredDispatchBeforeNativeWork(t *testing.T) {
	ctx := context.Background()
	f := newTaskWorkerFixture(t, domain.SessionModeTUI)
	cfg := f.cfg
	cfg.WorkerSelection = nil
	if _, _, _, err := f.manager.Spawn(ctx, cfg); err == nil {
		t.Fatal("task bypassed immutable configuration")
	}
	f.manager.clock = func() time.Time { return f.lease.ExpiresAt }
	if _, _, _, err := f.manager.Spawn(ctx, f.cfg); err == nil {
		t.Fatal("expired unseeded dispatch launched")
	}
	if f.runtime.created != 0 || f.native.checks != 0 {
		t.Fatal("invalid dispatch reached native side effects")
	}
}
