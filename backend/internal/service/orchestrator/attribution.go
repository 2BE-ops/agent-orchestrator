package orchestrator

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// PlanningOutcomes pages sealed planning receipts coupled with the read-time
// fate of the tasks they created or revised. The cursor matches the public
// receipt history; the page is bounded 1 to 100.
func (m *Manager) PlanningOutcomes(ctx context.Context, project domain.ProjectID, afterReceiptID string, limit int) ([]domain.OrchestratorPlanningOutcome, error) {
	if err := m.project(ctx, project); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, apierr.Invalid("INVALID_PLANNING_PAGE", "Planning outcome limit must be between 1 and 100", nil)
	}
	items, err := m.store.ListOrchestratorPlanningOutcomes(ctx, project, afterReceiptID, limit)
	return items, mapError(err)
}

// PlanningSummary aggregates one complete bounded planning cohort. Worker
// performance stays a separate surface; this reads the plan author.
func (m *Manager) PlanningSummary(ctx context.Context, project domain.ProjectID, query domain.OutcomeAttributionQuery) (domain.OrchestratorPlanningSummary, error) {
	if err := m.project(ctx, project); err != nil {
		return domain.OrchestratorPlanningSummary{}, err
	}
	if err := query.Validate(); err != nil {
		return domain.OrchestratorPlanningSummary{}, apierr.Invalid("INVALID_ATTRIBUTION_WINDOW", "Use a project, from before to and a window up to 366 days", nil)
	}
	summary, err := m.store.OrchestratorPlanningSummary(ctx, query)
	if errors.Is(err, ports.ErrOutcomeCohortTooLarge) {
		return domain.OrchestratorPlanningSummary{}, apierr.Invalid("ATTRIBUTION_WINDOW_TOO_LARGE", "Narrow the planning window to at most 1000 receipts; no partial summary was returned", nil)
	}
	return summary, mapError(err)
}
