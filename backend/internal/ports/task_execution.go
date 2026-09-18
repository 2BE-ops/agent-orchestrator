package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TaskExecutionStore closes the native-launch versus lease-release race.
// A repeated Begin returns created=false and is not permission to launch again.
type TaskExecutionStore interface {
	BeginTaskExecution(context.Context, domain.TaskExecutionOperation) (bool, error)
	PendingTaskExecution(context.Context, domain.SessionID) (domain.TaskExecutionOperation, bool, error)
	ResolveTaskExecution(context.Context, domain.TaskExecutionResolution) error
}
