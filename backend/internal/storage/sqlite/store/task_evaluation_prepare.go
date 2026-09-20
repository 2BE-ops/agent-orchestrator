package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func taskEvaluationRequestHash(input domain.TaskEvaluationRequest) (string, error) {
	for _, value := range []string{input.ID, input.ResultID, input.IdempotencyKey} {
		if strings.TrimSpace(value) == "" || len(value) > 200 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return "", ports.ErrTaskInvalid
		}
	}
	if input.ExpectedVersion < 0 || input.ExpectedVersion >= 64 {
		return "", ports.ErrTaskInvalid
	}
	if err := validateTaskMutation(input.Mutation); err != nil {
		return "", err
	}
	_, hash, err := domain.TaskContent(struct {
		ResultID        string
		ExpectedVersion int64
		Mutation        domain.TaskMutation
	}{input.ResultID, input.ExpectedVersion, input.Mutation})
	return hash, err
}

// PrepareTaskEvaluation authorizes read-only collection and acknowledges exact
// retries before filesystem/Git access. Final persistence repeats all fences.
func (s *Store) PrepareTaskEvaluation(ctx context.Context, input domain.TaskEvaluationRequest) (domain.TaskEvaluationPreparation, error) {
	var preparation domain.TaskEvaluationPreparation
	hash, err := taskEvaluationRequestHash(input)
	if err != nil {
		return preparation, err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return preparation, err
	}
	defer s.writeMu.Unlock()
	err = s.inTx(ctx, "prepare independent evaluation", func(q *gen.Queries) error {
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
			return err
		}
		if err := validateTaskActor(ctx, q, task.ProjectID, input.Mutation.Actor); err != nil {
			return err
		}
		previous, err := q.GetTaskEvaluationByKey(ctx, gen.GetTaskEvaluationByKeyParams{AttemptID: result.AttemptID, IdempotencyKey: input.IdempotencyKey})
		if err == nil {
			if previous.RequestHash != hash {
				return ports.ErrTaskConflict
			}
			evaluation, err := taskEvaluationFromRow(previous)
			preparation.Existing = &evaluation
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		latest, err := q.LatestTaskResult(ctx, result.AttemptID)
		if err != nil {
			return err
		}
		if latest.ID != result.ID {
			return ports.ErrTaskConflict
		}
		lastEvaluation, err := q.LatestTaskEvaluation(ctx, result.AttemptID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if lastEvaluation.Number != input.ExpectedVersion {
			return ports.ErrTaskConflict
		}
		criteriaRow, err := q.GetAdaptiveTaskCriteria(ctx, gen.GetAdaptiveTaskCriteriaParams{TaskID: result.TaskID, Number: result.CriteriaVersion})
		if err != nil {
			return err
		}
		criteria, err := taskCriteriaFromRow(criteriaRow)
		if err != nil {
			return err
		}
		worker, err := q.GetSession(ctx, result.SessionID)
		if err != nil {
			return err
		}
		preparation.Result, preparation.Criteria, preparation.WorkspacePath = result, criteria, worker.WorkspacePath
		return nil
	})
	return preparation, err
}

func validateCollectedArtifacts(items []domain.TaskArtifactEvidence, result domain.TaskResult, criteria domain.AcceptanceCriteria) error {
	if len(items) > 16 {
		return ports.ErrTaskInvalid
	}
	seen := map[string]bool{}
	for _, item := range items {
		if item.Validate() != nil || item.TargetCommit != result.Definition.ClaimedCommit || seen[item.CriterionID] {
			return ports.ErrTaskInvalid
		}
		seen[item.CriterionID] = true
		found := false
		for _, criterion := range criteria.Criteria {
			if criterion.ID == item.CriterionID && criterion.EvidenceKind == "artifact" && criterion.ArtifactPath == item.Path && criterion.ArtifactSHA256 != "" {
				found = true
				break
			}
		}
		if !found {
			return ports.ErrTaskInvalid
		}
	}
	return nil
}
