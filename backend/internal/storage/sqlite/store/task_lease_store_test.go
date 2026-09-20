package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func taskReservation(id, taskID string) domain.TaskReservation {
	return domain.TaskReservation{ID: id, TaskID: taskID, LaunchIntentID: "launch-" + id, HolderID: "scheduler-1", Mutation: taskMutation(1), Now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC), TTL: time.Minute}
}

func reserveTask(t *testing.T, s *sqlite.Store, id, taskID string) (domain.TaskAttempt, domain.TaskLease) {
	t.Helper()
	attempt, lease, err := s.ReserveTask(context.Background(), taskReservation(id, taskID))
	if err != nil {
		t.Fatal(err)
	}
	return attempt, lease
}

func TestTaskLeaseFreezesVersionsBeforeWorkerAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	createTask(t, s, "dependency", taskDefinition())
	definition := taskDefinition()
	definition.Dependencies = []string{"dependency"}
	createTask(t, s, "work", definition)
	attempt, lease := reserveTask(t, s, "attempt", "work")
	if attempt.CriteriaVersion != 1 || attempt.TaskRevision != 1 || len(attempt.Dependencies) != 1 || attempt.Dependencies[0].Revision != 1 || attempt.Dependencies[0].ContentHash == "" || lease.LastActivityAt != nil {
		t.Fatalf("incomplete pins: %+v %+v", attempt, lease)
	}
	if _, err := s.ReviseAcceptanceCriteria(ctx, "work", taskCriteria(), taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReviseAdaptiveTask(ctx, "dependency", taskDefinition(), taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	retained, err := reopened.GetTaskAttempt(ctx, "attempt")
	if err != nil || retained.CriteriaVersion != 1 || retained.TaskRevision != 1 || retained.Dependencies[0].Revision != 1 {
		t.Fatalf("attempt changed after replan/restart: %+v %v", retained, err)
	}
	active, ok, err := reopened.GetActiveTaskLease(ctx, "work")
	if err != nil || !ok || active.Generation != 1 || !active.NeedsReconciliation(lease.ExpiresAt) {
		t.Fatalf("reservation disappeared: %+v %v %v", active, ok, err)
	}
	request := taskReservation("second", "work")
	request.Mutation.ExpectedRevision, request.Now = 2, lease.ExpiresAt.Add(time.Hour)
	if _, _, err := reopened.ReserveTask(ctx, request); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("timeout admitted duplicate: %v", err)
	}
}

func TestTaskLeaseRequiresCriteriaAndBoundsRetries(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	if _, err := s.CreateAdaptiveTask(ctx, "planned", "project", taskDefinition(), nil, taskMutation(0)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReserveTask(ctx, taskReservation("unfrozen", "planned")); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("unfrozen task reserved: %v", err)
	}
	createTask(t, s, "work", taskDefinition())
	for i := 0; i < 3; i++ {
		_, lease := reserveTask(t, s, fmt.Sprintf("attempt-%d", i), "work")
		if lease.Generation != int64(i+1) {
			t.Fatalf("generation: %+v", lease)
		}
		if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, Reason: "Unseeded launch cancelled", Now: lease.HeartbeatAt.Add(time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.ReserveTask(ctx, taskReservation("too-many", "work")); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("retry budget ignored: %v", err)
	}
	page, err := s.ListTaskAttempts(ctx, "work", 1, 1)
	if err != nil || len(page) != 1 || page[0].Number != 2 {
		t.Fatalf("attempt pagination: %+v %v", page, err)
	}
}

func TestTaskLeaseHeartbeatDoesNotInventMeaningfulActivity(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	_, lease := reserveTask(t, s, "attempt", "work")
	now := lease.HeartbeatAt.Add(10 * time.Second)
	if err := s.RenewTaskLease(ctx, lease.TaskLeaseToken, now, time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTaskLease(ctx, "attempt")
	if err != nil || got.LastActivityAt != nil || !got.HeartbeatAt.Equal(now) {
		t.Fatalf("heartbeat fabricated activity: %+v %v", got, err)
	}
	activity := now.Add(time.Second)
	if err := s.RenewTaskLease(ctx, lease.TaskLeaseToken, activity, time.Minute, &activity); err != nil {
		t.Fatal(err)
	}
	if err := s.RenewTaskLease(ctx, lease.TaskLeaseToken, activity.Add(time.Second), time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetTaskLease(ctx, "attempt")
	if err != nil || got.LastActivityAt == nil || !got.LastActivityAt.Equal(activity) {
		t.Fatalf("activity not retained: %+v %v", got, err)
	}
	old := activity.Add(-time.Second)
	if err := s.RenewTaskLease(ctx, lease.TaskLeaseToken, activity.Add(2*time.Second), time.Minute, &old); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("activity regressed: %v", err)
	}
	stale := lease.TaskLeaseToken
	stale.HolderID = "other"
	if err := s.RenewTaskLease(ctx, stale, now, time.Minute, nil); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("foreign heartbeat: %v", err)
	}
	if err := s.RenewTaskLease(ctx, lease.TaskLeaseToken, got.ExpiresAt, time.Minute, nil); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("expired lease silently renewed: %v", err)
	}
	expired, err := s.ListExpiredTaskLeases(ctx, got.ExpiresAt, "", 1)
	if err != nil || len(expired) != 1 || expired[0].AttemptID != "attempt" {
		t.Fatalf("expired reservation: %+v %v", expired, err)
	}
}

func TestTaskLeaseConcurrentReservationAndIdempotentIntent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Go(func() {
			_, _, err := s.ReserveTask(ctx, taskReservation(fmt.Sprintf("attempt-%d", i), "work"))
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else if !errors.Is(err, ports.ErrTaskLeaseFenced) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("exclusive attempts=%d", success)
	}
	list, err := s.ListTaskAttempts(ctx, "work", 0, 100)
	if err != nil || len(list) != 1 {
		t.Fatalf("attempts: %+v %v", list, err)
	}
	r := taskReservation(list[0].ID, "work")
	r.ID = "new-request-id"
	got, _, err := s.ReserveTask(ctx, r)
	if err != nil || got.ID != list[0].ID {
		t.Fatalf("lost idempotent result: %+v %v", got, err)
	}
	r.TaskID = "foreign"
	if _, _, err := s.ReserveTask(ctx, r); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("intent collision accepted: %v", err)
	}
}

func TestTaskWorkerAssociationAtomicIdempotentAndRetained(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	_, lease := reserveTask(t, s, "attempt", "work")
	rec, snapshot := workerSnapshot(t, s)
	rec.ProjectID = "project"
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_task_dispatch BEFORE INSERT ON adaptive_task_dispatches BEGIN SELECT RAISE(ABORT,'dispatch failure'); END`); err != nil {
		t.Fatal(err)
	}
	before, err := s.EventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.HeartbeatAt); err == nil {
		t.Fatal("failed association accepted")
	}
	sessions, err := s.ListAllSessions(ctx)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("orphan worker: %+v %v", sessions, err)
	}
	after, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(after) != len(before) {
		t.Fatalf("orphan CDC: %d -> %d %v", len(before), len(after), err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_task_dispatch`); err != nil {
		t.Fatal(err)
	}
	seed, created, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.HeartbeatAt)
	if err != nil || !created {
		t.Fatalf("seed: %+v %v %v", seed, created, err)
	}
	retry, created, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.ExpiresAt.Add(time.Second))
	if err != nil || created || retry.ID != seed.ID {
		t.Fatalf("duplicate seed after request loss: %+v %v %v", retry, created, err)
	}
	dispatch, ok, err := s.GetTaskWorkerDispatchBySession(ctx, seed.ID)
	if err != nil || !ok || dispatch.AttemptID != "attempt" || dispatch.ConfigurationHash != snapshot.ContentHash {
		t.Fatalf("missing association: %+v %v %v", dispatch, ok, err)
	}
	if deleted, err := s.DeleteSession(ctx, seed.ID); err == nil && deleted {
		t.Fatal("session deletion erased dispatch history")
	}
	for _, table := range []string{"adaptive_task_attempts", "adaptive_task_dispatches", "adaptive_task_leases"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err == nil {
			t.Fatalf("erased retained %s", table)
		}
	}
	for _, statement := range []string{`UPDATE adaptive_task_attempts SET reason='rewritten'`, `UPDATE adaptive_task_dispatches SET configuration_hash=configuration_hash`, `UPDATE adaptive_task_leases SET generation=generation+1`} {
		if _, err := db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("rewrote immutable dispatch: %s", statement)
		}
	}
}

func TestTaskLeaseRecoveryRequiresExactWorkerOwnership(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	_, lease := reserveTask(t, s, "attempt", "work")
	rec, snapshot := workerSnapshot(t, s)
	rec.ProjectID = "project"
	seed, _, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.HeartbeatAt)
	if err != nil {
		t.Fatal(err)
	}
	proof := domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, NewHolderID: "scheduler-2", Now: lease.ExpiresAt.Add(time.Second), TTL: time.Minute, Reason: "Reconnected the existing host"}
	if err := s.ReleaseTaskLease(ctx, proof); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("unknown worker released: %v", err)
	}
	if err := s.RecoverTaskLease(ctx, proof); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("unknown worker adopted: %v", err)
	}
	owner := seed.ControllerOwner()
	proof.ObservedOwner, proof.SessionID = &owner, seed.ID
	if err := s.ReleaseTaskLease(ctx, proof); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("live worker released: %v", err)
	}
	if err := s.RecoverTaskLease(ctx, proof); err != nil {
		t.Fatal(err)
	}
	if err := s.RenewTaskLease(ctx, lease.TaskLeaseToken, proof.Now, time.Minute, nil); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("old owner still active: %v", err)
	}
	proof.Token.HolderID = "scheduler-2"
	seed.IsTerminated = true
	if err := s.UpdateSession(ctx, seed); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseTaskLease(ctx, proof); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("stale proof accepted: %v", err)
	}
	owner = seed.ControllerOwner()
	if err := s.ReleaseTaskLease(ctx, proof); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.GetActiveTaskLease(ctx, "work"); err != nil || ok {
		t.Fatalf("released reservation active: %v %v", ok, err)
	}
	newRequest := taskReservation("replacement", "work")
	newRequest.Now, newRequest.HolderID = proof.Now, "scheduler-2"
	_, replacement, err := s.ReserveTask(ctx, newRequest)
	if err != nil || replacement.Generation != 2 {
		t.Fatalf("confirmed replacement: %+v %v", replacement, err)
	}
	if err := s.ReleaseTaskLease(ctx, proof); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("old generation released replacement: %v", err)
	}
}

func TestTaskWorkerHonorsRequestedTypeAndLease(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	definition := taskDefinition()
	definition.RequestedWorker = &domain.WorkerSelection{AgentTypeID: "different-type", Version: 2}
	createTask(t, s, "work", definition)
	_, lease := reserveTask(t, s, "attempt", "work")
	rec, snapshot := workerSnapshot(t, s)
	rec.ProjectID = "project"
	if _, _, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.HeartbeatAt); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("manual selection replaced: %v", err)
	}
	if _, _, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.ExpiresAt); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("expired dispatch launched: %v", err)
	}
}

func TestTaskLeaseAuditFailureRollsBackReservationAndRelease(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	failAudit := func() {
		t.Helper()
		if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_lease_audit BEFORE INSERT ON adaptive_task_audit BEGIN SELECT RAISE(ABORT,'audit failure'); END`); err != nil {
			t.Fatal(err)
		}
	}
	failAudit()
	if _, _, err := s.ReserveTask(ctx, taskReservation("attempt", "work")); err == nil {
		t.Fatal("reservation ignored missing audit")
	}
	if _, err := s.GetTaskAttempt(ctx, "attempt"); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("orphan attempt: %v", err)
	}
	if _, ok, err := s.GetActiveTaskLease(ctx, "work"); err != nil || ok {
		t.Fatalf("orphan lease: %v %v", ok, err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_lease_audit`); err != nil {
		t.Fatal(err)
	}
	_, lease := reserveTask(t, s, "attempt", "work")
	failAudit()
	if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, Reason: "cancel unseeded work", Now: lease.HeartbeatAt}); err == nil {
		t.Fatal("release ignored missing audit")
	}
	if got, ok, err := s.GetActiveTaskLease(ctx, "work"); err != nil || !ok || got.ReleasedAt != nil {
		t.Fatalf("failed release freed reservation: %+v %v %v", got, ok, err)
	}
}

func TestTaskWorkerConcurrentSeedAssociationCreatesOneSession(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	_, lease := reserveTask(t, s, "attempt", "work")
	rec, snapshot := workerSnapshot(t, s)
	rec.ProjectID = "project"
	type result struct {
		id      domain.SessionID
		created bool
		err     error
	}
	results := make(chan result, 5)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Go(func() {
			seed, created, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.HeartbeatAt)
			results <- result{seed.ID, created, err}
		})
	}
	wg.Wait()
	close(results)
	var id domain.SessionID
	created := 0
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if id != "" && id != r.id {
			t.Fatalf("duplicate worker: %s and %s", id, r.id)
		}
		id = r.id
		if r.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("fresh launch winners=%d", created)
	}
	sessions, err := s.ListAllSessions(ctx)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("duplicate durable seeds: %+v %v", sessions, err)
	}
}
