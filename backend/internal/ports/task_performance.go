package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TaskPerformanceStore derives bounded history from existing immutable attempt,
// result, configuration and assessment records plus certified usage facts.
type TaskPerformanceStore interface {
	ListTaskPerformance(context.Context, domain.TaskPerformanceQuery) (domain.TaskPerformancePage, error)
}
