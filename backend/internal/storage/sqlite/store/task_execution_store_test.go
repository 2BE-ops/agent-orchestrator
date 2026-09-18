package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func taskExecutionSeed(t *testing.T, s *sqlite.Store) (domain.SessionRecord, domain.TaskLease) {
	t.Helper()
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	_, lease := reserveTask(t, s, "attempt", "work")
	rec, snapshot := workerSnapshot(t, s)
	rec.ProjectID = "project"
	seed, _, err := s.CreateTaskWorkerSession(context.Background(), lease.TaskLeaseToken, rec, snapshot, lease.HeartbeatAt)
	if err != nil {
		t.Fatal(err)
	}
	return seed, lease
}

func TestTaskExecutionGuardSurvivesRestartAndRequiresVerifiedResolution(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seed, lease := taskExecutionSeed(t, s)
	op := domain.TaskExecutionOperation{ID: "native-launch", SessionID: seed.ID, Lease: lease.TaskLeaseToken, SourceOwner: seed.ControllerOwner(), Kind: "dispatch", CreatedAt: lease.HeartbeatAt}
	if created, err := s.BeginTaskExecution(ctx, op); err != nil || !created {
		t.Fatalf("guard: %v %v", created, err)
	}
	if created, err := s.BeginTaskExecution(ctx, op); err != nil || created {
		t.Fatalf("replay permits duplicate launch: %v %v", created, err)
	}
	op.ID = "second-launch"
	if _, err := s.BeginTaskExecution(ctx, op); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("overlapping native operation: %v", err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	pending, ok, err := reopened.PendingTaskExecution(ctx, seed.ID)
	if err != nil || !ok || pending.ID != "native-launch" {
		t.Fatalf("lost pending execution: %+v %v %v", pending, ok, err)
	}
	// Even a terminated row cannot release unresolved native side effects.
	seed.IsTerminated = true
	if err := reopened.UpdateSession(ctx, seed); err != nil {
		t.Fatal(err)
	}
	owner := seed.ControllerOwner()
	release := domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, SessionID: seed.ID, ObservedOwner: &owner, Reason: "Confirm task worker termination", Now: lease.ExpiresAt.Add(time.Hour)}
	if err := reopened.ReleaseTaskLease(ctx, release); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("unknown native outcome released: %v", err)
	}
	resolution := domain.TaskExecutionResolution{OperationID: pending.ID, ObservedOwner: pending.SourceOwner, Outcome: "terminated", Reason: "Verified native teardown", CreatedAt: release.Now}
	if err := reopened.ResolveTaskExecution(ctx, resolution); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("stale owner proof accepted: %v", err)
	}
	resolution.ObservedOwner = owner
	if err := reopened.ResolveTaskExecution(ctx, resolution); err != nil {
		t.Fatal(err)
	}
	if err := reopened.ResolveTaskExecution(ctx, resolution); err != nil {
		t.Fatalf("resolution replay: %v", err)
	}
	if err := reopened.ReleaseTaskLease(ctx, release); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := reopened.PendingTaskExecution(ctx, seed.ID); err != nil || ok {
		t.Fatalf("resolved execution still pending: %v %v", ok, err)
	}
}

func TestTaskExecutionRestoreAndReleaseCannotBothWin(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seed, lease := taskExecutionSeed(t, s)
	seed.IsTerminated = true
	if err := s.UpdateSession(ctx, seed); err != nil {
		t.Fatal(err)
	}
	owner := seed.ControllerOwner()
	op := domain.TaskExecutionOperation{ID: "restore", SessionID: seed.ID, Lease: lease.TaskLeaseToken, SourceOwner: owner, Kind: "restore", CreatedAt: lease.ExpiresAt}
	release := domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, SessionID: seed.ID, ObservedOwner: &owner, Reason: "Confirmed stopped before reassignment", Now: op.CreatedAt}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Go(func() { _, err := s.BeginTaskExecution(ctx, op); errs <- err })
	wg.Go(func() { errs <- s.ReleaseTaskLease(ctx, release) })
	wg.Wait()
	close(errs)
	winners := 0
	for err := range errs {
		if err == nil {
			winners++
		} else if !errors.Is(err, ports.ErrTaskLeaseFenced) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("restore/reassignment winners=%d", winners)
	}
}

func TestTaskExecutionDatabaseFencesResurrectionAndDirectRelease(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seed, lease := taskExecutionSeed(t, s)
	seed.IsTerminated = true
	if err := s.UpdateSession(ctx, seed); err != nil {
		t.Fatal(err)
	}
	seed.IsTerminated = false
	if err := s.UpdateSession(ctx, seed); err == nil {
		t.Fatal("unreserved task worker resurrected")
	}
	seed.IsTerminated = true
	op := domain.TaskExecutionOperation{ID: "restore", SessionID: seed.ID, Lease: lease.TaskLeaseToken, SourceOwner: seed.ControllerOwner(), Kind: "restore", CreatedAt: lease.HeartbeatAt}
	if _, err := s.BeginTaskExecution(ctx, op); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `UPDATE adaptive_task_leases SET released_at=CURRENT_TIMESTAMP`); err == nil {
		t.Fatal("direct SQL bypassed unresolved launch fence")
	}
	seed.IsTerminated = false
	if err := s.UpdateSession(ctx, seed); err != nil {
		t.Fatalf("reserved restore rejected: %v", err)
	}
	if err := s.ResolveTaskExecution(ctx, domain.TaskExecutionResolution{OperationID: op.ID, ObservedOwner: seed.ControllerOwner(), Outcome: "connected", Reason: "Confirmed native controller connected", CreatedAt: lease.HeartbeatAt}); err != nil {
		t.Fatal(err)
	}
	seed.IsTerminated = true
	if err := s.UpdateSession(ctx, seed); err != nil {
		t.Fatal(err)
	}
	owner := seed.ControllerOwner()
	if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, SessionID: seed.ID, ObservedOwner: &owner, Reason: "Confirmed subsequent shutdown", Now: lease.HeartbeatAt}); err != nil {
		t.Fatal(err)
	}
	seed.IsTerminated = false
	if err := s.UpdateSession(ctx, seed); err == nil {
		t.Fatal("released historical worker resurrected")
	}
}

func TestTaskExecutionResolutionAuditFailureKeepsReservation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seed, lease := taskExecutionSeed(t, s)
	op := domain.TaskExecutionOperation{ID: "launch", SessionID: seed.ID, Lease: lease.TaskLeaseToken, SourceOwner: seed.ControllerOwner(), Kind: "dispatch", CreatedAt: lease.HeartbeatAt}
	if _, err := s.BeginTaskExecution(ctx, op); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_execution_audit BEFORE INSERT ON adaptive_task_audit BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	before, err := s.EventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveTaskExecution(ctx, domain.TaskExecutionResolution{OperationID: op.ID, ObservedOwner: seed.ControllerOwner(), Outcome: "connected", Reason: "Confirmed native connection", CreatedAt: op.CreatedAt}); err == nil {
		t.Fatal("lost resolution audit accepted")
	}
	if _, ok, err := s.PendingTaskExecution(ctx, seed.ID); err != nil || !ok {
		t.Fatalf("failed resolution cleared reservation: %v %v", ok, err)
	}
	after, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(after) != len(before) {
		t.Fatalf("rolled back resolution emitted CDC: %d -> %d %v", len(before), len(after), err)
	}
}
