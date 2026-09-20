package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func classifiedDelegationFixture(t *testing.T, s *sqlite.Store) (domain.TaskContextSnapshot, domain.TaskLease) {
	t.Helper()
	snapshot, lease := taskContextFixture(t, s)
	snapshot.SchemaVersion = 2
	snapshot.MaxContextClass = domain.ContextTechnical
	snapshot.Classification = domain.ContextTechnical
	snapshot.SystemPrompt = "Sealed classified system instructions"
	snapshot.SystemPromptHash = domain.ContextTextHash(snapshot.SystemPrompt)
	snapshot.SystemPromptBytes = len(snapshot.SystemPrompt)
	for i := range snapshot.Sources {
		snapshot.Sources[i].Classification = domain.ContextTechnical
	}
	sealContext(t, &snapshot)
	if err := s.SaveTaskContext(context.Background(), lease.TaskLeaseToken, snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot, lease
}

func restoreOperation(rec domain.SessionRecord, lease domain.TaskLease, id string) domain.TaskExecutionOperation {
	return domain.TaskExecutionOperation{ID: id, SessionID: rec.ID, SourceOwner: rec.ControllerOwner(),
		Lease: lease.TaskLeaseToken,
		Kind:  "restore", CreatedAt: lease.HeartbeatAt.Add(time.Second)}
}

func workerRecord(t *testing.T, s *sqlite.Store, id domain.SessionID) domain.SessionRecord {
	t.Helper()
	rec, found, err := s.GetSession(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("worker session: %v %v", found, err)
	}
	return rec
}

func TestAppendTaskDelegationJournalsReplacementGenerationsWithoutRewritingSealedContext(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	snapshot, lease := classifiedDelegationFixture(t, s)

	// The manifest seal already journalled version 1 for the dispatch operation.
	initial, err := s.GetTaskDelegation(ctx, snapshot.AttemptID, 1)
	if err != nil {
		t.Fatal(err)
	}
	// A replacement only exists once the original native execution settled.
	connectDispatch(t, s, snapshot)

	rec := workerRecord(t, s, snapshot.SessionID)
	first := restoreOperation(rec, lease, "restore-native-1")
	if _, err := s.BeginTaskExecution(ctx, first); err != nil {
		t.Fatal(err)
	}
	appended, created, err := s.AppendTaskDelegation(ctx, first, snapshot, time.Now().UTC())
	if err != nil || !created || appended.Number != 2 || appended.ExecutionOperationID != first.ID {
		t.Fatalf("append: %+v created=%v err=%v", appended, created, err)
	}
	// The receipt copies the retained bytes: sealed context is never rewritten
	// and live configuration cannot substitute for it.
	if appended.ContextHash != snapshot.ContentHash || appended.ConfigurationHash != snapshot.ConfigurationHash ||
		appended.Prompt != snapshot.Prompt || appended.SystemPrompt != snapshot.SystemPrompt ||
		appended.Classification != snapshot.Classification || appended.MaxContextClass != snapshot.MaxContextClass {
		t.Fatalf("replacement receipt changed retained input: %+v", appended)
	}
	if appended.ContentHash == initial.ContentHash {
		t.Fatal("replacement receipt did not bind its own generation identity")
	}

	// Replaying the same restore operation is idempotent: the original receipt
	// returns and no third version appears.
	replay, created, err := s.AppendTaskDelegation(ctx, first, snapshot, time.Now().UTC())
	if err != nil || created || replay.Number != 2 || replay.ContentHash != appended.ContentHash {
		t.Fatalf("replay: %+v created=%v err=%v", replay, created, err)
	}

	// The first replacement settled (its controller connected) before a second
	// generation can reserve the same exclusive lease.
	if err := s.ResolveTaskExecution(ctx, domain.TaskExecutionResolution{OperationID: first.ID,
		ObservedOwner: rec.ControllerOwner(), Outcome: "connected",
		Reason: "Fixture confirmed the first replacement", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	// A second replacement generation gets its own version with the same
	// retained bytes.
	second := restoreOperation(rec, lease, "restore-native-2")
	if _, err := s.BeginTaskExecution(ctx, second); err != nil {
		t.Fatal(err)
	}
	next, created, err := s.AppendTaskDelegation(ctx, second, snapshot, time.Now().UTC())
	if err != nil || !created || next.Number != 3 || next.Prompt != snapshot.Prompt || next.ContextHash != snapshot.ContentHash {
		t.Fatalf("second replacement: %+v created=%v err=%v", next, created, err)
	}

	// The sealed context row and the original receipt stay untouched.
	retained, found, err := s.GetTaskContext(ctx, lease.AttemptID)
	if err != nil || !found || retained.ContentHash != snapshot.ContentHash {
		t.Fatalf("sealed context changed: %v %v %v", retained, found, err)
	}
	unchanged, err := s.GetTaskDelegation(ctx, snapshot.AttemptID, 1)
	if err != nil || unchanged.ContentHash != initial.ContentHash || unchanged.ExecutionOperationID != initial.ExecutionOperationID {
		t.Fatalf("initial receipt changed: %+v %v", unchanged, err)
	}
}

func TestAppendTaskDelegationFencesUnownedAndLegacyRestoreInstructions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	snapshot, lease := classifiedDelegationFixture(t, s)
	connectDispatch(t, s, snapshot)
	rec := workerRecord(t, s, snapshot.SessionID)

	// A retained context from another execution cannot be journalled here.
	foreign := restoreOperation(rec, lease, "restore-native-foreign")
	foreign.Lease.AttemptID = "other-attempt"
	if _, _, err := s.AppendTaskDelegation(ctx, foreign, snapshot, time.Now().UTC()); !errors.Is(err, ports.ErrTaskForbidden) {
		t.Fatalf("foreign execution journalled: %v", err)
	}

	// An unreserved operation (no BeginTaskExecution row) is refused by the
	// delegation scope trigger, keeping receipts bound to real executions.
	unreserved := restoreOperation(rec, lease, "restore-native-unreserved")
	if _, _, err := s.AppendTaskDelegation(ctx, unreserved, snapshot, time.Now().UTC()); err == nil {
		t.Fatal("unreserved operation journalled")
	}

	// Only a restore-kind execution operation journals replacement instructions.
	dispatch := domain.TaskExecutionOperation{ID: "dispatch-native-2", SessionID: rec.ID, SourceOwner: rec.ControllerOwner(),
		Lease: lease.TaskLeaseToken, Kind: "dispatch", CreatedAt: lease.HeartbeatAt.Add(time.Second)}
	if _, err := s.BeginTaskExecution(ctx, dispatch); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AppendTaskDelegation(ctx, dispatch, snapshot, time.Now().UTC()); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("dispatch operation journalled: %v", err)
	}
}

func TestAppendTaskDelegationLeavesLegacyContextsUnjournalled(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpenAt(t, t.TempDir())
	legacy, legacyLease := taskContextFixture(t, s)
	connectDispatch(t, s, legacy)
	legacyRestore := restoreOperation(workerRecord(t, s, legacy.SessionID), legacyLease, "restore-native-legacy")
	if _, err := s.BeginTaskExecution(ctx, legacyRestore); err != nil {
		t.Fatal(err)
	}
	// Legacy schema-v1 contexts predate journalled instructions; they stay
	// unreadable rather than inventing the text they never retained.
	appended, created, err := s.AppendTaskDelegation(ctx, legacyRestore, legacy, time.Now().UTC())
	if err != nil || created || appended.Number != 0 {
		t.Fatalf("legacy append: %+v created=%v err=%v", appended, created, err)
	}
	if versions, err := s.ListTaskDelegations(ctx, legacyLease.AttemptID, 0, 100); err != nil || len(versions) != 0 {
		t.Fatalf("legacy journalled versions: %+v err=%v", versions, err)
	}
}

// connectDispatch settles the fixture's original dispatch operation so a
// replacement generation can reserve the same exclusive lease.
func connectDispatch(t *testing.T, s *sqlite.Store, snapshot domain.TaskContextSnapshot) {
	t.Helper()
	rec := workerRecord(t, s, snapshot.SessionID)
	if err := s.ResolveTaskExecution(context.Background(), domain.TaskExecutionResolution{
		OperationID: snapshot.ExecutionOperationID, ObservedOwner: rec.ControllerOwner(),
		Outcome: "connected", Reason: "Fixture confirmed the original dispatch", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}
