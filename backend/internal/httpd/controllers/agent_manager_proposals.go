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

// AgentManagerProposalService derives native source context without author tags.
type AgentManagerProposalService interface {
	Propose(context.Context, domain.SessionID, string, managersvc.ProposalInput) (managersvc.ProposalReceipt, error)
	Proposals(context.Context, domain.ProjectID, string) ([]domain.AgentManagerProposal, error)
	Proposal(context.Context, domain.ProjectID, string, string) (domain.AgentManagerProposal, error)
}

func (c *AgentManagersController) proposalService(w http.ResponseWriter, r *http.Request) (AgentManagerProposalService, bool) {
	svc, ok := c.Svc.(AgentManagerProposalService)
	if !ok {
		envelope.WriteError(w, r, apierr.NotImplemented("AGENT_MANAGER_PROPOSALS_UNAVAILABLE", "Manager proposal service is unavailable"))
	}
	return svc, ok
}

func (c *AgentManagersController) propose(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.proposalService(w, r)
	if !ok {
		return
	}
	var input AgentManagerProposalRequest
	if !decodeTaskOutput(w, r, &input, 512<<10, "INVALID_AGENT_MANAGER_JSON", "manager-proposal-envelope") {
		return
	}
	receipt, err := svc.Propose(r.Context(), sessionID(r), chi.URLParam(r, "requestId"), managersvc.ProposalInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerProposalResponse(receipt))
}

func (c *AgentManagersController) proposals(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.proposalService(w, r)
	if !ok {
		return
	}
	if r.URL.RawQuery != "" {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_AGENT_MANAGER_PAGE", "Proposal history contains at most five entries and takes no query parameters", nil))
		return
	}
	items, err := svc.Proposals(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AgentManagerProposalsResponse{Items: items})
}

func (c *AgentManagersController) proposal(w http.ResponseWriter, r *http.Request) {
	svc, ok := c.proposalService(w, r)
	if !ok {
		return
	}
	item, err := svc.Proposal(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "requestId"), chi.URLParam(r, "proposalId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, item)
}
