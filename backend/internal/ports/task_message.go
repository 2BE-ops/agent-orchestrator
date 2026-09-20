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
	TaskMessageDispatchCursor(context.Context) (int64, error)
	SetTaskMessageDispatchCursor(context.Context, int64) error
}

// TaskMessageTransportResult distinguishes proven no-write from an ambiguous
// transport failure. Only not_sent can authorize another automatic attempt.
type TaskMessageTransportResult struct {
	State  string
	Reason string
}

// TaskMessageTransport uses existing native controllers and terminal guards.
// Readiness is advisory; delivery must recheck the reserved native owner.
type TaskMessageTransport interface {
	TaskMessageTargetReady(context.Context, domain.SessionID) (bool, error)
	DeliverTaskMessage(context.Context, domain.TaskMessageDelivery, domain.TaskMessage) TaskMessageTransportResult
}
