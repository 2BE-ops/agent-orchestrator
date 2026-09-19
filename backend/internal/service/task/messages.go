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

// MessageInput supplies coordination content and retry identity, not authority.
type MessageInput struct {
	SourceGeneration string                       `json:"sourceGeneration"`
	IdempotencyKey   string                       `json:"idempotencyKey"`
	Definition       domain.TaskMessageDefinition `json:"definition"`
}

// MessageReceipt acknowledges persistence before asynchronous native delivery.
type MessageReceipt struct {
	Message domain.TaskMessage `json:"message"`
	Created bool               `json:"created"`
}

// MessageView keeps immutable content separate from transport observations.
type MessageView struct {
	Message    domain.TaskMessage           `json:"message"`
	Deliveries []domain.TaskMessageDelivery `json:"deliveries"`
}

func messageError(err error) error {
	switch {
	case errors.Is(err, ports.ErrTaskConflict):
		return apierr.Conflict("TASK_MESSAGE_CONFLICT", "Retry content, configuration or message bounds conflict", nil)
	case errors.Is(err, ports.ErrTaskLeaseFenced):
		return apierr.Conflict("TASK_MESSAGE_OWNER_CHANGED", "The worker no longer has confirmed ownership for this message", nil)
	case errors.Is(err, ports.ErrTaskInvalid):
		return apierr.Invalid("INVALID_TASK_MESSAGE", err.Error(), nil)
	case errors.Is(err, ports.ErrTaskNotFound):
		return apierr.NotFound("TASK_MESSAGE_NOT_FOUND", "Message, task or referenced attempt was not found")
	default:
		return err
	}
}

// SubmitMessage derives ownership from the native session and persists first.
// It does not synchronously deliver, execute content or change task planning.
func (m *Manager) SubmitMessage(ctx context.Context, sessionID domain.SessionID, input MessageInput) (MessageReceipt, error) {
	var receipt MessageReceipt
	if strings.TrimSpace(input.SourceGeneration) == "" || len(input.SourceGeneration) > 200 {
		return receipt, apierr.Invalid("INVALID_MESSAGE_GENERATION", "A bounded sourceGeneration is required", nil)
	}
	submission := domain.TaskMessageSubmission{ID: uuid.NewString(), AttemptID: "pending-session-lookup", SessionID: sessionID, IdempotencyKey: input.IdempotencyKey, Definition: input.Definition}
	if err := submission.Validate(); err != nil {
		return receipt, apierr.Invalid("INVALID_TASK_MESSAGE", err.Error(), nil)
	}
	identity, err := m.workerSubmissionIdentity(ctx, sessionID, input.SourceGeneration)
	if err != nil {
		return receipt, messageError(err)
	}
	submission.AttemptID, submission.SourceOwner, submission.ExpectedActivation = identity.attemptID, identity.owner, identity.activation
	receipt.Message, receipt.Created, err = m.store.SubmitTaskMessage(ctx, submission)
	return receipt, messageError(err)
}

// Messages exposes the same project timeline to workers, orchestrator and UI.
func (m *Manager) Messages(ctx context.Context, projectID domain.ProjectID, taskID string, after int64, limit int) ([]domain.TaskMessage, error) {
	if err := validatePage(after, limit); err != nil {
		return nil, err
	}
	if err := m.project(ctx, projectID); err != nil {
		return nil, err
	}
	if taskID != "" {
		task, err := m.store.GetAdaptiveTask(ctx, taskID)
		if err != nil {
			return nil, messageError(err)
		}
		if task.ProjectID != projectID {
			return nil, messageError(ports.ErrTaskNotFound)
		}
	}
	items, err := m.store.ListTaskMessages(ctx, projectID, taskID, after, limit)
	return items, messageError(err)
}

// Message scopes content and its delivery history to the requested project.
func (m *Manager) Message(ctx context.Context, projectID domain.ProjectID, messageID string) (MessageView, error) {
	var view MessageView
	message, err := m.store.GetTaskMessage(ctx, messageID)
	if err != nil {
		return view, messageError(err)
	}
	if message.ProjectID != projectID {
		return view, messageError(ports.ErrTaskNotFound)
	}
	view.Message = message
	view.Deliveries, err = m.store.ListTaskMessageDeliveries(ctx, messageID)
	return view, messageError(err)
}
