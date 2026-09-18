package task

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ResultInput contains claims and retry identity, never claimed actor authority.
type ResultInput struct {
	SourceGeneration string                      `json:"sourceGeneration"`
	IdempotencyKey   string                      `json:"idempotencyKey"`
	ExpectedVersion  int64                       `json:"expectedVersion"`
	Definition       domain.TaskResultDefinition `json:"definition"`
}

// ResultReceipt acknowledges durable claims; it is not an evaluation outcome.
type ResultReceipt struct {
	Result  domain.TaskResult `json:"result"`
	Created bool              `json:"created"`
}

func resultError(err error) error {
	switch {
	case errors.Is(err, ports.ErrTaskConflict):
		return apierr.Conflict("TASK_RESULT_CONFLICT", "Result version, retry content or execution configuration changed", nil)
	case errors.Is(err, ports.ErrTaskLeaseFenced):
		return apierr.Conflict("TASK_RESULT_OWNER_CHANGED", "The worker no longer has confirmed ownership for this submission", nil)
	case errors.Is(err, ports.ErrTaskInvalid):
		return apierr.Invalid("INVALID_TASK_RESULT", err.Error(), nil)
	case errors.Is(err, ports.ErrTaskNotFound):
		return apierr.NotFound("TASK_RESULT_NOT_FOUND", "Task attempt or result was not found")
	default:
		return err
	}
}

// SubmitResult derives attempt and controller attribution from the selected
// session. The generation is supplied by the submitting native execution, like
// AO's existing handoff protocol; it cannot be replaced by claimed author tags.
func (m *Manager) SubmitResult(ctx context.Context, sessionID domain.SessionID, input ResultInput) (ResultReceipt, error) {
	var receipt ResultReceipt
	if strings.TrimSpace(input.SourceGeneration) == "" || len(input.SourceGeneration) > 200 {
		return receipt, apierr.Invalid("INVALID_RESULT_GENERATION", "A bounded sourceGeneration is required", nil)
	}
	submission := domain.TaskResultSubmission{ID: uuid.NewString(), AttemptID: "pending-session-lookup", SessionID: sessionID, ExpectedVersion: input.ExpectedVersion, IdempotencyKey: input.IdempotencyKey, Definition: input.Definition}
	if err := submission.Validate(); err != nil {
		return receipt, apierr.Invalid("INVALID_TASK_RESULT", err.Error(), nil)
	}
	rec, found, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return receipt, err
	}
	if !found {
		return receipt, resultError(ports.ErrTaskNotFound)
	}
	owner := rec.ControllerOwner()
	generation := owner.RuntimeLaunchID
	if owner.Mode == domain.SessionModeChat {
		generation = owner.ControllerGeneration
	}
	if rec.Kind != domain.KindWorker || generation != input.SourceGeneration {
		return receipt, resultError(ports.ErrTaskLeaseFenced)
	}
	dispatch, found, err := m.store.GetTaskWorkerDispatchBySession(ctx, sessionID)
	if err != nil {
		return receipt, err
	}
	if !found {
		return receipt, resultError(ports.ErrTaskNotFound)
	}
	_, sequence, found, err := m.store.GetEffectiveWorkerConfiguration(ctx, sessionID)
	if err != nil {
		return receipt, err
	}
	if !found {
		return receipt, resultError(ports.ErrTaskNotFound)
	}
	submission.AttemptID, submission.SourceOwner, submission.ExpectedActivation = dispatch.AttemptID, owner, sequence
	receipt.Result, receipt.Created, err = m.store.SubmitTaskResult(ctx, submission)
	return receipt, resultError(err)
}

// Results returns only the requested task's retained attempt claims.
func (m *Manager) Results(ctx context.Context, taskID, attemptID string, after int64, limit int) ([]domain.TaskResult, error) {
	if err := validatePage(after, limit); err != nil {
		return nil, err
	}
	attempt, err := m.store.GetTaskAttempt(ctx, attemptID)
	if err != nil {
		return nil, resultError(err)
	}
	if attempt.TaskID != taskID {
		return nil, resultError(ports.ErrTaskNotFound)
	}
	results, err := m.store.ListTaskResults(ctx, attemptID, after, limit)
	return results, resultError(err)
}

// Result returns a historical claim only within its task/attempt scope.
func (m *Manager) Result(ctx context.Context, taskID, attemptID, resultID string) (domain.TaskResult, error) {
	result, err := m.store.GetTaskResult(ctx, resultID)
	if err != nil {
		return result, resultError(err)
	}
	if result.TaskID != taskID || result.AttemptID != attemptID {
		return domain.TaskResult{}, resultError(ports.ErrTaskNotFound)
	}
	return result, nil
}
