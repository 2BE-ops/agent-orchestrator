package store_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func evaluationReview(t *testing.T, s *sqlite.Store, result domain.TaskResult, id, prURL, body string, status domain.ReviewRunStatus, verdict domain.ReviewVerdict, created time.Time) {
	t.Helper()
	ctx := context.Background()
	worker, _, err := s.GetSession(ctx, result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertReview(ctx, domain.Review{ID: "reviewer", SessionID: result.SessionID, ProjectID: worker.ProjectID, Harness: domain.ReviewerHarness("claude-code"), CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertReviewRun(ctx, domain.ReviewRun{ID: id, ReviewID: "reviewer", SessionID: result.SessionID, Harness: domain.ReviewerHarness("claude-code"), PRURL: prURL, TargetSHA: result.Definition.ClaimedCommit, Status: status, Verdict: verdict, Body: body, CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskEvaluationRetainsReviewAndLifecycleSourcesWithoutTrustingProse(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	_, result := taskEvaluationFixture(t, s, domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "review", Requirement: "Independent review of frozen requirements", EvidenceKind: "review"}}})
	now := time.Now().UTC()
	url := "https://example.test/pr/1"
	body := strings.Repeat("界", 2000)
	evaluationReview(t, s, result, "approved", url, body, domain.ReviewRunComplete, domain.VerdictApproved, now)
	first, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "first", 0))
	if err != nil {
		t.Fatal(err)
	}
	evidence := first.Definition.Observations
	if first.Definition.Outcome != "inconclusive" || evidence == nil || len(evidence.Reviews) != 1 || evidence.Reviews[0].RunID != "approved" || evidence.Reviews[0].BodyBytes != 6000 || !evidence.Reviews[0].BodyTruncated || !utf8.ValidString(evidence.Reviews[0].BodyPreview) || len(evidence.Reviews[0].BodyPreview) > 4096 || evidence.Reviews[0].BodyPreviewHash != domain.ContextTextHash(evidence.Reviews[0].BodyPreview) || !evidence.Worker.ReservationOngoing || evidence.Worker.SessionID != result.SessionID {
		t.Fatalf("review claim or source attribution: %+v", first)
	}
	// Existing review retry rows are append-only; historical statuses include
	// failed passes carrying a verdict, which must not hide a newer live pass.
	otherURL := "https://example.test/pr/2"
	evaluationReview(t, s, result, "older", otherURL, "An older approval", domain.ReviewRunFailed, domain.VerdictApproved, now)
	evaluationReview(t, s, result, "newer", otherURL, "", domain.ReviewRunRunning, domain.VerdictNone, now.Add(time.Millisecond))
	rec, _, err := s.GetSession(ctx, result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	rec.IsTerminated = true
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	lease, _, err := s.GetActiveTaskLease(ctx, result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	owner := rec.ControllerOwner()
	if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, SessionID: rec.ID, ObservedOwner: &owner, Reason: "Confirmed native termination", Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	second, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "second", 1))
	if err != nil {
		t.Fatal(err)
	}
	evidence = second.Definition.Observations
	if len(evidence.Reviews) != 2 || evidence.Reviews[1].RunID != "newer" || evidence.Reviews[1].Status != domain.ReviewRunRunning || !evidence.Worker.Terminated || evidence.Worker.ReservationOngoing || evidence.Worker.LeaseReleasedAt == nil || evidence.Worker.LeaseReleaseReason != "Confirmed native termination" {
		t.Fatalf("latest review/lifecycle facts: %+v", evidence)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	replay, created, err := reopened.EvaluateTaskResult(ctx, evaluationRequest(result, "first", 0))
	if err != nil || created || replay.ContentHash != first.ContentHash || replay.Definition.Observations.Worker.Terminated {
		t.Fatalf("retry recollected history: %+v %v", replay, err)
	}
}

func TestTaskEvaluationBoundsPRAndReviewCollections(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, result := taskEvaluationFixture(t, s, domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "merge", Requirement: "PRs are mergeable", EvidenceKind: "mergeability"}}})
	now := time.Now().UTC()
	for i := 0; i < 34; i++ {
		url := fmt.Sprintf("https://example.test/pr/%02d", i)
		if err := s.WriteSCMObservation(ctx, domain.PullRequest{URL: url, SessionID: result.SessionID, Number: i + 1, HeadSHA: result.Definition.ClaimedCommit, Mergeability: domain.MergeMergeable, ObservedAt: now, UpdatedAt: now}, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
			t.Fatal(err)
		}
		evaluationReview(t, s, result, fmt.Sprintf("review-%d", i), url, "Independent review", domain.ReviewRunComplete, domain.VerdictApproved, now)
	}
	evaluation, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "bounded", 0))
	if err != nil {
		t.Fatal(err)
	}
	evidence := evaluation.Definition.Observations
	if len(evidence.PRs) != 16 || !evidence.PRsTruncated || len(evidence.Reviews) != 32 || !evidence.ReviewsTruncated || evaluation.Definition.Outcome != "inconclusive" {
		t.Fatalf("unbounded or incomplete evidence passed: %+v", evidence)
	}
}

func TestTaskEvaluationCollectsMergeabilityForExactResultCommit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, result := taskEvaluationFixture(t, s, domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "merge", Requirement: "PR is mergeable", EvidenceKind: "mergeability"}}})
	for index, tc := range []struct {
		head  string
		merge domain.Mergeability
		want  string
	}{
		{result.Definition.ClaimedCommit, domain.MergeMergeable, "passed"},
		{strings.Repeat("b", 40), domain.MergeMergeable, "inconclusive"},
		{result.Definition.ClaimedCommit, domain.MergeConflicting, "failed"},
	} {
		now := time.Now().UTC()
		pr := domain.PullRequest{URL: "https://example.test/pr/1", SessionID: result.SessionID, Number: 1, HeadSHA: tc.head, Mergeability: tc.merge, ObservedAt: now, UpdatedAt: now}
		if err := s.WriteSCMObservation(ctx, pr, nil, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
			t.Fatal(err)
		}
		evaluation, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, fmt.Sprintf("merge-%d", index), int64(index)))
		if err != nil || evaluation.Definition.Outcome != tc.want || len(evaluation.Definition.Observations.PRs) != 1 || !evaluation.Definition.Observations.PRs[0].ObservedAt.Equal(now) {
			t.Fatalf("stored mergeability: %+v %v", evaluation, err)
		}
	}
	historical, err := s.GetTaskEvaluation(ctx, "merge-0")
	if err != nil || historical.Definition.Outcome != "passed" || historical.Definition.Observations.PRs[0].Mergeability != domain.MergeMergeable {
		t.Fatalf("history changed with PR: %+v %v", historical, err)
	}
}
