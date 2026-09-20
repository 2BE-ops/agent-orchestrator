package controllers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
)

// AgentManagerDecisionService exposes recorded checks without a public verdict write.
type AgentManagerDecisionService interface {
	Decisions(context.Context, domain.ProjectID, string) ([]domain.AgentManagerDecision, error)
	Decision(context.Context, domain.ProjectID, string, string) (*domain.AgentManagerDecision, error)
}

func (c *AgentManagersController) decisionService(w http.ResponseWriter, r *http.Request) (AgentManagerDecisionService, bool) {
	svc, ok := c.Svc.(AgentManagerDecisionService)
	if !ok {
		envelope.WriteError(w, r, apierr.NotImplemented("AGENT_MANAGER_DECISIONS_UNAVAILABLE", "Manager decision history is unavailable"))
	}
	return svc, ok
}

func (c *AgentManagersController) decisions(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.decisionService(w, r)
	if !ok {
		return
	}
	items, err := svc.Decisions(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerDecisionsResponse{Items: items})
}

func (c *AgentManagersController) decision(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.decisionService(w, r)
	if !ok {
		return
	}
	item, err := svc.Decision(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"), chi.URLParam(r, "proposalId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerDecisionResponse{Decision: item})
}
