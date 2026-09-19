package task

import (
	"context"
	"errors"
	"strconv"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func delegationError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrTaskNotFound):
		return apierr.NotFound("TASK_DELEGATION_NOT_FOUND", "Task attempt or delegation was not found")
	case errors.Is(err, ports.ErrTaskInvalid):
		return apierr.Invalid("INVALID_TASK_DELEGATION", err.Error(), nil)
	default:
		return err
	}
}

// Delegations returns the attempt's immutable sealed input receipts. A
// delegation proves what exact frozen context a native execution received;
// it never implies a send, activation or completion.
func (m *Manager) Delegations(ctx context.Context, taskID, attemptID string, after int64, limit int) ([]domain.TaskDelegation, error) {
	if err := validatePage(after, limit); err != nil {
		return nil, err
	}
	attempt, err := m.store.GetTaskAttempt(ctx, attemptID)
	if err != nil {
		return nil, delegationError(err)
	}
	if attempt.TaskID != taskID {
		return nil, delegationError(ports.ErrTaskNotFound)
	}
	items, err := m.store.ListTaskDelegations(ctx, attemptID, after, limit)
	return items, delegationError(err)
}

// Delegation returns one exact sealed input receipt within task/attempt scope.
func (m *Manager) Delegation(ctx context.Context, taskID, attemptID string, number int64) (domain.TaskDelegation, error) {
	if number < 1 || number > 1000 {
		return domain.TaskDelegation{}, apierr.Invalid("INVALID_TASK_DELEGATION", "Delegation number must be 1 to 1000", nil)
	}
	attempt, err := m.store.GetTaskAttempt(ctx, attemptID)
	if err != nil {
		return domain.TaskDelegation{}, delegationError(err)
	}
	if attempt.TaskID != taskID {
		return domain.TaskDelegation{}, delegationError(ports.ErrTaskNotFound)
	}
	item, err := m.store.GetTaskDelegation(ctx, attemptID, number)
	if err != nil {
		return domain.TaskDelegation{}, delegationError(err)
	}
	if item.AttemptID != attemptID {
		return domain.TaskDelegation{}, delegationError(ports.ErrTaskNotFound)
	}
	return item, nil
}

// ParseTaskDelegationNumber bounds the exact delegation route parameter.
func ParseTaskDelegationNumber(raw string) (int64, error) {
	number, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || number < 1 || number > 1000 {
		return 0, apierr.Invalid("INVALID_TASK_DELEGATION", "Delegation number must be 1 to 1000", nil)
	}
	return number, nil
}
