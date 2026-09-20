package controllers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	managersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agentmanager"
)

// AgentManagerControllerService starts only under dedicated durable admission.
type AgentManagerControllerService interface {
	StartController(context.Context, domain.AdaptiveActor, domain.ProjectID, managersvc.ControllerStartInput) (managersvc.ControllerStartReceipt, error)
	Controller(context.Context, domain.ProjectID, string) (managersvc.ControllerState, error)
	CurrentController(context.Context, domain.ProjectID) (*managersvc.ControllerState, error)
}

func (c *AgentManagersController) controllerService(w http.ResponseWriter, r *http.Request) (AgentManagerControllerService, bool) {
	svc, ok := c.Svc.(AgentManagerControllerService)
	if !ok {
		envelope.WriteError(w, r, apierr.NotImplemented("AGENT_MANAGER_CONTROLLERS_UNAVAILABLE", "Manager controller service is unavailable"))
	}
	return svc, ok
}

func (c *AgentManagersController) startController(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.controllerService(w, r)
	if !ok {
		return
	}
	var input AgentManagerStartRequest
	if !decodeTaskOutput(w, r, &input, 16<<10, "INVALID_AGENT_MANAGER_JSON", "manager-start") {
		return
	}
	receipt, err := svc.StartController(r.Context(), taskHumanActor(), domain.ProjectID(chi.URLParam(r, "id")), managersvc.ControllerStartInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerStartResponse{State: AgentManagerControllerResponse(receipt.State), Created: receipt.Created})
}

func (c *AgentManagersController) currentController(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.controllerService(w, r)
	if !ok {
		return
	}
	state, err := svc.CurrentController(r.Context(), domain.ProjectID(chi.URLParam(r, "id")))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	var response AgentManagerCurrentControllerResponse
	if state != nil {
		converted := AgentManagerControllerResponse(*state)
		response.State = &converted
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AgentManagersController) controller(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.controllerService(w, r)
	if !ok {
		return
	}
	state, err := svc.Controller(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "controllerId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerControllerResponse(state))
}
