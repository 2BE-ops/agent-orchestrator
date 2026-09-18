package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TaskContextStore seals provenance under the same exclusive launch reservation.
type TaskContextStore interface {
	SaveTaskContext(context.Context, domain.TaskLeaseToken, domain.TaskContextSnapshot) error
	GetTaskContext(context.Context, string) (domain.TaskContextSnapshot, bool, error)
	GetTaskContextBySession(context.Context, domain.SessionID) (domain.TaskContextSnapshot, bool, error)
}

// TaskContextRequest is internal manager context, never public file-path input.
type TaskContextRequest struct {
	Lease                domain.TaskLeaseToken
	SessionID            domain.SessionID
	ExecutionOperationID string
	WorkspacePath        string
	Prompt               string
	SystemPrompt         string
	Budget               domain.ContextBudget
}

// TaskContextBuilder freezes or replays bounded context before worker execution.
type TaskContextBuilder interface {
	Build(context.Context, TaskContextRequest) (domain.TaskContextSnapshot, error)
}
