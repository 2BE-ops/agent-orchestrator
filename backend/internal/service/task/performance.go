package task

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Performance exposes attributable admission-cohort evidence to supervisors.
func (m *Manager) Performance(ctx context.Context, query domain.TaskPerformanceQuery) (domain.TaskPerformancePage, error) {
	if err := query.Validate(); err != nil {
		return domain.TaskPerformancePage{}, apierr.Invalid("INVALID_PERFORMANCE_QUERY", err.Error(), nil)
	}
	page, err := m.store.ListTaskPerformance(ctx, query)
	return page, mapError(err)
}

// PerformanceSummary keeps sample counts and refuses incomplete large cohorts.
func (m *Manager) PerformanceSummary(ctx context.Context, query domain.TaskPerformanceSummaryQuery) (domain.TaskPerformanceSummary, error) {
	if err := query.Validate(); err != nil {
		return domain.TaskPerformanceSummary{}, apierr.Invalid("INVALID_PERFORMANCE_QUERY", err.Error(), nil)
	}
	summary, err := m.store.GetTaskPerformanceSummary(ctx, query)
	if errors.Is(err, ports.ErrTaskPerformanceWindowTooLarge) {
		return domain.TaskPerformanceSummary{}, apierr.Invalid("PERFORMANCE_WINDOW_TOO_LARGE", "Narrow the admission window to at most 1000 attempts; no partial summary was returned", nil)
	}
	return summary, mapError(err)
}
