package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestTaskEvaluationRequiresPinnedReviewAndObservedLaunch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, run, frozen := taskReviewFixture(t, s)
	result, err := s.GetTaskResult(ctx, frozen.ResultID)
	if err != nil {
		t.Fatal(err)
	}
	generic := run
	generic.ID, generic.TaskScope, generic.Status, generic.Verdict = "generic-approval", "", domain.ReviewRunComplete, domain.VerdictApproved
	if err := s.InsertReviewRun(ctx, generic); err != nil {
		t.Fatal(err)
	}
	assess := func(id string, number int64, want string) domain.TaskEvaluation {
		t.Helper()
		e, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, id, number))
		if err != nil || e.Definition.Outcome != want {
			t.Fatalf("%s: %+v %v", id, e, err)
		}
		return e
	}
	assess("generic-only", 0, "inconclusive")
	if err := s.InsertTaskReviewRun(ctx, run, frozen); err != nil {
		t.Fatal(err)
	}
	claim := domain.Review{ID: run.ReviewID, SessionID: run.SessionID, ProjectID: "project", Harness: run.Harness, ReviewerLaunchID: frozen.LaunchID, CreatedAt: run.CreatedAt, UpdatedAt: time.Now().UTC()}
	if err := s.UpsertReview(ctx, claim); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitTaskReviewResult(ctx, domain.TaskReviewSubmission{RunID: run.ID, SessionID: run.SessionID, SourceGeneration: frozen.LaunchID, Verdict: domain.VerdictApproved, Body: "No blocking findings"}); err != nil {
		t.Fatal(err)
	}
	assess("unwitnessed", 1, "inconclusive")
	claim.ReviewerHandleID = "native-handle"
	if err := s.UpsertReview(ctx, claim); err != nil {
		t.Fatal(err)
	}
	if started, err := s.MarkTaskReviewStarted(ctx, run.ID, frozen.LaunchID); err != nil || !started {
		t.Fatalf("launch witness: %v %v", started, err)
	}
	passed := assess("native-approved", 2, "passed")
	if proof, err := s.GetTaskCompletion(ctx, result.TaskID, result.TaskRevision); err != nil || !proof.Verified {
		t.Fatalf("native review completion: %+v %v", proof, err)
	}
	var attributed *domain.TaskReviewAttribution
	for _, e := range passed.Definition.Observations.Reviews {
		if e.RunID == run.ID {
			attributed = e.Attribution
		}
	}
	if attributed == nil || attributed.ContextHash != frozen.ContentHash || attributed.ConfigurationHash != frozen.Reviewer.ContentHash || attributed.ReviewerType != frozen.Reviewer.AgentType || attributed.Target.ResultID != result.ID || attributed.StartedAt == nil {
		t.Fatalf("lost native attribution: %+v", attributed)
	}
	// A new effective review scope must not be hidden by an older approval.
	newContext := frozen
	newContext.LaunchID = "later-launch"
	newContext.CreatedAt = time.Now().UTC()
	newContext.Reviewer.SystemPrompt = "Assess independently with retained task context"
	newContext.Reviewer.ContentHash = newContext.Reviewer.Hash()
	newContext.ContentHash = newContext.Hash()
	newRun := run
	newRun.ID, newRun.TaskScope, newRun.CreatedAt = "later-pass", newContext.ScopeHash(), newContext.CreatedAt
	if err := s.InsertTaskReviewRun(ctx, newRun, newContext); err != nil {
		t.Fatal(err)
	}
	if proof, err := s.GetTaskCompletion(ctx, result.TaskID, result.TaskRevision); err != nil || proof.Verified {
		t.Fatalf("new review retained old completion without reassessment: %+v %v", proof, err)
	}
	assess("newer-running", 3, "inconclusive")
	if _, err := s.UpdateReviewRunResult(ctx, newRun.ID, domain.ReviewRunFailed, domain.VerdictNone, "Native provider failed", "", false); err != nil {
		t.Fatal(err)
	}
	assess("newer-failed", 4, "inconclusive")
	newContext.LaunchID = "changes-launch"
	newContext.CreatedAt = time.Now().UTC()
	newContext.ContentHash = newContext.Hash()
	newRun.ID, newRun.CreatedAt = "changes-pass", newContext.CreatedAt
	if err := s.InsertTaskReviewRun(ctx, newRun, newContext); err != nil {
		t.Fatal(err)
	}
	claim.ReviewerLaunchID = newContext.LaunchID
	if err := s.UpsertReview(ctx, claim); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkTaskReviewStarted(ctx, newRun.ID, newContext.LaunchID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitTaskReviewResult(ctx, domain.TaskReviewSubmission{RunID: newRun.ID, SessionID: newRun.SessionID, SourceGeneration: newContext.LaunchID, Verdict: domain.VerdictChangesRequested, Body: "A required error path is missing"}); err != nil {
		t.Fatal(err)
	}
	assess("changes-requested", 5, "failed")
	metadata := registryMetadata("Disabled after retained review")
	metadata.Enabled = false
	if _, err := s.UpdateRegistryMetadata(ctx, frozen.Reviewer.AgentType.ID, metadata, registryMutation(domain.RegistryUser, 1)); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	replay, created, err := reopened.EvaluateTaskResult(ctx, evaluationRequest(result, "native-approved", 2))
	if err != nil || created || replay.ContentHash != passed.ContentHash || replay.Definition.Outcome != "passed" {
		t.Fatalf("reopened evaluation lost history: %+v %v", replay, err)
	}
	// A corrected result at the same commit does not inherit the old approval.
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "corrected-result", "corrected", 1
	input.Definition.Summary = "Corrected implementation claims"
	result, _, err = s.SubmitTaskResult(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	corrected := assess("corrected-no-review", 6, "inconclusive")
	for _, e := range corrected.Definition.Observations.Reviews {
		if e.Attribution != nil {
			t.Fatalf("old result review leaked into corrected result: %+v", e)
		}
	}
}
