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

// AgentManagerInboxService is routing intent and history, independent of launch.
type AgentManagerInboxService interface {
	Enqueue(context.Context, domain.AdaptiveActor, domain.ProjectID, managersvc.EnqueueInput) (domain.AgentManagerRequest, error)
	Request(context.Context, domain.ProjectID, string) (domain.AgentManagerRequest, error)
	Requests(context.Context, domain.ProjectID, int64, int, bool) ([]domain.AgentManagerRequest, error)
	Resolve(context.Context, domain.AdaptiveActor, domain.ProjectID, string, managersvc.ResolveInput) (domain.AgentManagerRequestResolution, error)
	Resolution(context.Context, domain.ProjectID, string) (*domain.AgentManagerRequestResolution, error)
}

func (c *AgentManagersController) inboxService(w http.ResponseWriter, r *http.Request) (AgentManagerInboxService, bool) {
	svc, ok := c.Svc.(AgentManagerInboxService)
	if !ok {
		envelope.WriteError(w, r, apierr.NotImplemented("AGENT_MANAGER_INBOX_UNAVAILABLE", "Manager inbox service is unavailable"))
	}
	return svc, ok
}

func (c *AgentManagersController) enqueue(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.inboxService(w, r)
	if !ok {
		return
	}
	var input AgentManagerEnqueueRequest
	if !decodeTaskOutput(w, r, &input, 16<<10, "INVALID_AGENT_MANAGER_JSON", "manager-request") {
		return
	}
	request, err := svc.Enqueue(r.Context(), taskHumanActor(), domain.ProjectID(chi.URLParam(r, "id")), managersvc.EnqueueInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, request)
}

func (c *AgentManagersController) request(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.inboxService(w, r)
	if !ok {
		return
	}
	request, err := svc.Request(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, request)
}

func (c *AgentManagersController) requests(w http.ResponseWriter, r *http.Request) {
	c.listRequests(w, r, false)
}
func (c *AgentManagersController) inbox(w http.ResponseWriter, r *http.Request) {
	c.listRequests(w, r, true)
}

func (c *AgentManagersController) listRequests(w http.ResponseWriter, r *http.Request, pendingOnly bool) {
	svc, ok := c.inboxService(w, r)
	if !ok {
		return
	}
	after, limit, err := managerPage(r)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := svc.Requests(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), after, limit, pendingOnly)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := AgentManagerRequestsResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Sequence, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AgentManagersController) resolveRequest(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.inboxService(w, r)
	if !ok {
		return
	}
	var input AgentManagerResolveRequest
	if !decodeTaskOutput(w, r, &input, 16<<10, "INVALID_AGENT_MANAGER_JSON", "manager-resolution") {
		return
	}
	resolution, err := svc.Resolve(r.Context(), taskHumanActor(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"), managersvc.ResolveInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, resolution)
}

func (c *AgentManagersController) requestResolution(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.inboxService(w, r)
	if !ok {
		return
	}
	resolution, err := svc.Resolution(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerResolutionResponse{Resolution: resolution})
}
