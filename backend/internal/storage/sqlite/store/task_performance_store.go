package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.TaskPerformanceStore = (*Store)(nil)

// ListTaskPerformance reads an admission cohort in one database snapshot. It
// retains unseeded attempts in the denominator and never consults live registry
// versions or interprets historical success as current task completion.
func (s *Store) ListTaskPerformance(ctx context.Context, query domain.TaskPerformanceQuery) (domain.TaskPerformancePage, error) {
	page := domain.TaskPerformancePage{Items: []domain.TaskPerformanceAttempt{}, From: query.From, To: query.To}
	if err := query.Validate(); err != nil {
		return page, fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return page, err
	}
	defer s.writeMu.Unlock()
	err := s.inTx(ctx, "read task performance cohort", func(q *gen.Queries) error {
		page.ObservedAt = time.Now().UTC()
		if _, err := q.GetProject(ctx, query.ProjectID); err != nil {
			return taskReadError(err)
		}
		rows, err := q.ListTaskPerformanceAttempts(ctx, gen.ListTaskPerformanceAttemptsParams{ProjectID: string(query.ProjectID), WindowStart: query.From.UTC(), WindowEnd: query.To.UTC(), AfterID: query.After, PageLimit: int64(query.Limit + 1)})
		if err != nil {
			return err
		}
		for _, row := range rows[:min(len(rows), query.Limit)] {
			item, err := taskPerformanceAttempt(ctx, q, row, page.ObservedAt)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, item)
		}
		if len(rows) > query.Limit {
			page.NextCursor = page.Items[len(page.Items)-1].AttemptID
		}
		return nil
	})
	if err != nil {
		// Never expose a partial aggregate after a corrupt row or integer overflow.
		page.Items, page.NextCursor = []domain.TaskPerformanceAttempt{}, ""
	}
	return page, err
}

func taskPerformanceAttempt(ctx context.Context, q *gen.Queries, attempt gen.AdaptiveTaskAttempt, now time.Time) (domain.TaskPerformanceAttempt, error) {
	item := domain.TaskPerformanceAttempt{AttemptID: attempt.ID, TaskID: attempt.TaskID, TaskRevision: attempt.TaskRevision, CriteriaVersion: attempt.CriteriaVersion, AttemptNumber: attempt.Number, CreatedAt: attempt.CreatedAt, RequiredCapabilities: []string{}, AssessedOutcome: "unassessed", Usage: domain.TaskPerformanceUsage{Scope: "attempt_session", Incomplete: true}}
	row, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: attempt.TaskID, Number: attempt.TaskRevision})
	if err != nil {
		return item, err
	}
	revision, err := taskRevisionFromRow(row)
	if err != nil {
		return item, err
	}
	item.Category = revision.Definition.Category
	item.RequiredCapabilities = append(item.RequiredCapabilities, revision.Definition.RequiredCapabilities...)
	lease, err := q.GetTaskLease(ctx, attempt.ID)
	if err != nil {
		return item, err
	}
	end := now
	item.ReservationOngoing = !lease.ReleasedAt.Valid
	if lease.ReleasedAt.Valid {
		end = lease.ReleasedAt.Time
	}
	item.ReservationElapsedMS = max(0, end.Sub(attempt.CreatedAt).Milliseconds())
	dispatch, err := q.GetTaskWorkerDispatch(ctx, attempt.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return item, err
	}
	item.SessionID = domain.SessionID(dispatch.SessionID)
	configurationRow, err := q.GetWorkerConfiguration(ctx, dispatch.SessionID)
	if err != nil {
		return item, err
	}
	original, err := workerConfigurationFromRow(configurationRow)
	if err != nil {
		return item, err
	}
	configuration := original
	result := domain.TaskResult{ConfigurationHash: original.ContentHash}
	resultRow, err := q.LatestTaskResult(ctx, attempt.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return item, err
	}
	if err == nil {
		result, err = taskResultFromRow(resultRow)
		if err != nil {
			return item, err
		}
		configuration, err = evaluationWorkerConfiguration(ctx, q, result)
		if err != nil {
			return item, err
		}
		item.ResultID, item.ResultVersions = result.ID, result.Number
	}
	attribution := taskEvaluationAttribution(configuration, item.Category, attempt.Number, result)
	item.Configuration = &attribution
	activations, err := q.TaskPerformanceActivations(ctx, dispatch.SessionID)
	if err != nil {
		return item, err
	}
	// Even a later rollback does not erase exposure to another configuration.
	item.MixedConfigurations = activations > 0 || configuration.ContentHash != original.ContentHash
	if err := taskPerformanceAssessments(ctx, q, &item); err != nil {
		return item, err
	}
	item.Usage, err = taskPerformanceUsage(ctx, q, item.SessionID)
	return item, err
}

func taskEvaluationAttribution(configuration domain.WorkerConfiguration, category string, attemptNumber int64, result domain.TaskResult) domain.TaskEvaluationAttribution {
	attribution := domain.TaskEvaluationAttribution{AgentType: configuration.AgentType, Skills: []domain.WorkerDefinitionRef{}, Harness: configuration.Effective.Harness, Mode: configuration.Effective.SessionMode, Model: configuration.Effective.Config.Model, Category: category, AttemptNumber: attemptNumber, ResultNumber: result.Number, ConfigurationHash: result.ConfigurationHash, ConfigurationSequence: result.ConfigurationSequence}
	for _, skill := range configuration.Skills {
		attribution.Skills = append(attribution.Skills, skill.Reference)
	}
	return attribution
}

func taskPerformanceAssessments(ctx context.Context, q *gen.Queries, item *domain.TaskPerformanceAttempt) error {
	criteriaRow, err := q.GetAdaptiveTaskCriteria(ctx, gen.GetAdaptiveTaskCriteriaParams{TaskID: item.TaskID, Number: item.CriteriaVersion})
	if err != nil {
		return err
	}
	criteria, err := taskCriteriaFromRow(criteriaRow)
	if err != nil {
		return err
	}
	ciIDs := []string{}
	for _, criterion := range criteria.Definition.Criteria {
		switch criterion.EvidenceKind {
		case "ci", "test", "build", "lint":
			ciIDs = append(ciIDs, criterion.ID)
		}
	}
	encoded, err := json.Marshal(ciIDs)
	if err != nil {
		return err
	}
	stats, err := q.TaskPerformanceAssessmentStats(ctx, gen.TaskPerformanceAssessmentStatsParams{AttemptID: item.AttemptID, CICriteria: string(encoded)})
	if err != nil {
		return err
	}
	item.Evaluations, item.CIFailureObserved = stats.Evaluations, stats.CIFailureObserved != 0
	item.ReviewChangesRequested, err = q.TaskPerformanceReviewChanges(ctx, item.AttemptID)
	if err != nil || item.Evaluations == 0 {
		return err
	}
	latestRow, err := q.LatestTaskEvaluation(ctx, item.AttemptID)
	if err != nil {
		return err
	}
	latest, err := taskEvaluationFromRow(latestRow)
	if err != nil {
		return err
	}
	item.EvaluationID, item.AssessedOutcome = latest.ID, latest.Definition.Outcome
	if latest.ResultID != item.ResultID {
		item.AssessedOutcome = "superseded"
	}
	if item.AttemptNumber != 1 || item.ResultVersions != 1 || item.AssessedOutcome != "passed" {
		return nil
	}
	firstRow, err := q.FirstTaskEvaluation(ctx, item.AttemptID)
	if err != nil {
		return err
	}
	first, err := taskEvaluationFromRow(firstRow)
	if err != nil {
		return err
	}
	item.FirstPassCompleted = first.ResultID == item.ResultID && first.Definition.Outcome == "passed"
	return nil
}

func taskPerformanceUsage(ctx context.Context, q *gen.Queries, sessionID domain.SessionID) (domain.TaskPerformanceUsage, error) {
	usage := domain.TaskPerformanceUsage{Scope: "attempt_session", Incomplete: true}
	row, err := q.TaskPerformanceUsage(ctx, sessionID)
	if err != nil {
		return usage, err
	}
	incomplete, err := q.GetUsageSessionIncomplete(ctx, string(sessionID))
	if err != nil {
		return usage, err
	}
	usage.Events, usage.NativeReportedEvents, usage.EstimatedEvents, usage.UnknownEvents = row.Events, row.NativeReportedEvents, row.EstimatedEvents, row.UnknownEvents
	usage.InputTokens = int64PtrWhen(row.InputTokens, row.Events > 0 && row.KnownInputEvents == row.Events)
	usage.OutputTokens = int64PtrWhen(row.OutputTokens, row.Events > 0 && row.KnownOutputEvents == row.Events)
	usage.PricedEvents, usage.PricedCostNanos = row.PricedEvents, row.PricedCostNanos
	usage.Incomplete = incomplete != 0 || row.Events == 0 || usage.InputTokens == nil || usage.OutputTokens == nil
	return usage, nil
}
