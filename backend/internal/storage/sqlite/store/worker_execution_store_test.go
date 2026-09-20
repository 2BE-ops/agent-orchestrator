package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func prepareWorkerInterface(t *testing.T, s *sqlite.Store, rec domain.SessionRecord, snapshot domain.WorkerConfiguration) domain.WorkerExecution {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	transition := domain.SessionInterfaceTransition{ID: "worker-interface", SessionID: rec.ID, SourceMode: domain.SessionModeTUI, TargetMode: domain.SessionModeChat, Policy: domain.SessionInterfaceTransitionDrain, HistoryPolicy: domain.SessionInterfaceTransitionHistoryStrict, Phase: domain.SessionInterfaceTransitionRequested, NativeConversationID: "native-worker", CreatedAt: now, UpdatedAt: now}
	if _, created, err := s.CreateSessionInterfaceTransition(ctx, transition); err != nil || !created {
		t.Fatalf("transition: %v %v", created, err)
	}
	snapshot.Effective.SessionMode = domain.SessionModeChat
	snapshot.Selection.Overrides.SessionMode = &snapshot.Effective.SessionMode
	snapshot.ContentHash = snapshot.Hash()
	execution := domain.WorkerExecution{ID: "execution-1", SessionID: rec.ID, SourceKind: "interface_transition", SourceID: transition.ID, Configuration: snapshot, Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Use Chat", CreatedAt: now}
	prepared, err := s.PrepareWorkerExecution(ctx, rec.ControllerOwner(), execution)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestWorkerExecutionInterfaceActivationAndRollbackAreAtomic(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	rec, original := workerSnapshot(t, s)
	rec, err := s.CreateConfiguredSession(ctx, rec, original)
	if err != nil {
		t.Fatal(err)
	}
	execution := prepareWorkerInterface(t, s, rec, original)
	if duplicate, err := s.PrepareWorkerExecution(ctx, rec.ControllerOwner(), execution); err != nil || duplicate.ID != execution.ID {
		t.Fatalf("idempotent preparation: %+v %v", duplicate, err)
	}
	conflict := execution
	conflict.Configuration.Effective.Config.Model = "changed"
	conflict.Configuration.ContentHash = conflict.Configuration.Hash()
	if _, err := s.PrepareWorkerExecution(ctx, rec.ControllerOwner(), conflict); !errors.Is(err, ports.ErrRegistryConflict) {
		t.Fatalf("idempotency conflict: %v", err)
	}
	current, sequence, found, err := s.GetEffectiveWorkerConfiguration(ctx, rec.ID)
	if err != nil || !found || sequence != 0 || current.ContentHash != original.ContentHash {
		t.Fatalf("prepared config prematurely active: %+v %d %v", current, sequence, err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_worker_activation BEFORE INSERT ON adaptive_worker_execution_activations BEGIN SELECT RAISE(ABORT,'injected activation failure'); END;`); err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := s.EventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := s.CommitSessionControllerEpoch(ctx, rec.ID, domain.SessionModeTUI, domain.SessionModeChat, "native-worker", time.Now().UTC()); err == nil || changed {
		t.Fatalf("activation failure accepted: %v %v", changed, err)
	}
	after, _, err := s.GetSession(ctx, rec.ID)
	if err != nil || after.Mode != domain.SessionModeTUI {
		t.Fatalf("mode escaped rollback: %+v %v", after, err)
	}
	eventsAfter, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(eventsBefore) != len(eventsAfter) {
		t.Fatalf("failed activation emitted CDC: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_worker_activation`); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.CommitSessionControllerEpoch(ctx, rec.ID, domain.SessionModeTUI, domain.SessionModeChat, "native-worker", time.Now().UTC()); err != nil || !changed {
		t.Fatalf("activation: %v %v", changed, err)
	}
	current, sequence, _, err = s.GetEffectiveWorkerConfiguration(ctx, rec.ID)
	if err != nil || sequence == 0 || current.Effective.SessionMode != domain.SessionModeChat || current.ContentHash != execution.Configuration.ContentHash {
		t.Fatalf("wrong active config: %+v %v", current, err)
	}
	history, err := s.ListWorkerExecutions(ctx, rec.ID, 0, 100)
	if err != nil || len(history) != 1 || history[0].ExecutionID != execution.ID || history[0].Action != "applied" {
		t.Fatalf("activation history: %+v %v", history, err)
	}
	for _, statement := range []string{`UPDATE adaptive_worker_executions SET reason='rewritten'`, `DELETE FROM adaptive_worker_executions`, `UPDATE adaptive_worker_execution_activations SET action='rolled_back'`, `DELETE FROM adaptive_worker_execution_activations`} {
		if _, err := db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("mutable execution history: %s", statement)
		}
	}
	if restored, err := s.RestoreSessionControllerEpoch(ctx, rec.ID, domain.SessionModeChat, domain.SessionModeTUI, "native-worker", time.Now().UTC()); err != nil || !restored {
		t.Fatalf("rollback: %v %v", restored, err)
	}
	current, sequence, _, err = s.GetEffectiveWorkerConfiguration(ctx, rec.ID)
	if err != nil || current.ContentHash != original.ContentHash || sequence <= history[0].Sequence {
		t.Fatalf("rollback lost original: %+v %v", current, err)
	}
	history, err = s.ListWorkerExecutions(ctx, rec.ID, history[0].Sequence, 1)
	if err != nil || len(history) != 1 || history[0].Action != "rolled_back" || history[0].ExecutionID != "" {
		t.Fatalf("rollback history: %+v %v", history, err)
	}
	retained, ok, err := s.GetWorkerConfiguration(ctx, rec.ID)
	if err != nil || !ok || retained.ContentHash != original.ContentHash {
		t.Fatal("launch facts changed")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	current, afterSequence, _, err := reopened.GetEffectiveWorkerConfiguration(ctx, rec.ID)
	if err != nil || afterSequence != sequence || current.ContentHash != original.ContentHash {
		t.Fatalf("restart lost execution rollback: %v", err)
	}
	if deleted, err := reopened.DeleteSession(ctx, rec.ID); err != nil || deleted {
		t.Fatalf("retained worker was treated as a disposable seed: %v %v", deleted, err)
	}
	if _, _, found, err := reopened.GetEffectiveWorkerConfiguration(ctx, rec.ID); err != nil || !found {
		t.Fatalf("retained execution history disappeared: %v %v", found, err)
	}
}

func TestWorkerExecutionRejectsStaleOwnersAndUnpreparedModeChanges(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	rec, original := workerSnapshot(t, s)
	rec, err := s.CreateConfiguredSession(ctx, rec, original)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := s.CommitSessionControllerEpoch(ctx, rec.ID, domain.SessionModeTUI, domain.SessionModeChat, "native", time.Now()); err == nil || changed {
		t.Fatalf("unprepared mode change: %v %v", changed, err)
	}
	execution := prepareWorkerInterface(t, s, rec, original)
	wrongOwner := rec.ControllerOwner()
	wrongOwner.RuntimeLaunchID = "stale"
	if _, err := s.PrepareWorkerExecution(ctx, wrongOwner, execution); !errors.Is(err, ports.ErrRegistryConflict) {
		t.Fatalf("stale owner accepted: %v", err)
	}
	if _, err := s.GetWorkerExecution(ctx, "other-session", execution.ID); err == nil {
		t.Fatal("cross-session history returned")
	}
	if _, err := s.ListWorkerExecutions(ctx, rec.ID, -1, 10); err == nil {
		t.Fatal("negative cursor accepted")
	}
	if _, err := s.ListWorkerExecutions(ctx, rec.ID, 0, 101); err == nil {
		t.Fatal("unbounded history accepted")
	}
}
