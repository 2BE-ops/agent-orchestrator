package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TaskEvaluationStore collects and retains independent evidence atomically with
// attribution and audit. Callers cannot supply an outcome or substitute evidence.
type TaskEvaluationStore interface {
	EvaluateTaskResult(context.Context, domain.TaskEvaluationRequest) (domain.TaskEvaluation, bool, error)
	GetTaskEvaluation(context.Context, string) (domain.TaskEvaluation, error)
	LatestTaskEvaluation(context.Context, string) (domain.TaskEvaluation, bool, error)
	ListTaskEvaluations(context.Context, string, int64, int) ([]domain.TaskEvaluation, error)
}
