package controllers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	managersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agentmanager"
)

// AgentManagerRegistryService derives native authoring context without actor tags.
type AgentManagerRegistryService interface {
	AuthorRegistryAction(context.Context, domain.SessionID, string, managersvc.RegistryActionInput) (managersvc.RegistryActionReceipt, error)
	RegistryReceipts(context.Context, domain.ProjectID, string, string, int) ([]domain.AgentManagerRegistryReceipt, error)
	RegistryReceipt(context.Context, domain.ProjectID, string, string) (domain.AgentManagerRegistryReceipt, error)
}

func (c *AgentManagersController) registryService(w http.ResponseWriter, r *http.Request) (AgentManagerRegistryService, bool) {
	svc, ok := c.Svc.(AgentManagerRegistryService)
	if !ok {
		envelope.WriteError(w, r, apierr.NotImplemented("AGENT_MANAGER_REGISTRY_UNAVAILABLE", "Manager registry authoring service is unavailable"))
	}
	return svc, ok
}

func (c *AgentManagersController) registryAuthor(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.registryService(w, r)
	if !ok {
		return
	}
	var input AgentManagerRegistryRequest
	if !decodeTaskOutput(w, r, &input, 288<<10, "INVALID_AGENT_MANAGER_JSON", "manager-registry-envelope") {
		return
	}
	receipt, err := svc.AuthorRegistryAction(r.Context(), sessionID(r), chi.URLParam(r, "requestId"), managersvc.RegistryActionInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerRegistryResponse(receipt))
}

func (c *AgentManagersController) registryReceipts(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.registryService(w, r)
	if !ok {
		return
	}
	invalid := apierr.Invalid("INVALID_AGENT_MANAGER_PAGE", "Use one bounded afterId and a limit from 1 to 100", nil)
	query := r.URL.Query()
	for key, values := range query {
		if (key != "afterId" && key != "limit") || len(values) != 1 || values[0] == "" {
			envelope.WriteError(w, r, invalid)
			return
		}
	}
	afterID := query.Get("afterId")
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			envelope.WriteError(w, r, invalid)
			return
		}
	}
	items, err := svc.RegistryReceipts(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"), afterID, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := AgentManagerRegistryReceiptsResponse{Items: items}
	if len(items) == limit {
		response.NextAfterID = items[len(items)-1].ID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AgentManagersController) registryReceipt(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.registryService(w, r)
	if !ok {
		return
	}
	item, err := svc.RegistryReceipt(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"), chi.URLParam(r, "receiptId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, item)
}
