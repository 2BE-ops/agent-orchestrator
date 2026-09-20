package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// GetTaskCompletion derives current acceptance in a single transaction. It
// never rewrites an evaluation, releases a lease or probes a native process.
func (s *Store) GetTaskCompletion(ctx context.Context, taskID string, revision int64) (domain.TaskCompletion, error) {
	var completion domain.TaskCompletion
	if err := s.writeMu.LockContext(ctx); err != nil {
		return completion, err
	}
	defer s.writeMu.Unlock()
	err := s.inTx(ctx, "inspect current task completion", func(q *gen.Queries) error {
		var err error
		completion, err = currentTaskCompletion(ctx, q, taskID, revision, time.Now().UTC())
		return err
	})
	return completion, err
}

// currentTaskCompletion is also usable inside scheduler admission transactions;
// a read projection alone must never authorize a later unchecked reservation.
func currentTaskCompletion(ctx context.Context, q *gen.Queries, taskID string, revision int64, now time.Time) (domain.TaskCompletion, error) {
	proof := domain.TaskCompletion{TaskID: taskID, TaskRevision: revision, Reason: "No independently assessed result exists for the current revision"}
	task, err := q.GetAdaptiveTask(ctx, taskID)
	if err != nil {
		return proof, taskReadError(err)
	}
	if task.Revision != revision {
		return proof, ports.ErrTaskConflict
	}
	if err := requireTaskRunIntent(ctx, q, taskID); err != nil {
		if errors.Is(err, ports.ErrTaskLeaseFenced) {
			proof.Reason = "Cancellation intent prevents current completion"
			return proof, nil
		}
		return proof, err
	}
	attempt, err := q.LatestTaskAttempt(ctx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return proof, nil
	}
	if err != nil {
		return proof, err
	}
	proof.AttemptID = attempt.ID
	if attempt.TaskRevision != revision {
		proof.Reason = "The latest attempt belongs to an earlier task revision"
		return proof, nil
	}
	resultRow, err := q.LatestTaskResult(ctx, attempt.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return proof, nil
	}
	if err != nil {
		return proof, err
	}
	result, err := taskResultFromRow(resultRow)
	if err != nil {
		return proof, err
	}
	proof.ResultID = result.ID
	evaluationRow, err := q.LatestTaskEvaluation(ctx, attempt.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return proof, nil
	}
	if err != nil {
		return proof, err
	}
	evaluation, err := taskEvaluationFromRow(evaluationRow)
	if err != nil {
		return proof, err
	}
	proof.EvaluationID = evaluation.ID
	if evaluation.ResultID != result.ID || evaluation.TaskRevision != revision || evaluation.CriteriaVersion != attempt.CriteriaVersion || evaluation.Definition.ResultHash != result.ContentHash || evaluation.Attribution.ConfigurationHash != result.ConfigurationHash {
		proof.Reason = "The latest result or task revision supersedes the retained assessment"
		return proof, nil
	}
	if evaluation.Definition.Outcome != "passed" {
		proof.Reason = "The latest independent assessment does not pass all frozen criteria"
		return proof, nil
	}
	criteriaRow, err := q.GetAdaptiveTaskCriteria(ctx, gen.GetAdaptiveTaskCriteriaParams{TaskID: taskID, Number: attempt.CriteriaVersion})
	if err != nil {
		return proof, err
	}
	criteria, err := taskCriteriaFromRow(criteriaRow)
	if err != nil {
		return proof, err
	}
	if criteria.ContentHash != evaluation.Definition.CriteriaHash {
		return proof, ports.ErrTaskConflict
	}
	configuration, err := evaluationWorkerConfiguration(ctx, q, result)
	if err != nil {
		return proof, err
	}
	checks, truncated, err := collectEvaluationChecks(ctx, q, result, criteria.Definition)
	if err != nil {
		return proof, err
	}
	var reviewTarget *domain.TaskReviewTarget
	if criteria.Definition.ReviewPolicy != nil {
		reviewTarget = &domain.TaskReviewTarget{ResultID: result.ID, ResultHash: result.ContentHash, CriteriaHash: criteria.ContentHash, ImplementingType: configuration.AgentType, ImplementingHarness: configuration.Effective.Harness, ImplementingConfigurationHash: result.ConfigurationHash}
	}
	observations, err := collectTaskObservations(ctx, q, result, reviewTarget, attempt.CreatedAt, now)
	if err != nil {
		return proof, err
	}
	// Git blobs addressed by exact commit/object IDs are immutable. Reuse the
	// previously verified artifact observation without filesystem I/O in SQLite.
	if retained := evaluation.Definition.Observations; retained != nil {
		observations.Artifacts = retained.Artifacts
		observations.ArtifactsTruncated = retained.ArtifactsTruncated
		if observations.PRsTruncated || retained.PRsTruncated {
			proof.Reason = "A complete current PR observation is required to retain completion"
			return proof, nil
		}
		for _, priorPR := range retained.PRs {
			if priorPR.HeadCommit != result.Definition.ClaimedCommit {
				continue
			}
			current := false
			for _, pr := range observations.PRs {
				if pr.URL == priorPR.URL && pr.HeadCommit == priorPR.HeadCommit && !pr.ObservedAt.IsZero() && !pr.ObservedAt.After(now.Add(30*time.Second)) {
					current = true
					break
				}
			}
			if !current {
				proof.Reason = "Current PR head evidence is missing, changed or does not match the exact result commit"
				return proof, nil
			}
		}
	}
	_, outcome, reason := domain.EvaluateTaskEvidence(criteria.Definition, result.Definition.ClaimedCommit, checks, truncated, &observations, now)
	proof.Verified = outcome == "passed"
	proof.Reason = reason
	return proof, nil
}
