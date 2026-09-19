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

func taskEvaluationFixture(t *testing.T, s *sqlite.Store, criteria domain.AcceptanceCriteria) (domain.TaskResultSubmission, domain.TaskResult) {
	t.Helper()
	ctx := context.Background()
	input, _, _ := taskResultFixture(t, s, true)
	if _, err := s.ReviseAcceptanceCriteria(ctx, "work", criteria, taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	seed, lease, _ := nextArtifactContext(t, s, domain.TaskMessageSubmission{AttemptID: input.AttemptID, SessionID: input.SessionID, SourceOwner: input.SourceOwner})
	if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, seed); err != nil {
		t.Fatal(err)
	}
	rec, _, err := s.GetSession(ctx, seed.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	rec.Metadata.RuntimeLaunchID = seed.ExecutionOperationID
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveTaskExecution(ctx, domain.TaskExecutionResolution{OperationID: seed.ExecutionOperationID, ObservedOwner: rec.ControllerOwner(), Outcome: "connected", Reason: "Fixture observed worker", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	input.AttemptID, input.SessionID, input.SourceOwner = lease.AttemptID, rec.ID, rec.ControllerOwner()
	result, _, err := s.SubmitTaskResult(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return input, result
}

func evaluationCriteria() domain.AcceptanceCriteria {
	return domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "ci", Requirement: "Named test and build checks pass", EvidenceKind: "ci", CheckNames: []string{"test", "build"}}}}
}

func evaluationRequest(result domain.TaskResult, id string, version int64) domain.TaskEvaluationRequest {
	return domain.TaskEvaluationRequest{ID: id, ResultID: result.ID, ExpectedVersion: version, IdempotencyKey: id, Mutation: domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "evaluator"}, Reason: "Collect independent checks"}}
}

func evaluationChecks(t *testing.T, s *sqlite.Store, result domain.TaskResult, head string, status domain.PRCheckStatus) {
	t.Helper()
	now := time.Now().UTC()
	pr := domain.PullRequest{URL: "https://example.test/pr/1", SessionID: result.SessionID, Number: 1, HeadSHA: head, UpdatedAt: now, ObservedAt: now, CIObservedAt: now}
	checks := []domain.PullRequestCheck{}
	for _, name := range []string{"test", "build"} {
		checks = append(checks, domain.PullRequestCheck{Name: name, CommitHash: result.Definition.ClaimedCommit, Status: status, Conclusion: "success", CreatedAt: now})
	}
	if err := s.WriteSCMObservation(context.Background(), pr, checks, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
}

func TestTaskEvaluationUsesIndependentFrozenEvidenceAndRetainsHistory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	_, result := taskEvaluationFixture(t, s, evaluationCriteria())
	first, created, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "missing", 0))
	if err != nil || !created || first.Definition.Outcome != "inconclusive" {
		t.Fatalf("worker success claim became evidence: %+v %v", first, err)
	}
	// Later changes to criteria do not weaken this attempt's frozen contract.
	if _, err := s.ReviseAcceptanceCriteria(ctx, "work", taskCriteria(), taskMutation(2)); err != nil {
		t.Fatal(err)
	}
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	passedRequest := evaluationRequest(result, "passed", 1)
	passed, created, err := s.EvaluateTaskResult(ctx, passedRequest)
	if err != nil || !created || passed.Definition.Outcome != "passed" || passed.CriteriaVersion != 2 || passed.Attribution.AttemptNumber != 2 || passed.Attribution.ResultNumber != 1 || passed.Attribution.ConfigurationHash != result.ConfigurationHash {
		t.Fatalf("incorrect verified provenance: %+v %v", passed, err)
	}
	evaluationChecks(t, s, result, strings.Repeat("b", 40), domain.PRCheckPassed)
	stale, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "stale", 2))
	if err != nil || stale.Definition.Outcome != "inconclusive" {
		t.Fatalf("stale PR head passed: %+v %v", stale, err)
	}
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckFailed)
	failed, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "failed", 3))
	if err != nil || failed.Definition.Outcome != "failed" {
		t.Fatalf("failed check accepted: %+v %v", failed, err)
	}
	if _, active, err := s.GetActiveTaskLease(ctx, "work"); err != nil || !active {
		t.Fatal("evaluation released worker ownership")
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	replay, created, err := reopened.EvaluateTaskResult(ctx, passedRequest)
	if err != nil || created || replay.ContentHash != passed.ContentHash || replay.Definition.Outcome != "passed" {
		t.Fatalf("retry rewrote historical evidence: %+v %v", replay, err)
	}
	items, err := reopened.ListTaskEvaluations(ctx, result.AttemptID, 1, 2)
	if err != nil || len(items) != 2 || items[0].ID != passed.ID || items[1].ID != stale.ID {
		t.Fatalf("history page: %+v %v", items, err)
	}
}

func TestTaskEvaluationFencesAuthorityVersionsAndConcurrentCollection(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, result := taskEvaluationFixture(t, s, evaluationCriteria())
	request := evaluationRequest(result, "evaluation", 0)
	for _, actor := range []domain.AdaptiveActor{{Kind: "WORKER", ID: string(result.SessionID), SessionID: result.SessionID}, {Kind: "AGENT_MANAGER", ID: "manager"}, {Kind: "ORCHESTRATOR", ID: string(result.SessionID), SessionID: result.SessionID}} {
		invalid := request
		invalid.Mutation.Actor = actor
		if _, _, err := s.EvaluateTaskResult(ctx, invalid); !errors.Is(err, ports.ErrTaskForbidden) {
			t.Fatalf("invalid evaluator authority: %v", err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		wg.Go(func() { _, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, id, 0)); results <- err })
	}
	wg.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, ports.ErrTaskConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent snapshots: %d %d", succeeded, conflicted)
	}
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "corrected", "corrected", 1
	if _, _, err := s.SubmitTaskResult(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "obsolete", 1)); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("obsolete result became latest assessment: %v", err)
	}
}

func TestTaskEvaluationAuditRollbackAndImmutableRetention(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	_, result := taskEvaluationFixture(t, s, evaluationCriteria())
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TRIGGER reject_evaluation_audit BEFORE INSERT ON adaptive_task_audit WHEN NEW.action='evaluation_recorded' BEGIN SELECT RAISE(ABORT,'injected audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	request := evaluationRequest(result, "evaluation", 0)
	if _, created, err := s.EvaluateTaskResult(ctx, request); err == nil || created {
		t.Fatal("failed audit retained evaluation")
	}
	if _, found, err := s.LatestTaskEvaluation(ctx, result.AttemptID); err != nil || found {
		t.Fatalf("failed transaction leaked: %v %v", found, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before {
		t.Fatal("failed evaluation emitted CDC")
	}
	if _, err := db.Exec(`DROP TRIGGER reject_evaluation_audit`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EvaluateTaskResult(ctx, request); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`UPDATE adaptive_task_evaluations SET snapshot='{}'`, `DELETE FROM adaptive_task_evaluations`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatal("evaluation history mutated")
		}
	}
	request.ID = "same-key-new-content"
	request.Mutation.Reason = "Rewrite assessment"
	if _, _, err := s.EvaluateTaskResult(ctx, request); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("idempotency content changed: %v", err)
	}
}

func TestTaskEvaluationEvidenceAndHistoryBounds(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	criteria := domain.AcceptanceCriteria{}
	checks := []domain.PullRequestCheck{}
	for i := range 9 {
		criterion := domain.AcceptanceCriterion{ID: fmt.Sprint(i), Requirement: "All named checks", EvidenceKind: "ci"}
		for j := range 16 {
			name := fmt.Sprintf("check-%02d-%02d", i, j)
			criterion.CheckNames = append(criterion.CheckNames, name)
			checks = append(checks, domain.PullRequestCheck{Name: name, CommitHash: strings.Repeat("a", 40), Status: domain.PRCheckPassed, Conclusion: "success", CreatedAt: time.Now().UTC()})
		}
		criteria.Criteria = append(criteria.Criteria, criterion)
	}
	_, result := taskEvaluationFixture(t, s, criteria)
	now := time.Now().UTC()
	if err := s.WriteSCMObservation(ctx, domain.PullRequest{URL: "https://example.test/pr/1", SessionID: result.SessionID, HeadSHA: result.Definition.ClaimedCommit, UpdatedAt: now, CIObservedAt: now}, checks, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	for i := range int64(64) {
		evaluation, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, fmt.Sprint(i), i))
		if err != nil || evaluation.Definition.Outcome != "inconclusive" || !evaluation.Definition.ChecksTruncated || len(evaluation.Definition.Checks) != 128 {
			t.Fatalf("bounded evidence: %+v %v", evaluation, err)
		}
	}
	if _, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "overflow", 64)); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("unbounded evaluations: %v", err)
	}
}

func TestTaskEvaluationExcludesCheckAbsentFromNewerSnapshot(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, result := taskEvaluationFixture(t, s, evaluationCriteria())
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	pr, found, err := s.GetPR(ctx, "https://example.test/pr/1")
	if err != nil || !found {
		t.Fatalf("PR missing: %v", err)
	}
	pr.CIObservedAt = pr.CIObservedAt.Add(time.Second)
	if err := s.WriteSCMObservation(ctx, pr, []domain.PullRequestCheck{{Name: "test", CommitHash: pr.HeadSHA, Status: domain.PRCheckPassed, Conclusion: "success", CreatedAt: pr.CIObservedAt}}, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	evaluation, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "missing-build", 0))
	if err != nil || evaluation.Definition.Outcome != "inconclusive" || len(evaluation.Definition.Checks) != 2 {
		t.Fatalf("retained removed check passed: %+v %v", evaluation, err)
	}
}

func TestTaskEvaluationAttributesOriginalAndLaterExecutionSegments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, result := taskEvaluationFixture(t, s, evaluationCriteria())
	rec, _, err := s.GetSession(ctx, result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := s.GetWorkerConfiguration(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution := prepareWorkerInterface(t, s, rec, original)
	if changed, err := s.CommitSessionControllerEpoch(ctx, rec.ID, domain.SessionModeTUI, domain.SessionModeChat, "native-worker", time.Now().UTC()); err != nil || !changed {
		t.Fatalf("activate Chat: %v %v", changed, err)
	}
	old, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "original-config", 0))
	if err != nil || old.Attribution.Mode != domain.SessionModeTUI || old.Attribution.ConfigurationHash != original.ContentHash || old.Attribution.ConfigurationSequence != 0 {
		t.Fatalf("current configuration rewrote old result: %+v %v", old, err)
	}
	if changed, err := s.AdvanceSessionInterfaceTransition(ctx, "worker-interface", domain.SessionInterfaceTransitionRequested, domain.SessionInterfaceTransitionCompleted, "native-worker", "", "", time.Now().UTC()); err != nil || !changed {
		t.Fatalf("finish interface fixture: %v %v", changed, err)
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
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "chat-result", "chat-result", 1
	input.SourceOwner, input.ExpectedActivation = rec.ControllerOwner(), sequence
	chatResult, _, err := s.SubmitTaskResult(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	rec.Metadata.Model = "later-provider-display-value"
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	current, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(chatResult, "chat-config", 1))
	if err != nil || current.Attribution.Mode != domain.SessionModeChat || current.Attribution.ConfigurationHash != execution.Configuration.ContentHash || current.Attribution.ConfigurationSequence != sequence || current.Attribution.Model != execution.Configuration.Effective.Config.Model {
		t.Fatalf("wrong result execution attribution: %+v %v", current, err)
	}
}
