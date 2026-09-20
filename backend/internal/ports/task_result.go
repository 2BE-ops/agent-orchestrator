package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TaskResultStore preserves claims independently of native output and evaluation.
// Submission never releases the task lease, changes criteria or marks completion.
type TaskResultStore interface {
	SubmitTaskResult(context.Context, domain.TaskResultSubmission) (domain.TaskResult, bool, error)
	GetTaskResult(context.Context, string) (domain.TaskResult, error)
	ListTaskResults(context.Context, string, int64, int) ([]domain.TaskResult, error)
	LatestTaskResult(context.Context, string) (domain.TaskResult, bool, error)
}
