package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TaskMessageStore owns immutable coordination and exclusive delivery journals.
// It never performs native I/O or changes task completion/acceptance facts.
type TaskMessageStore interface {
	SubmitTaskMessage(context.Context, domain.TaskMessageSubmission) (domain.TaskMessage, bool, error)
	GetTaskMessage(context.Context, string) (domain.TaskMessage, error)
	ListTaskMessages(context.Context, domain.ProjectID, string, int64, int) ([]domain.TaskMessage, error)
	ListPendingTaskMessages(context.Context, int64, int) ([]domain.TaskMessage, error)
	BeginTaskMessageDelivery(context.Context, string, string) (domain.TaskMessageDelivery, bool, error)
	ResolveTaskMessageDelivery(context.Context, domain.TaskMessageDeliveryResolution) error
	ListTaskMessageDeliveries(context.Context, string) ([]domain.TaskMessageDelivery, error)
	ListUnresolvedTaskMessageDeliveries(context.Context, string, int) ([]domain.TaskMessageDelivery, error)
}
