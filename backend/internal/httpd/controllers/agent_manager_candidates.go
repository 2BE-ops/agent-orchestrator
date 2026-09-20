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

// AgentManagerCandidateService checks current compatibility without launching.
type AgentManagerCandidateService interface {
	Candidates(context.Context, domain.ProjectID, string, string, int) (managersvc.CandidatePage, error)
	Candidate(context.Context, domain.ProjectID, string, string, int64) (domain.AgentManagerCandidate, error)
}

func (c *AgentManagersController) candidateService(w http.ResponseWriter, r *http.Request) (AgentManagerCandidateService, bool) {
	svc, ok := c.Svc.(AgentManagerCandidateService)
	if !ok {
		envelope.WriteError(w, r, apierr.NotImplemented("AGENT_MANAGER_CANDIDATES_UNAVAILABLE", "Manager candidate assessment is unavailable"))
	}
	return svc, ok
}

func (c *AgentManagersController) candidates(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.candidateService(w, r)
	if !ok {
		return
	}
	limit := 20
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil {
			envelope.WriteError(w, r, apierr.Invalid("INVALID_MANAGER_CANDIDATE_PAGE", "Candidate limit must be 1 to 20", nil))
			return
		}
	}
	page, err := svc.Candidates(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerCandidatesResponse(page))
}

func (c *AgentManagersController) candidate(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.candidateService(w, r)
	if !ok {
		return
	}
	version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if err != nil || version < 1 {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_MANAGER_CANDIDATE", "An exact Type version is required", nil))
		return
	}
	item, err := svc.Candidate(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"), chi.URLParam(r, "agentTypeId"), version)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, item)
}
