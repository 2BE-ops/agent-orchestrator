package task

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// EvaluationInput requests collection of stored independent facts. Callers
// cannot supply criteria, author identity, evidence or a verdict.
type EvaluationInput struct {
	ResultID        string `json:"resultId"`
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
	Reason          string `json:"reason"`
}

// EvaluationReceipt distinguishes a new observation from an exact retry.
type EvaluationReceipt struct {
	Evaluation domain.TaskEvaluation `json:"evaluation"`
	Created    bool                  `json:"created"`
}

func evaluationError(err error) error {
	switch {
	case errors.Is(err, ports.ErrTaskConflict):
		return apierr.Conflict("TASK_EVALUATION_CONFLICT", "Result, evaluation version or retry content changed", nil)
	case errors.Is(err, ports.ErrTaskForbidden):
		return apierr.Forbidden("TASK_EVALUATION_FORBIDDEN", "This actor cannot request task evaluation")
	case errors.Is(err, ports.ErrTaskInvalid):
		return apierr.Invalid("INVALID_TASK_EVALUATION", err.Error(), nil)
	case errors.Is(err, ports.ErrTaskNotFound):
		return apierr.NotFound("TASK_EVALUATION_NOT_FOUND", "Task, attempt, result or evaluation was not found")
	default:
		return err
	}
}

// Evaluate collects stored facts for one exact result after checking URL scope.
// It neither starts native work nor changes the task's acceptance criteria.
func (m *Manager) Evaluate(ctx context.Context, actor domain.AdaptiveActor, taskID, attemptID string, input EvaluationInput) (EvaluationReceipt, error) {
	var receipt EvaluationReceipt
	for _, value := range []string{input.ResultID, input.IdempotencyKey} {
		if strings.TrimSpace(value) == "" || len(value) > 200 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return receipt, apierr.Invalid("INVALID_TASK_EVALUATION", "Bounded resultId and idempotencyKey are required", nil)
		}
	}
	if input.ExpectedVersion < 0 || input.ExpectedVersion >= 64 || strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 2000 {
		return receipt, apierr.Invalid("INVALID_TASK_EVALUATION", "A reason and expectedVersion between 0 and 63 are required", nil)
	}
	result, err := m.store.GetTaskResult(ctx, input.ResultID)
	if err != nil {
		return receipt, evaluationError(err)
	}
	if result.TaskID != taskID || result.AttemptID != attemptID {
		return receipt, evaluationError(ports.ErrTaskNotFound)
	}
	receipt.Evaluation, receipt.Created, err = m.store.EvaluateTaskResult(ctx, domain.TaskEvaluationRequest{ID: uuid.NewString(), ResultID: input.ResultID, ExpectedVersion: input.ExpectedVersion, IdempotencyKey: input.IdempotencyKey, Mutation: domain.TaskMutation{Actor: actor, Reason: input.Reason}})
	return receipt, evaluationError(err)
}

// Evaluations pages immutable snapshots within the requested task and attempt.
func (m *Manager) Evaluations(ctx context.Context, taskID, attemptID string, after int64, limit int) ([]domain.TaskEvaluation, error) {
	if err := validatePage(after, limit); err != nil {
		return nil, err
	}
	attempt, err := m.store.GetTaskAttempt(ctx, attemptID)
	if err != nil {
		return nil, evaluationError(err)
	}
	if attempt.TaskID != taskID {
		return nil, evaluationError(ports.ErrTaskNotFound)
	}
	items, err := m.store.ListTaskEvaluations(ctx, attemptID, after, limit)
	return items, evaluationError(err)
}

// Evaluation reads historical evidence without replacing it with today's facts.
func (m *Manager) Evaluation(ctx context.Context, taskID, attemptID, id string) (domain.TaskEvaluation, error) {
	item, err := m.store.GetTaskEvaluation(ctx, id)
	if err != nil {
		return item, evaluationError(err)
	}
	if item.TaskID != taskID || item.AttemptID != attemptID {
		return domain.TaskEvaluation{}, evaluationError(ports.ErrTaskNotFound)
	}
	return item, nil
}
