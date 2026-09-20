package controllers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
)

// AgentManagerDeliveryService exposes historical input and transport facts only.
type AgentManagerDeliveryService interface {
	Contexts(context.Context, domain.ProjectID, string) ([]domain.AgentManagerContext, error)
	Context(context.Context, domain.ProjectID, string, string) (domain.AgentManagerContext, error)
	Deliveries(context.Context, domain.ProjectID, string) ([]domain.AgentManagerDelivery, error)
}

func (c *AgentManagersController) deliveryService(w http.ResponseWriter, r *http.Request) (AgentManagerDeliveryService, bool) {
	svc, ok := c.Svc.(AgentManagerDeliveryService)
	if !ok {
		envelope.WriteError(w, r, apierr.NotImplemented("AGENT_MANAGER_DELIVERY_UNAVAILABLE", "Manager delivery history is unavailable"))
	}
	return svc, ok
}

func (c *AgentManagersController) contexts(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.deliveryService(w, r)
	if !ok {
		return
	}
	items, err := svc.Contexts(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerContextsResponse{Items: items})
}

func (c *AgentManagersController) context(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.deliveryService(w, r)
	if !ok {
		return
	}
	item, err := svc.Context(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"), chi.URLParam(r, "contextId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, item)
}

func (c *AgentManagersController) deliveries(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.deliveryService(w, r)
	if !ok {
		return
	}
	items, err := svc.Deliveries(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerDeliveriesResponse{Items: items})
}
