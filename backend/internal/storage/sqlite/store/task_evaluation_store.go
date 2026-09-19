package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.TaskEvaluationStore = (*Store)(nil)

func taskEvaluationFromRow(row gen.AdaptiveTaskEvaluation) (domain.TaskEvaluation, error) {
	var evaluation domain.TaskEvaluation
	if err := json.Unmarshal([]byte(row.Snapshot), &evaluation); err != nil {
		return evaluation, err
	}
	if err := evaluation.Definition.Validate(); err != nil {
		return evaluation, err
	}
	if evaluation.ID != row.ID || string(evaluation.ProjectID) != row.ProjectID || evaluation.TaskID != row.TaskID || evaluation.AttemptID != row.AttemptID || evaluation.ResultID != row.ResultID || evaluation.Number != row.Number || evaluation.TaskRevision != row.TaskRevision || evaluation.CriteriaVersion != row.CriteriaVersion || !evaluation.CreatedAt.Equal(row.CreatedAt) || evaluation.ContentHash != row.ContentHash || evaluation.Hash() != row.ContentHash {
		return evaluation, fmt.Errorf("task evaluation provenance hash mismatch")
	}
	return evaluation, nil
}

// EvaluateTaskResult snapshots stored SCM observations under the same writer
// transaction as evaluation/audit. It never executes worker-reported commands,
// changes criteria, or releases a worker's lease.
func (s *Store) EvaluateTaskResult(ctx context.Context, input domain.TaskEvaluationRequest) (domain.TaskEvaluation, bool, error) {
	var evaluation domain.TaskEvaluation
	for _, value := range []string{input.ID, input.ResultID, input.IdempotencyKey} {
		if strings.TrimSpace(value) == "" || len(value) > 200 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return evaluation, false, ports.ErrTaskInvalid
		}
	}
	if input.ExpectedVersion < 0 || input.ExpectedVersion >= 64 {
		return evaluation, false, ports.ErrTaskInvalid
	}
	if err := validateTaskMutation(input.Mutation); err != nil {
		return evaluation, false, err
	}
	_, requestHash, err := domain.TaskContent(struct {
		ResultID        string
		ExpectedVersion int64
		Mutation        domain.TaskMutation
	}{input.ResultID, input.ExpectedVersion, input.Mutation})
	if err != nil {
		return evaluation, false, err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return evaluation, false, err
	}
	defer s.writeMu.Unlock()
	created := false
	err = s.inTx(ctx, "collect independent task evaluation", func(q *gen.Queries) error {
		row, err := q.GetTaskResult(ctx, input.ResultID)
		if err != nil {
			return taskReadError(err)
		}
		result, err := taskResultFromRow(row)
		if err != nil {
			return err
		}
		task, err := q.GetAdaptiveTask(ctx, result.TaskID)
		if err != nil {
			return taskReadError(err)
		}
		if err := validateTaskActor(ctx, q, task.ProjectID, input.Mutation.Actor); err != nil {
			return err
		}
		previous, err := q.GetTaskEvaluationByKey(ctx, gen.GetTaskEvaluationByKeyParams{AttemptID: result.AttemptID, IdempotencyKey: input.IdempotencyKey})
		if err == nil {
			if previous.RequestHash != requestHash {
				return ports.ErrTaskConflict
			}
			evaluation, err = taskEvaluationFromRow(previous)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		latestResult, err := q.LatestTaskResult(ctx, result.AttemptID)
		if err != nil {
			return err
		}
		if latestResult.ID != result.ID {
			return ports.ErrTaskConflict
		}
		latest, err := q.LatestTaskEvaluation(ctx, result.AttemptID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if latest.Number != input.ExpectedVersion {
			return ports.ErrTaskConflict
		}
		attempt, err := q.GetTaskAttempt(ctx, result.AttemptID)
		if err != nil {
			return err
		}
		criteriaRow, err := q.GetAdaptiveTaskCriteria(ctx, gen.GetAdaptiveTaskCriteriaParams{TaskID: result.TaskID, Number: result.CriteriaVersion})
		if err != nil {
			return err
		}
		criteria, err := taskCriteriaFromRow(criteriaRow)
		if err != nil {
			return err
		}
		revisionRow, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: result.TaskID, Number: result.TaskRevision})
		if err != nil {
			return err
		}
		revision, err := taskRevisionFromRow(revisionRow)
		if err != nil {
			return err
		}
		configuration, err := evaluationWorkerConfiguration(ctx, q, result)
		if err != nil {
			return err
		}
		checks, truncated, err := collectEvaluationChecks(ctx, q, result, criteria.Definition)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		decisions, outcome, reason := domain.EvaluateTaskCriteria(criteria.Definition, result.Definition.ClaimedCommit, checks, truncated, now)
		attribution := domain.TaskEvaluationAttribution{AgentType: configuration.AgentType, Skills: []domain.WorkerDefinitionRef{}, Harness: configuration.Effective.Harness, Mode: configuration.Effective.SessionMode, Model: configuration.Effective.Config.Model, Category: revision.Definition.Category, AttemptNumber: attempt.Number, ResultNumber: result.Number, ConfigurationHash: result.ConfigurationHash, ConfigurationSequence: result.ConfigurationSequence}
		for _, skill := range configuration.Skills {
			attribution.Skills = append(attribution.Skills, skill.Reference)
		}
		evaluation = domain.TaskEvaluation{ID: input.ID, ProjectID: domain.ProjectID(task.ProjectID), TaskID: result.TaskID, AttemptID: result.AttemptID, ResultID: result.ID, Number: latest.Number + 1, TaskRevision: result.TaskRevision, CriteriaVersion: result.CriteriaVersion, ContextHash: result.ContextHash, Attribution: attribution, Actor: input.Mutation.Actor, Reason: input.Mutation.Reason, CreatedAt: now,
			Definition: domain.TaskEvaluationDefinition{SchemaVersion: 1, TargetCommit: result.Definition.ClaimedCommit, CriteriaHash: criteria.ContentHash, ResultHash: result.ContentHash, Checks: checks, ChecksTruncated: truncated, Criteria: decisions, Outcome: outcome, Reason: reason}}
		if err := evaluation.Definition.Validate(); err != nil {
			return err
		}
		evaluation.ContentHash = evaluation.Hash()
		snapshot, err := json.Marshal(evaluation)
		if err != nil {
			return err
		}
		if len(snapshot) > 512<<10 {
			return ports.ErrTaskInvalid
		}
		if err := q.InsertTaskEvaluation(ctx, gen.InsertTaskEvaluationParams{ID: evaluation.ID, ProjectID: string(evaluation.ProjectID), TaskID: evaluation.TaskID, AttemptID: evaluation.AttemptID, ResultID: evaluation.ResultID, Number: evaluation.Number, TaskRevision: evaluation.TaskRevision, CriteriaVersion: evaluation.CriteriaVersion, IdempotencyKey: input.IdempotencyKey, RequestHash: requestHash, Snapshot: string(snapshot), ContentHash: evaluation.ContentHash, CreatedAt: now}); err != nil {
			return err
		}
		if err := insertTaskAudit(ctx, q, evaluation.TaskID, evaluation.TaskRevision, "evaluation_recorded", input.Mutation, now); err != nil {
			return err
		}
		created = true
		return nil
	})
	return evaluation, created && err == nil, err
}

func evaluationWorkerConfiguration(ctx context.Context, q *gen.Queries, result domain.TaskResult) (domain.WorkerConfiguration, error) {
	var configuration domain.WorkerConfiguration
	var executionID sql.NullString
	if result.ConfigurationSequence > 0 {
		activation, err := q.GetWorkerActivation(ctx, gen.GetWorkerActivationParams{SessionID: string(result.SessionID), Seq: result.ConfigurationSequence})
		if err != nil {
			return configuration, err
		}
		executionID = activation.ExecutionID
	}
	if executionID.Valid {
		row, err := q.GetWorkerExecution(ctx, gen.GetWorkerExecutionParams{ID: executionID.String, SessionID: string(result.SessionID)})
		if err != nil {
			return configuration, err
		}
		execution, err := workerExecutionFromRow(row)
		if err != nil {
			return configuration, err
		}
		configuration = execution.Configuration
	} else {
		row, err := q.GetWorkerConfiguration(ctx, string(result.SessionID))
		if err != nil {
			return configuration, err
		}
		var errDecode error
		configuration, errDecode = workerConfigurationFromRow(row)
		if errDecode != nil {
			return configuration, errDecode
		}
	}
	if configuration.ContentHash != result.ConfigurationHash {
		return configuration, ports.ErrTaskConflict
	}
	return configuration, nil
}

func collectEvaluationChecks(ctx context.Context, q *gen.Queries, result domain.TaskResult, criteria domain.AcceptanceCriteria) ([]domain.TaskCICheckEvidence, bool, error) {
	names := []string{}
	for _, criterion := range criteria.Criteria {
		names = append(names, criterion.CheckNames...)
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return nil, false, err
	}
	rows, err := q.CollectTaskCIChecks(ctx, gen.CollectTaskCIChecksParams{SessionID: result.SessionID, CheckNames: string(encoded), TargetCommit: result.Definition.ClaimedCommit})
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > 128
	checks := make([]domain.TaskCICheckEvidence, 0, min(len(rows), 128))
	for i, row := range rows {
		if i == 128 {
			break
		}
		checks = append(checks, domain.TaskCICheckEvidence{PRURL: row.PRURL, URL: row.URL, HeadCommit: row.HeadSha, Name: row.Name, TargetCommit: row.CommitHash, Status: row.Status, Conclusion: row.Conclusion, ObservedAt: row.ObservedAt.Time, SnapshotAt: row.CIObservedAt.Time})
	}
	return checks, truncated, nil
}

// GetTaskEvaluation reads immutable evidence without substituting today's facts.
func (s *Store) GetTaskEvaluation(ctx context.Context, id string) (domain.TaskEvaluation, error) {
	row, err := s.qr.GetTaskEvaluation(ctx, id)
	if err != nil {
		return domain.TaskEvaluation{}, taskReadError(err)
	}
	return taskEvaluationFromRow(row)
}

// LatestTaskEvaluation distinguishes no assessment from a failed database read.
func (s *Store) LatestTaskEvaluation(ctx context.Context, attemptID string) (domain.TaskEvaluation, bool, error) {
	row, err := s.qr.LatestTaskEvaluation(ctx, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskEvaluation{}, false, nil
	}
	if err != nil {
		return domain.TaskEvaluation{}, false, err
	}
	evaluation, err := taskEvaluationFromRow(row)
	return evaluation, err == nil, err
}

// ListTaskEvaluations pages assessment history without inflating attempt counts.
func (s *Store) ListTaskEvaluations(ctx context.Context, attemptID string, after int64, limit int) ([]domain.TaskEvaluation, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListTaskEvaluations(ctx, gen.ListTaskEvaluationsParams{AttemptID: attemptID, Number: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]domain.TaskEvaluation, 0, len(rows))
	for _, row := range rows {
		item, err := taskEvaluationFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
