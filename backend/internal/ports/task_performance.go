package ports

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TaskPerformanceStore derives bounded history from existing immutable attempt,
// result, configuration and assessment records plus certified usage facts.
type TaskPerformanceStore interface {
	ListTaskPerformance(context.Context, domain.TaskPerformanceQuery) (domain.TaskPerformancePage, error)
	GetTaskPerformanceSummary(context.Context, domain.TaskPerformanceSummaryQuery) (domain.TaskPerformanceSummary, error)
}

// ErrTaskPerformanceWindowTooLarge requires a narrower complete summary cohort.
var ErrTaskPerformanceWindowTooLarge = errors.New("performance window exceeds the attempt limit")
