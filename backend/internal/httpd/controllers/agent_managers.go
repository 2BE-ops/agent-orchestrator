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

// AgentManagerService is the shared governance boundary; actors are not payloads.
type AgentManagerService interface {
	Configure(context.Context, domain.AdaptiveActor, domain.ProjectID, managersvc.ConfigureInput) (domain.AgentManagerConfiguration, error)
	Get(context.Context, domain.ProjectID) (domain.AgentManagerConfiguration, error)
	Configuration(context.Context, domain.ProjectID, int64) (domain.AgentManagerConfiguration, error)
	Configurations(context.Context, domain.ProjectID, int64, int) ([]domain.AgentManagerConfiguration, error)
	Audit(context.Context, domain.ProjectID, int64, int) ([]domain.AgentManagerAudit, error)
}

// AgentManagersController exposes user configuration and retained history.
type AgentManagersController struct{ Svc AgentManagerService }

// Register mounts the project-owned governance surface without generic spawn.
func (c *AgentManagersController) Register(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(c.available)
		r.Get("/projects/{id}/agent-manager", c.get)
		r.Put("/projects/{id}/agent-manager", c.configure)
		r.Get("/projects/{id}/agent-manager/configurations", c.configurations)
		r.Get("/projects/{id}/agent-manager/configurations/{version}", c.configuration)
		r.Get("/projects/{id}/agent-manager/audit", c.audit)
		r.Get("/projects/{id}/agent-manager/inbox", c.inbox)
		r.Get("/projects/{id}/agent-manager/requests", c.requests)
		r.Post("/projects/{id}/agent-manager/requests", c.enqueue)
		r.Get("/projects/{id}/agent-manager/requests/{requestId}", c.request)
		r.Get("/projects/{id}/agent-manager/requests/{requestId}/resolution", c.requestResolution)
		r.Post("/projects/{id}/agent-manager/requests/{requestId}/resolution", c.resolveRequest)
		r.Get("/projects/{id}/agent-manager/requests/{requestId}/proposals", c.proposals)
		r.Get("/projects/{id}/agent-manager/requests/{requestId}/proposals/{proposalId}", c.proposal)
		r.Post("/sessions/{sessionId}/agent-manager/requests/{requestId}/proposals", c.propose)
	})
}

func (c *AgentManagersController) available(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.Svc == nil {
			envelope.WriteError(w, r, apierr.NotImplemented("AGENT_MANAGER_UNAVAILABLE", "Agent Manager service is unavailable"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (c *AgentManagersController) configure(w http.ResponseWriter, r *http.Request) {
	var input AgentManagerConfigureRequest
	if !decodeTaskOutput(w, r, &input, 64<<10, "INVALID_AGENT_MANAGER_JSON", "manager-configuration") {
		return
	}
	configuration, err := c.Svc.Configure(r.Context(), taskHumanActor(), domain.ProjectID(chi.URLParam(r, "id")), managersvc.ConfigureInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, configuration)
}

func (c *AgentManagersController) get(w http.ResponseWriter, r *http.Request) {
	configuration, err := c.Svc.Get(r.Context(), domain.ProjectID(chi.URLParam(r, "id")))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, configuration)
}

func (c *AgentManagersController) configuration(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.ParseInt(chi.URLParam(r, "version"), 10, 64)
	if err != nil {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_AGENT_MANAGER", "Configuration version must be 1 to 1000", nil))
		return
	}
	configuration, err := c.Svc.Configuration(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), number)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, configuration)
}

func managerPage(r *http.Request) (int64, int, error) {
	query := r.URL.Query()
	invalid := apierr.Invalid("INVALID_AGENT_MANAGER_PAGE", "Use one non-negative cursor and a limit from 1 to 100", nil)
	for key, values := range query {
		if (key != "cursor" && key != "limit") || len(values) != 1 || values[0] == "" {
			return 0, 0, invalid
		}
	}
	var after int64
	limit := 20
	var err error
	if raw := query.Get("cursor"); raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			return 0, 0, invalid
		}
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, invalid
		}
	}
	return after, limit, nil
}

func (c *AgentManagersController) configurations(w http.ResponseWriter, r *http.Request) {
	after, limit, err := managerPage(r)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Configurations(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := AgentManagerConfigurationsResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Number, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AgentManagersController) audit(w http.ResponseWriter, r *http.Request) {
	after, limit, err := managerPage(r)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Audit(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := AgentManagerAuditResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Sequence, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}
