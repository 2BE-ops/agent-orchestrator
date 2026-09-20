package store_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func performanceQuery() domain.TaskPerformanceQuery {
	return domain.TaskPerformanceQuery{ProjectID: "project", From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), Limit: 100}
}

func performanceAttempt(t *testing.T, s *sqlite.Store, id string) domain.TaskPerformanceAttempt {
	t.Helper()
	page, err := s.ListTaskPerformance(context.Background(), performanceQuery())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.AttemptID == id {
			return item
		}
	}
	t.Fatalf("missing attempt %s: %+v", id, page)
	return domain.TaskPerformanceAttempt{}
}

func TestTaskPerformanceCohortIncludesUnseededAttemptsAndBoundsPages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "project")
	seedProject(t, s, "other")
	definition := taskDefinition()
	definition.Category, definition.RequiredCapabilities = "backend", []string{"go"}
	createTask(t, s, "work", definition)
	_, lease := reserveTask(t, s, "a", "work")
	if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, Reason: "Unseeded launch cancelled", Now: lease.HeartbeatAt.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	reserveTask(t, s, "b", "work")
	query := performanceQuery()
	query.Limit = 1
	page, err := s.ListTaskPerformance(ctx, query)
	if err != nil || len(page.Items) != 1 || page.NextCursor != "a" || page.ObservedAt.IsZero() || !page.From.Equal(query.From) || !page.To.Equal(query.To) {
		t.Fatalf("first page: %+v %v", page, err)
	}
	a := page.Items[0]
	if a.Configuration != nil || a.SessionID != "" || a.ReservationOngoing || a.ReservationElapsedMS != 1000 || a.Category != "backend" || !reflect.DeepEqual(a.RequiredCapabilities, []string{"go"}) || a.AssessedOutcome != "unassessed" || !a.Usage.Incomplete || a.Usage.InputTokens != nil {
		t.Fatalf("invented attribution for unseeded reservation: %+v", a)
	}
	query.After = page.NextCursor
	page, err = s.ListTaskPerformance(ctx, query)
	if err != nil || len(page.Items) != 1 || page.NextCursor != "" || page.Items[0].AttemptNumber != 2 || !page.Items[0].ReservationOngoing {
		t.Fatalf("second page: %+v %v", page, err)
	}
	query = performanceQuery()
	query.From, query.To = a.CreatedAt, a.CreatedAt.Add(time.Nanosecond)
	page, err = s.ListTaskPerformance(ctx, query)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("inclusive admission boundary: %+v %v", page, err)
	}
	query.From, query.To = a.CreatedAt.Add(-time.Hour), a.CreatedAt
	page, err = s.ListTaskPerformance(ctx, query)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("exclusive admission boundary: %+v %v", page, err)
	}
	query = performanceQuery()
	query.ProjectID = "other"
	page, err = s.ListTaskPerformance(ctx, query)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("project isolation: %+v %v", page, err)
	}
	query.ProjectID = "missing"
	if _, err := s.ListTaskPerformance(ctx, query); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("missing project: %v", err)
	}
	query = performanceQuery()
	query.Limit = 101
	if _, err := s.ListTaskPerformance(ctx, query); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("unbounded query: %v", err)
	}
}

func TestTaskPerformanceRetainsFirstPassAndHistoricalAssessmentWithoutInflatingAttempts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, _, _ := taskResultFixture(t, s, true, evaluationCriteria())
	result, _, err := s.SubmitTaskResult(ctx, input)
	mustNoError(t, err)
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	assessment, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "first-pass", 0))
	mustNoError(t, err)
	_, _, err = s.EvaluateTaskResult(ctx, evaluationRequest(result, "first-pass", 0))
	mustNoError(t, err)
	item := performanceAttempt(t, s, result.AttemptID)
	if !item.FirstPassCompleted || item.AssessedOutcome != "passed" || item.Evaluations != 1 || item.ResultVersions != 1 || item.Configuration == nil || !reflect.DeepEqual(*item.Configuration, assessment.Attribution) || item.MixedConfigurations {
		t.Fatalf("first pass evidence: %+v", item)
	}
	// Current failure invalidates completion, but does not rewrite history.
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckFailed)
	proof, err := s.GetTaskCompletion(ctx, result.TaskID, result.TaskRevision)
	if err != nil || proof.Verified || performanceAttempt(t, s, result.AttemptID).AssessedOutcome != "passed" {
		t.Fatalf("historical and current completion conflated: %+v %v", proof, err)
	}
	_, _, err = s.EvaluateTaskResult(ctx, evaluationRequest(result, "ci-failed", 1))
	mustNoError(t, err)
	_, _, err = s.EvaluateTaskResult(ctx, evaluationRequest(result, "ci-failed-again", 2))
	mustNoError(t, err)
	item = performanceAttempt(t, s, result.AttemptID)
	if !item.CIFailureObserved || item.FirstPassCompleted || item.Evaluations != 3 || item.AssessedOutcome != "failed" {
		t.Fatalf("CI evidence: %+v", item)
	}
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "correction", "correction", 1
	result, _, err = s.SubmitTaskResult(ctx, input)
	mustNoError(t, err)
	item = performanceAttempt(t, s, result.AttemptID)
	if item.AssessedOutcome != "superseded" || item.ResultVersions != 2 || item.ResultID != result.ID {
		t.Fatalf("old assessment applied to correction: %+v", item)
	}
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	_, _, err = s.EvaluateTaskResult(ctx, evaluationRequest(result, "corrected-pass", 3))
	mustNoError(t, err)
	metadata := registryMetadata("Renamed and disabled")
	metadata.Enabled = false
	_, err = s.UpdateRegistryMetadata(ctx, item.Configuration.AgentType.ID, metadata, registryMutation(domain.RegistryUser, 1))
	mustNoError(t, err)
	reopened, err := sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	page, err := reopened.ListTaskPerformance(ctx, performanceQuery())
	if err != nil || len(page.Items) != 1 || page.Items[0].AssessedOutcome != "passed" || page.Items[0].FirstPassCompleted || !page.Items[0].CIFailureObserved || page.Items[0].Evaluations != 4 || page.Items[0].Configuration.AgentType != assessment.Attribution.AgentType {
		t.Fatalf("restart/denominator: %+v %v", page, err)
	}
}

func TestTaskPerformanceReassessmentDoesNotRecoverFirstPassCredit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	input, _, _ := taskResultFixture(t, s, true, evaluationCriteria())
	result, _, err := s.SubmitTaskResult(ctx, input)
	mustNoError(t, err)
	_, _, err = s.EvaluateTaskResult(ctx, evaluationRequest(result, "missing-checks", 0))
	mustNoError(t, err)
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	_, _, err = s.EvaluateTaskResult(ctx, evaluationRequest(result, "later-pass", 1))
	mustNoError(t, err)
	if item := performanceAttempt(t, s, result.AttemptID); item.FirstPassCompleted || item.AssessedOutcome != "passed" || item.Evaluations != 2 {
		t.Fatalf("later assessment recovered first-pass credit: %+v", item)
	}
}

func TestTaskPerformanceUsagePreservesUnknownZeroEstimationAndOverflow(t *testing.T) {
	for _, scenario := range []string{"missing", "zero", "partial", "estimated", "overflow"} {
		t.Run(scenario, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			rec, lease := taskExecutionSeed(t, s)
			if scenario != "missing" {
				source := seedUsageSource(t, s, rec, time.Now().UTC())
				events := []domain.ModelUsageEvent{usageEvent("zero", canonicalUsageTokens(0, 0, 0, 0))}
				switch scenario {
				case "partial":
					events = append(events, usageEvent("unknown", domain.UsageTokenMetrics{}))
				case "estimated":
					events[0].MeasurementKind = domain.UsageMeasurementAOEstimated
				case "overflow":
					events = []domain.ModelUsageEvent{usageEvent("huge1", canonicalUsageTokens(math.MaxInt64, 0, math.MaxInt64, 0)), usageEvent("huge2", canonicalUsageTokens(math.MaxInt64, 0, math.MaxInt64, 0))}
				}
				err := s.ApplyUsageChunk(ctx, source.ID, 0, source.UpdatedAt, domain.SourceCursorState{ByteOffset: 10, State: domain.UsageSourceComplete, UpdatedAt: time.Now().UTC()}, events)
				mustNoError(t, err)
			}
			page, err := s.ListTaskPerformance(ctx, performanceQuery())
			if scenario == "overflow" {
				if err == nil || !strings.Contains(err.Error(), "integer overflow") || len(page.Items) != 0 {
					t.Fatalf("overflow produced partial/zero data: %+v %v", page, err)
				}
				return
			}
			if err != nil || len(page.Items) != 1 || page.Items[0].AttemptID != lease.AttemptID {
				t.Fatalf("usage cohort: %+v %v", page, err)
			}
			u := page.Items[0].Usage
			if u.Scope != "attempt_session" || u.PricedEvents != 0 {
				t.Fatalf("invented usage price/scope: %+v", u)
			}
			switch scenario {
			case "missing", "partial":
				if !u.Incomplete || u.InputTokens != nil || u.OutputTokens != nil || (scenario == "partial" && u.Events != 2) {
					t.Fatalf("unknown reported as zero: %+v", u)
				}
			case "zero", "estimated":
				if u.Events != 1 || u.Incomplete || u.InputTokens == nil || *u.InputTokens != 0 || u.OutputTokens == nil || *u.OutputTokens != 0 || (scenario == "estimated" && u.EstimatedEvents != 1) || (scenario == "zero" && u.NativeReportedEvents != 1) {
					t.Fatalf("zero/native/estimated facts lost: %+v", u)
				}
			}
		})
	}
}

func TestTaskPerformanceCountsOnlyWitnessedNativeReviewChanges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, run, frozen := taskReviewFixture(t, s)
	generic := run
	generic.ID, generic.TaskScope, generic.Status, generic.Verdict = "generic-changes", "", domain.ReviewRunComplete, domain.VerdictChangesRequested
	mustNoError(t, s.InsertReviewRun(ctx, generic))
	mustNoError(t, s.InsertTaskReviewRun(ctx, run, frozen))
	claim := domain.Review{ID: run.ReviewID, SessionID: run.SessionID, ProjectID: "project", Harness: run.Harness, ReviewerLaunchID: frozen.LaunchID, CreatedAt: run.CreatedAt, UpdatedAt: time.Now().UTC()}
	mustNoError(t, s.UpsertReview(ctx, claim))
	_, _, err := s.SubmitTaskReviewResult(ctx, domain.TaskReviewSubmission{RunID: run.ID, SessionID: run.SessionID, SourceGeneration: frozen.LaunchID, Verdict: domain.VerdictChangesRequested, Body: "Missing error handling"})
	mustNoError(t, err)
	if item := performanceAttempt(t, s, frozen.AttemptID); item.ReviewChangesRequested != 0 {
		t.Fatalf("unwitnessed changes counted: %+v", item)
	}
	claim.ReviewerHandleID = "native-handle"
	mustNoError(t, s.UpsertReview(ctx, claim))
	_, err = s.MarkTaskReviewStarted(ctx, run.ID, frozen.LaunchID)
	mustNoError(t, err)
	if item := performanceAttempt(t, s, frozen.AttemptID); item.ReviewChangesRequested != 1 {
		t.Fatalf("witnessed review missing: %+v", item)
	}
}
