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

func TestTaskMessageDeliveryExclusiveClaimAndCrashRetention(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, reply, _ := taskMessageFixture(t, s, true)
	if _, _, err := s.SubmitTaskMessage(ctx, input); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	winners := make(chan domain.TaskMessageDelivery, 4)
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			delivery, created, err := s.BeginTaskMessageDelivery(ctx, input.ID, fmt.Sprint(i))
			if err != nil && !errors.Is(err, ports.ErrTaskConflict) {
				t.Errorf("claim failed: %v", err)
			}
			if created {
				winners <- delivery
			}
		}()
	}
	wg.Wait()
	close(winners)
	var winner domain.TaskMessageDelivery
	count := 0
	for delivery := range winners {
		winner, count = delivery, count+1
	}
	if count != 1 || winner.SessionID != reply.SessionID || winner.Owner != reply.SourceOwner || winner.State != "dispatching" || winner.DeliveryKey != "adaptive-message:"+input.ID {
		t.Fatalf("exclusive pinned destination: %d %+v", count, winner)
	}
	if _, created, err := s.BeginTaskMessageDelivery(ctx, input.ID, winner.ID); err != nil || created {
		t.Fatalf("claim replay permits native send: %v %v", created, err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	pending, err := reopened.ListPendingTaskMessages(ctx, 0, 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("uncertain native operation requeued after crash: %+v %v", pending, err)
	}
	unresolved, err := reopened.ListUnresolvedTaskMessageDeliveries(ctx, "", 100)
	if err != nil || len(unresolved) != 1 || unresolved[0].ID != winner.ID {
		t.Fatalf("lost reconciliation facts: %+v %v", unresolved, err)
	}
	resolution := domain.TaskMessageDeliveryResolution{ID: winner.ID, State: "uncertain", Reason: "Daemon restarted before native result was recorded"}
	if err := reopened.ResolveTaskMessageDelivery(ctx, resolution); err != nil {
		t.Fatal(err)
	}
	if err := reopened.ResolveTaskMessageDelivery(ctx, resolution); err != nil {
		t.Fatalf("resolution retry: %v", err)
	}
	resolution.State = "not_sent"
	if err := reopened.ResolveTaskMessageDelivery(ctx, resolution); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("uncertain outcome rewritten: %v", err)
	}
	if _, _, err := reopened.BeginTaskMessageDelivery(ctx, input.ID, "restart-retry"); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("unknown outcome blindly retried: %v", err)
	}
}

func TestTaskMessageDeliveryOnlyProvenNoSendCanRetryAndRetriesAreBounded(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, _, _ := taskMessageFixture(t, s, true)
	if _, _, err := s.SubmitTaskMessage(ctx, input); err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		delivery, created, err := s.BeginTaskMessageDelivery(ctx, input.ID, fmt.Sprint(i))
		if err != nil || !created || delivery.Number != int64(i+1) {
			t.Fatalf("claim: %+v %v %v", delivery, created, err)
		}
		if err := s.ResolveTaskMessageDelivery(ctx, domain.TaskMessageDeliveryResolution{ID: delivery.ID, State: "not_sent", Reason: "Owner guard rejected before transport was called"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.BeginTaskMessageDelivery(ctx, input.ID, "exhausted"); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("unbounded retry: %v", err)
	}
	if pending, err := s.ListPendingTaskMessages(ctx, 0, 100); err != nil || len(pending) != 0 {
		t.Fatalf("exhausted work requeued: %+v %v", pending, err)
	}
	input.ID, input.IdempotencyKey = "delivered", "delivered"
	if _, _, err := s.SubmitTaskMessage(ctx, input); err != nil {
		t.Fatal(err)
	}
	delivery, _, err := s.BeginTaskMessageDelivery(ctx, input.ID, "delivered-intent")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveTaskMessageDelivery(ctx, domain.TaskMessageDeliveryResolution{ID: delivery.ID, State: "handed_off", Reason: "Native transport accepted the message"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.BeginTaskMessageDelivery(ctx, input.ID, "duplicate"); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("handed-off message retried: %v", err)
	}
}

func TestTaskMessageDeliveryRechecksTargetIntentOwnershipAndLease(t *testing.T) {
	ctx := context.Background()
	for _, condition := range []string{"cancelled", "terminated", "expired", "pending_native"} {
		t.Run(condition, func(t *testing.T) {
			dir := t.TempDir()
			s := sqlitetest.MustOpenAt(t, dir)
			input, target, lease := taskMessageFixture(t, s, true)
			if _, _, err := s.SubmitTaskMessage(ctx, input); err != nil {
				t.Fatal(err)
			}
			switch condition {
			case "cancelled":
				cancelTask(t, s, "target")
			case "terminated":
				rec, _, err := s.GetSession(ctx, target.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				rec.IsTerminated = true
				if err := s.UpdateSession(ctx, rec); err != nil {
					t.Fatal(err)
				}
			case "expired":
				// Simulate elapsed time without a sleep or weakening heartbeat CAS.
				db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Close() }()
				if _, err := db.Exec(`UPDATE adaptive_task_leases SET heartbeat_at=?,expires_at=? WHERE attempt_id=?`, time.Now().Add(-2*time.Minute), time.Now().Add(-time.Minute), lease.AttemptID); err != nil {
					t.Fatal(err)
				}
			case "pending_native":
				op := domain.TaskExecutionOperation{ID: "pending-target", SessionID: target.SessionID, Lease: lease.TaskLeaseToken, SourceOwner: target.SourceOwner, Kind: "restore", CreatedAt: time.Now().UTC()}
				if _, err := s.BeginTaskExecution(ctx, op); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := s.BeginTaskMessageDelivery(ctx, input.ID, condition); !errors.Is(err, ports.ErrTaskLeaseFenced) {
				t.Fatalf("ineligible target: %v", err)
			}
			if pending, err := s.ListPendingTaskMessages(ctx, 0, 100); err != nil || len(pending) != 1 {
				t.Fatalf("blocked message lost: %+v %v", pending, err)
			}
		})
	}
}

func TestTaskMessageDeliveryJournalAndAuditCommitTogether(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, _, _ := taskMessageFixture(t, s, true)
	if _, _, err := s.SubmitTaskMessage(ctx, input); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TRIGGER fail_delivery_audit BEFORE INSERT ON adaptive_task_audit WHEN NEW.action LIKE 'message_delivery_%' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.BeginTaskMessageDelivery(ctx, input.ID, "delivery"); err == nil || created {
		t.Fatalf("claim escaped failed audit: %v %v", created, err)
	}
	if rows, err := s.ListTaskMessageDeliveries(ctx, input.ID); err != nil || len(rows) != 0 {
		t.Fatalf("partial journal: %+v %v", rows, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_delivery_audit`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.BeginTaskMessageDelivery(ctx, input.ID, "delivery"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_delivery_resolution BEFORE INSERT ON adaptive_task_audit WHEN NEW.action='message_delivery_handed_off' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	resolution := domain.TaskMessageDeliveryResolution{ID: "delivery", State: "handed_off", Reason: "Transport accepted"}
	if err := s.ResolveTaskMessageDelivery(ctx, resolution); err == nil {
		t.Fatal("resolution escaped failed audit")
	}
	rows, err := s.ListTaskMessageDeliveries(ctx, input.ID)
	if err != nil || len(rows) != 1 || rows[0].State != "dispatching" {
		t.Fatalf("lost unknown outcome: %+v %v", rows, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_delivery_resolution`); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveTaskMessageDelivery(ctx, resolution); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`UPDATE adaptive_task_message_deliveries SET state='not_sent'`, `UPDATE adaptive_task_message_deliveries SET session_id='forged'`, `DELETE FROM adaptive_task_message_deliveries`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("mutable journal: %s", statement)
		}
	}
}
