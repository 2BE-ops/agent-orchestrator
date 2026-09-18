package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TaskIntentStore records audited, fenced admissions and cancellation intent.
type TaskIntentStore interface {
	ChangeTaskIntent(context.Context, string, domain.TaskIntentChange) (domain.TaskIntent, error)
	GetTaskIntent(context.Context, string) (domain.TaskIntent, error)
	ListTaskIntents(context.Context, string, int64, int) ([]domain.TaskIntent, error)
	CancelledTaskAncestor(context.Context, string) (string, error)
}
