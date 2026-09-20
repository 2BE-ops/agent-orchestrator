package controllers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
)

// AgentManagerAttributionService exposes the Manager's routing outcomes as a
// decision-maker surface distinct from worker performance.
type AgentManagerAttributionService interface {
	RoutingOutcomes(ctx context.Context, project domain.ProjectID, afterSequence int64, limit int) ([]domain.ManagerRoutingOutcome, error)
	RoutingSummary(ctx context.Context, project domain.ProjectID, query domain.OutcomeAttributionQuery) (domain.ManagerRoutingSummary, error)
}

func (c *AgentManagersController) attributionService(w http.ResponseWriter, r *http.Request) (AgentManagerAttributionService, bool) {
	svc, ok := c.Svc.(AgentManagerAttributionService)
	if !ok {
		envelope.WriteError(w, r, apierr.NotImplemented("AGENT_MANAGER_ATTRIBUTION_UNAVAILABLE", "Manager routing attribution is unavailable"))
	}
	return svc, ok
}

func (c *AgentManagersController) routingOutcomes(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.attributionService(w, r)
	if !ok {
		return
	}
	invalid := apierr.Invalid("INVALID_ROUTING_PAGE", "Use one non-negative after sequence and a limit from 1 to 100", nil)
	query := r.URL.Query()
	for key, values := range query {
		if (key != "after" && key != "limit") || len(values) != 1 {
			envelope.WriteError(w, r, invalid)
			return
		}
	}
	var after int64
	if raw := query.Get("after"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			envelope.WriteError(w, r, invalid)
			return
		}
		after = value
	}
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			envelope.WriteError(w, r, invalid)
			return
		}
		limit = value
	}
	items, err := svc.RoutingOutcomes(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := ManagerRoutingOutcomesResponse{Items: items}
	if len(items) == limit {
		response.NextAfter = items[len(items)-1].Sequence
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AgentManagersController) routingSummary(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.attributionService(w, r)
	if !ok {
		return
	}
	from, to, ok := parseAttributionWindow(w, r)
	if !ok {
		return
	}
	summary, err := svc.RoutingSummary(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), domain.OutcomeAttributionQuery{ProjectID: domain.ProjectID(chi.URLParam(r, "id")), From: from, To: to})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ManagerRoutingSummaryResponse{Summary: summary})
}
