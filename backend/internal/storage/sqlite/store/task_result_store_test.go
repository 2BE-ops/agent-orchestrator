package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func taskResultFixture(t *testing.T, s *sqlite.Store, seal bool, acceptance ...domain.AcceptanceCriteria) (domain.TaskResultSubmission, domain.TaskContextSnapshot, domain.TaskLease) {
	t.Helper()
	ctx := context.Background()
	snapshot, lease := taskContextFixture(t, s, acceptance...)
	if seal {
		if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	rec, _, err := s.GetSession(ctx, snapshot.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	rec.Metadata.RuntimeLaunchID = snapshot.ExecutionOperationID
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveTaskExecution(ctx, domain.TaskExecutionResolution{OperationID: snapshot.ExecutionOperationID, ObservedOwner: rec.ControllerOwner(), Outcome: "connected", Reason: "Test observed native controller", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	input := domain.TaskResultSubmission{ID: "result", AttemptID: lease.AttemptID, SessionID: rec.ID, SourceOwner: rec.ControllerOwner(), IdempotencyKey: "submission-1", Definition: domain.TaskResultDefinition{SchemaVersion: 1, ClaimedOutcome: "completed", ClaimedCommit: strings.Repeat("a", 40), Summary: "Implemented bounded task", Implementation: "Added requested behavior", Tests: []domain.TaskTestClaim{{Command: []string{"go", "test", "./..."}, Outcome: "passed", Details: "Worker claim only"}}, KnowledgeCandidates: []domain.TaskKnowledgeCandidate{{Title: "Candidate", Kind: "convention", Content: "Possible useful convention", Confidence: "medium"}}}}
	return input, snapshot, lease
}

func TestTaskResultHistoryReplayAndRestartNeverDeclareCompletion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, snapshot, lease := taskResultFixture(t, s, true)
	first, created, err := s.SubmitTaskResult(ctx, input)
	if err != nil || !created || first.Number != 1 || first.ContextHash != snapshot.ContentHash || first.ConfigurationHash != snapshot.ConfigurationHash || first.CriteriaVersion != 1 {
		t.Fatalf("attribution: %+v %v %v", first, created, err)
	}
	if _, created, err := s.SubmitTaskResult(ctx, input); err != nil || created {
		t.Fatalf("duplicate submission: %v %v", created, err)
	}
	conflict := input
	conflict.Definition.Summary = "Different claims"
	if _, _, err := s.SubmitTaskResult(ctx, conflict); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("idempotency overwritten: %v", err)
	}
	// Cancellation does not erase useful output from a controller still owned by
	// this attempt. It also cannot cause the claim to override cancellation.
	cancelTask(t, s, "work")
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "correction", "submission-2", 1
	input.Definition.ClaimedOutcome, input.Definition.Summary = "partial", "Found a remaining issue"
	second, created, err := s.SubmitTaskResult(ctx, input)
	if err != nil || !created || second.Number != 2 {
		t.Fatalf("correction: %+v %v", second, err)
	}
	if _, active, err := s.GetActiveTaskLease(ctx, "work"); err != nil || !active {
		t.Fatalf("worker claim released ownership: %v %v", active, err)
	}
	task, err := s.GetAdaptiveTask(ctx, "work")
	if err != nil || task.Revision != 1 {
		t.Fatalf("worker rewrote planning: %+v %v", task, err)
	}
	knowledge, err := s.ListProjectKnowledge(ctx, domain.KnowledgeFilter{ProjectID: "project", Limit: 100})
	if err != nil || len(knowledge) != 0 {
		t.Fatalf("claim promoted knowledge: %+v %v", knowledge, err)
	}
	rec, _, err := s.GetSession(ctx, input.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	rec.IsTerminated = true
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	owner := rec.ControllerOwner()
	if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, SessionID: rec.ID, ObservedOwner: &owner, Reason: "Verified exit", Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if replay, created, err := reopened.SubmitTaskResult(ctx, input); err != nil || created || replay.ID != second.ID {
		t.Fatalf("historical acknowledgement: %+v %v %v", replay, created, err)
	}
	rows, err := reopened.ListTaskResults(ctx, lease.AttemptID, 0, 1)
	if err != nil || len(rows) != 1 || rows[0].ContentHash != first.ContentHash {
		t.Fatalf("retained history: %+v %v", rows, err)
	}
	latest, found, err := reopened.LatestTaskResult(ctx, lease.AttemptID)
	if err != nil || !found || latest.ID != second.ID {
		t.Fatalf("latest correction: %+v %v %v", latest, found, err)
	}
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "late", "late", 2
	if _, _, err := reopened.SubmitTaskResult(ctx, input); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("released worker wrote: %v", err)
	}
}

func TestTaskResultRejectsUnownedStaleAndUnsealedClaims(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"session", "generation", "activation", "version", "unsealed", "terminated", "pending_native", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			s := newTestStore(t)
			input, _, lease := taskResultFixture(t, s, kind != "unsealed")
			switch kind {
			case "session":
				input.SessionID = "different"
			case "generation":
				input.SourceOwner.RuntimeLaunchID = "stale"
			case "activation":
				input.ExpectedActivation = 1
			case "version":
				input.ExpectedVersion = 1
			case "malformed":
				input.Definition.SchemaVersion = 2
			case "terminated":
				rec, _, err := s.GetSession(ctx, input.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				rec.IsTerminated = true
				if err := s.UpdateSession(ctx, rec); err != nil {
					t.Fatal(err)
				}
			case "pending_native":
				if _, err := s.BeginTaskExecution(ctx, domain.TaskExecutionOperation{ID: "uncertain-restore", SessionID: input.SessionID, Lease: lease.TaskLeaseToken, SourceOwner: input.SourceOwner, Kind: "restore", CreatedAt: time.Now().UTC()}); err != nil {
					t.Fatal(err)
				}
			}
			if _, created, err := s.SubmitTaskResult(ctx, input); err == nil || created {
				t.Fatalf("invalid result admitted: %v %v", created, err)
			}
			if _, found, err := s.LatestTaskResult(ctx, lease.AttemptID); err != nil || found {
				t.Fatalf("failed submission persisted: %v %v", found, err)
			}
		})
	}
}

func TestTaskResultConcurrentRetriesAndCorrectionsAreFenced(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, _, _ := taskResultFixture(t, s, true)
	type outcome struct {
		result  domain.TaskResult
		created bool
		err     error
	}
	for _, sameKey := range []bool{true, false} {
		results := make(chan outcome, 4)
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				request := input
				request.ID = fmt.Sprintf("result-%v-%d", sameKey, i)
				if !sameKey {
					request.ExpectedVersion = 1
					request.IdempotencyKey = request.ID
				}
				result, created, err := s.SubmitTaskResult(ctx, request)
				results <- outcome{result, created, err}
			}(i)
		}
		wg.Wait()
		close(results)
		created, conflicts := 0, 0
		for got := range results {
			if got.created {
				created++
			}
			if errors.Is(got.err, ports.ErrTaskConflict) {
				conflicts++
			} else if got.err != nil {
				t.Fatal(got.err)
			}
		}
		if created != 1 || (sameKey && conflicts != 0) || (!sameKey && conflicts != 3) {
			t.Fatalf("concurrent submission effects: created=%d conflicts=%d", created, conflicts)
		}
	}
}

func TestTaskResultAuditIsAtomicAndHistoryImmutable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, _, _ := taskResultFixture(t, s, true)
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM change_log WHERE event_type='adaptive_task_changed'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_result_audit BEFORE INSERT ON adaptive_task_audit WHEN NEW.action='result_submitted' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.SubmitTaskResult(ctx, input); err == nil || created {
		t.Fatalf("failed audit ignored: %v %v", created, err)
	}
	if _, found, err := s.LatestTaskResult(ctx, input.AttemptID); err != nil || found {
		t.Fatalf("result escaped rollback: %v %v", found, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_result_audit`); err != nil {
		t.Fatal(err)
	}
	for i := int64(0); i < 16; i++ {
		input.ID, input.IdempotencyKey, input.ExpectedVersion = fmt.Sprint(i), fmt.Sprint(i), i
		if _, _, err := s.SubmitTaskResult(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	input.ExpectedVersion = 16
	if _, _, err := s.SubmitTaskResult(ctx, input); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("unbounded result revisions: %v", err)
	}
	for _, statement := range []string{`UPDATE adaptive_task_results SET definition='{}'`, `DELETE FROM adaptive_task_results`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("mutable result: %s", statement)
		}
	}
	var events int
	if err := db.QueryRow(`SELECT count(*) FROM adaptive_task_audit WHERE action='result_submitted'`).Scan(&events); err != nil || events != 16 {
		t.Fatalf("result audit count: %d %v", events, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM change_log WHERE event_type='adaptive_task_changed'`).Scan(&events); err != nil || events != before+16 {
		t.Fatalf("result CDC count: %d -> %d %v", before, events, err)
	}
}

func TestTaskResultAttributesEffectiveConfigurationAfterInterfaceChange(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, contextSnapshot, _ := taskResultFixture(t, s, true)
	rec, _, err := s.GetSession(ctx, input.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := s.GetWorkerConfiguration(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution := prepareWorkerInterface(t, s, rec, original)
	if _, _, err := s.SubmitTaskResult(ctx, input); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("claim during interface change: %v", err)
	}
	if changed, err := s.CommitSessionControllerEpoch(ctx, rec.ID, domain.SessionModeTUI, domain.SessionModeChat, "native-worker", time.Now().UTC()); err != nil || !changed {
		t.Fatalf("change mode: %v %v", changed, err)
	}
	rec, _, err = s.GetSession(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec.Metadata.ControllerGeneration = "chat-result-generation"
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	_, sequence, _, err := s.GetEffectiveWorkerConfiguration(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	input.SourceOwner, input.ExpectedActivation = rec.ControllerOwner(), sequence
	if changed, err := s.AdvanceSessionInterfaceTransition(ctx, execution.SourceID, domain.SessionInterfaceTransitionRequested, domain.SessionInterfaceTransitionRecovery, "native-worker", "", "", time.Now().UTC()); err != nil || !changed {
		t.Fatalf("record uncertain transition: %v %v", changed, err)
	}
	if _, _, err := s.SubmitTaskResult(ctx, input); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("claim during uncertain ownership: %v", err)
	}
	if changed, err := s.AdvanceSessionInterfaceTransition(ctx, execution.SourceID, domain.SessionInterfaceTransitionRecovery, domain.SessionInterfaceTransitionRecovery, "native-worker", "DAEMON_RESTARTED", "Existing reconciler confirmed controller ownership", time.Now().UTC()); err != nil || !changed {
		t.Fatalf("reconcile transition: %v %v", changed, err)
	}
	result, _, err := s.SubmitTaskResult(ctx, input)
	if err != nil || result.ConfigurationHash != execution.Configuration.ContentHash || result.ConfigurationSequence != sequence || result.ContextHash != contextSnapshot.ContentHash || result.NativeGeneration != rec.Metadata.ControllerGeneration {
		t.Fatalf("wrong segment attribution: %+v %v", result, err)
	}
	performance := performanceAttempt(t, s, result.AttemptID)
	if !performance.MixedConfigurations || performance.Configuration == nil || performance.Configuration.ConfigurationHash != result.ConfigurationHash || performance.Configuration.ConfigurationSequence != sequence || performance.Configuration.Mode != domain.SessionModeChat {
		t.Fatalf("performance lost mixed configuration attribution: %+v", performance)
	}
	conversation, err := s.CreateConversation(ctx, "result-conversation", domain.ConversationScopeSession, rec.ProjectID, rec.ID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	change := domain.WorkerNativeChange{ID: "uncertain-control", SessionID: rec.ID, ConversationID: conversation.ID, Owner: rec.ControllerOwner(), PreviousActivation: sequence, Previous: []domain.WorkerNativeOption{{ID: "effort", Select: "low"}}, Requested: domain.WorkerNativeOption{ID: "effort", Select: "high"}, CreatedAt: time.Now().UTC()}
	if err := s.BeginWorkerNativeChange(ctx, change); err != nil {
		t.Fatal(err)
	}
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "corrected", "corrected", 1
	if _, _, err := s.SubmitTaskResult(ctx, input); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("uncertain native setting attributed: %v", err)
	}
	if err := s.RevertWorkerNativeChange(ctx, rec.ControllerOwner(), change.ID, "Verified original setting"); err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.SubmitTaskResult(ctx, input); err != nil || !created {
		t.Fatalf("reconciled result denied: %v %v", created, err)
	}
}
