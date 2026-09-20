package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TaskEvaluationStore collects and retains independent evidence atomically with
// attribution and audit. Collector input is trusted daemon context, never public
// request JSON. Final writes recheck the preparation's immutable result/version.
type TaskEvaluationStore interface {
	GetTaskCompletion(context.Context, string, int64) (domain.TaskCompletion, error)
	PrepareTaskEvaluation(context.Context, domain.TaskEvaluationRequest) (domain.TaskEvaluationPreparation, error)
	EvaluateTaskResult(context.Context, domain.TaskEvaluationRequest) (domain.TaskEvaluation, bool, error)
	GetTaskEvaluation(context.Context, string) (domain.TaskEvaluation, error)
	LatestTaskEvaluation(context.Context, string) (domain.TaskEvaluation, bool, error)
	ListTaskEvaluations(context.Context, string, int64, int) ([]domain.TaskEvaluation, error)
}

// TaskArtifactCollector reads committed artifacts without executing project code.
type TaskArtifactCollector interface {
	Collect(context.Context, domain.TaskEvaluationPreparation) ([]domain.TaskArtifactEvidence, error)
}
