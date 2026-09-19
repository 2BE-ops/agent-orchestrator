package agentmanager

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// RoutingOutcomes pages sealed routing decisions coupled with the read-time
// fate of each routed task. The cursor is the request's durable arrival
// sequence; the page is bounded 1 to 100.
func (m *Manager) RoutingOutcomes(ctx context.Context, project domain.ProjectID, afterSequence int64, limit int) ([]domain.ManagerRoutingOutcome, error) {
	store, ok := m.store.(ports.ManagerRoutingOutcomeStore)
	if !ok {
		return nil, apierr.NotImplemented("AGENT_MANAGER_ATTRIBUTION_UNAVAILABLE", "Manager routing attribution is unavailable")
	}
	if limit < 1 || limit > 100 || afterSequence < 0 {
		return nil, apierr.Invalid("INVALID_ROUTING_PAGE", "Routing outcome cursor must be non-negative and limit must be 1 to 100", nil)
	}
	items, err := store.ListManagerRoutingOutcomes(ctx, project, afterSequence, limit)
	return items, mapError(err)
}

// RoutingSummary aggregates one complete bounded routing-decision cohort.
// Worker performance stays a separate surface; this reads the decision maker.
func (m *Manager) RoutingSummary(ctx context.Context, project domain.ProjectID, query domain.OutcomeAttributionQuery) (domain.ManagerRoutingSummary, error) {
	store, ok := m.store.(ports.ManagerRoutingOutcomeStore)
	if !ok {
		return domain.ManagerRoutingSummary{}, apierr.NotImplemented("AGENT_MANAGER_ATTRIBUTION_UNAVAILABLE", "Manager routing attribution is unavailable")
	}
	if err := query.Validate(); err != nil {
		return domain.ManagerRoutingSummary{}, apierr.Invalid("INVALID_ATTRIBUTION_WINDOW", "Use a project, from before to and a window up to 366 days", nil)
	}
	summary, err := store.ManagerRoutingSummary(ctx, query)
	if errors.Is(err, ports.ErrOutcomeCohortTooLarge) {
		return domain.ManagerRoutingSummary{}, apierr.Invalid("ATTRIBUTION_WINDOW_TOO_LARGE", "Narrow the decision window to at most 1000 routed requests; no partial summary was returned", nil)
	}
	return summary, mapError(err)
}
