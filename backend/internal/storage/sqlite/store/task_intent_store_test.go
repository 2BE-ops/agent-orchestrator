package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func cancelTask(t *testing.T, s *sqlite.Store, id string) domain.TaskIntent {
	t.Helper()
	i, err := s.ChangeTaskIntent(context.Background(), id, domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)})
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestTaskIntentCancellationIsRetainedAndFencesDescendants(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	createTask(t, s, "parent", taskDefinition())
	child := taskDefinition()
	child.ParentID = "parent"
	createTask(t, s, "child", child)
	createTask(t, s, "independent", taskDefinition())
	initial, err := s.GetTaskIntent(ctx, "child")
	if err != nil || initial.Version != 0 || initial.Intent != "run" || initial.Actor.ID != "human" {
		t.Fatalf("initial intent: %+v %v", initial, err)
	}
	cancelTask(t, s, "parent")
	if _, _, err := s.ReserveTask(ctx, taskReservation("blocked", "child")); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("cancelled ancestor admitted worker: %v", err)
	}
	reserveTask(t, s, "unrelated", "independent")
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if ancestor, err := reopened.CancelledTaskAncestor(ctx, "child"); err != nil || ancestor != "parent" {
		t.Fatalf("lost cancellation after restart: %q %v", ancestor, err)
	}
	if _, err := reopened.ChangeTaskIntent(ctx, "parent", domain.TaskIntentChange{Intent: "run", ExpectedVersion: 1, Mutation: taskMutation(1)}); err != nil {
		t.Fatal(err)
	}
	reserveTask(t, reopened, "resumed", "child")
	history, err := reopened.ListTaskIntents(ctx, "parent", 0, 20)
	if err != nil || len(history) != 2 || history[0].Intent != "cancel" || history[1].Intent != "run" || history[0].TaskRevision != 1 {
		t.Fatalf("lost explicit history: %+v %v", history, err)
	}
}

func TestTaskIntentFencesSeedsAndNativeWorkWithoutReleasingOwnership(t *testing.T) {
	ctx := context.Background()
	for _, seeded := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_seed", true: "after_seed"}[seeded], func(t *testing.T) {
			s := newTestStore(t)
			seedProject(t, s, "project")
			createTask(t, s, "work", taskDefinition())
			_, lease := reserveTask(t, s, "attempt", "work")
			rec, snapshot := workerSnapshot(t, s)
			rec.ProjectID = "project"
			if seeded {
				var err error
				rec, _, err = s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.HeartbeatAt)
				if err != nil {
					t.Fatal(err)
				}
			}
			cancelTask(t, s, "work")
			if !seeded {
				if _, _, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.HeartbeatAt); !errors.Is(err, ports.ErrTaskLeaseFenced) {
					t.Fatalf("cancelled intent created a worker: %v", err)
				}
			} else {
				for _, kind := range []string{"dispatch", "restore"} {
					if _, err := s.BeginTaskExecution(ctx, domain.TaskExecutionOperation{ID: kind, SessionID: rec.ID, Lease: lease.TaskLeaseToken, SourceOwner: rec.ControllerOwner(), Kind: kind, CreatedAt: lease.HeartbeatAt}); !errors.Is(err, ports.ErrTaskLeaseFenced) {
						t.Fatalf("cancelled intent began %s: %v", kind, err)
					}
				}
			}
			active, ok, err := s.GetActiveTaskLease(ctx, "work")
			if err != nil || !ok || active.TaskLeaseToken != lease.TaskLeaseToken {
				t.Fatalf("cancellation pretended worker stopped: %+v %v %v", active, ok, err)
			}
		})
	}
}

func TestTaskIntentAuthorityAndConcurrentFences(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	for _, role := range []string{"WORKER", "AGENT_MANAGER", "ORCHESTRATOR"} {
		mutation := taskMutation(1)
		mutation.Actor = domain.AdaptiveActor{Kind: role, ID: "intruder", SessionID: "missing"}
		if _, err := s.ChangeTaskIntent(ctx, "work", domain.TaskIntentChange{Intent: "cancel", Mutation: mutation}); !errors.Is(err, ports.ErrTaskForbidden) {
			t.Fatalf("%s changed planning: %v", role, err)
		}
	}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			_, err := s.ChangeTaskIntent(ctx, "work", domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	winners := 0
	for err := range errs {
		if err == nil {
			winners++
		} else if !errors.Is(err, ports.ErrTaskConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("control CAS winners: %d", winners)
	}
	if _, err := s.ReviseAcceptanceCriteria(ctx, "work", taskCriteria(), taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeTaskIntent(ctx, "work", domain.TaskIntentChange{Intent: "run", ExpectedVersion: 1, Mutation: taskMutation(1)}); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("stale planning resumed task: %v", err)
	}
}

func TestTaskIntentCancellationRetainsPendingNativeRecovery(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seed, lease := taskExecutionSeed(t, s)
	op := domain.TaskExecutionOperation{ID: "reserved-native", SessionID: seed.ID, Lease: lease.TaskLeaseToken, SourceOwner: seed.ControllerOwner(), Kind: "dispatch", CreatedAt: lease.HeartbeatAt}
	if _, err := s.BeginTaskExecution(ctx, op); err != nil {
		t.Fatal(err)
	}
	cancelTask(t, s, "work")
	if _, pending, err := s.PendingTaskExecution(ctx, seed.ID); err != nil || !pending {
		t.Fatalf("cancel discarded unresolved native operation: %v %v", pending, err)
	}
	seed.IsTerminated = true
	if err := s.UpdateSession(ctx, seed); err != nil {
		t.Fatal(err)
	}
	owner := seed.ControllerOwner()
	release := domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, SessionID: seed.ID, ObservedOwner: &owner, Reason: "Confirmed cancellation cleanup", Now: lease.ExpiresAt}
	if err := s.ReleaseTaskLease(ctx, release); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("cancel released unknown native operation: %v", err)
	}
	if err := s.ResolveTaskExecution(ctx, domain.TaskExecutionResolution{OperationID: op.ID, ObservedOwner: owner, Outcome: "terminated", Reason: "Confirmed native termination", CreatedAt: lease.ExpiresAt}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseTaskLease(ctx, release); err != nil {
		t.Fatal(err)
	}
}

func TestTaskIntentAuditFailureRollsBackAndHistoryIsImmutable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TRIGGER fail_intent_audit BEFORE INSERT ON adaptive_task_audit BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeTaskIntent(ctx, "work", domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)}); err == nil {
		t.Fatal("lost audit accepted")
	}
	intent, err := s.GetTaskIntent(ctx, "work")
	if err != nil || intent.Version != 0 {
		t.Fatalf("partial cancellation: %+v %v", intent, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_intent_audit`); err != nil {
		t.Fatal(err)
	}
	cancelTask(t, s, "work")
	for _, statement := range []string{`UPDATE adaptive_task_intents SET intent='run'`, `DELETE FROM adaptive_task_intents`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("history rewrite accepted: %s", statement)
		}
	}
}
