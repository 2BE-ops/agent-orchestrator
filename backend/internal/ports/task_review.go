package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TaskReviewStore binds existing review passes to exact adaptive task evidence.
type TaskReviewStore interface {
	PrepareTaskReview(context.Context, string, domain.AdaptiveActor) (domain.TaskReviewPreparation, error)
	InsertTaskReviewRun(context.Context, domain.ReviewRun, domain.TaskReviewContext) error
	GetTaskReviewContext(context.Context, string) (domain.TaskReviewSnapshot, bool, error)
	MarkTaskReviewStarted(context.Context, string, string) (bool, error)
}
