package agentmanager

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type decisionWorkStore interface {
	ports.AgentManagerDecisionStore
	ports.AgentManagerInboxStore
	GetAdaptiveTask(context.Context, string) (domain.AdaptiveTask, error)
	CancelledTaskAncestor(context.Context, string) (string, error)
}

func (m *Manager) decisionStore() (decisionWorkStore, error) {
	store, ok := m.store.(decisionWorkStore)
	if !ok {
		return nil, apierr.NotImplemented("AGENT_MANAGER_DECISIONS_UNAVAILABLE", "Manager decision persistence is unavailable")
	}
	return store, nil
}

// Decision reads a retained assessment or nil while a proposal is unassessed.
// Historical access does not require current selection permission or governance.
func (m *Manager) Decision(ctx context.Context, project domain.ProjectID, requestID, proposalID string) (*domain.AgentManagerDecision, error) {
	if _, err := m.Proposal(ctx, project, requestID, proposalID); err != nil {
		return nil, err
	}
	store, err := m.decisionStore()
	if err != nil {
		return nil, err
	}
	decision, found, err := store.GetAgentManagerDecision(ctx, project, requestID, proposalID)
	if err != nil || !found {
		return nil, mapInboxError(err)
	}
	return &decision, nil
}

// Decisions returns the request's bounded retained assessment history.
func (m *Manager) Decisions(ctx context.Context, project domain.ProjectID, requestID string) ([]domain.AgentManagerDecision, error) {
	if _, err := m.Request(ctx, project, requestID); err != nil {
		return nil, err
	}
	store, err := m.decisionStore()
	if err != nil {
		return nil, err
	}
	items, err := store.ListAgentManagerDecisions(ctx, project, requestID)
	return items, mapInboxError(err)
}

// AssessProposal consumes only already-attributed native output. It cannot accept
// a public compatibility verdict or turn malformed output into a runnable choice.
// The result selects a configuration; AO's shared scheduler owns worker admission.
func (m *Manager) AssessProposal(ctx context.Context, project domain.ProjectID, requestID, proposalID string) (*domain.AgentManagerDecision, error) {
	proposal, err := m.Proposal(ctx, project, requestID, proposalID)
	if err != nil {
		return nil, err
	}
	store, err := m.decisionStore()
	if err != nil {
		return nil, err
	}
	retained, found, err := store.GetAgentManagerDecision(ctx, project, requestID, proposalID)
	if err != nil {
		return nil, mapInboxError(err)
	}
	if found {
		return &retained, nil
	}
	if proposal.Definition == nil || proposal.Definition.Action != "select_existing" {
		return nil, nil
	}
	if _, found, err := store.GetAgentManagerRequestResolution(ctx, project, requestID); err != nil || found {
		return nil, mapInboxError(err)
	}
	request, err := m.Request(ctx, project, requestID)
	if err != nil {
		return nil, err
	}
	if proposal.ContextID == "" {
		return nil, m.closeAssessmentRequest(ctx, store, request, "needs_human", "Legacy native proposal has no sealed input attribution; inspect before rerouting")
	}
	if m.candidates == nil {
		return nil, apierr.NotImplemented("AGENT_MANAGER_CANDIDATES_UNAVAILABLE", "Manager candidate assessment is unavailable")
	}
	assessmentCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	// One bounded retry refreshes an observation if durable policy/configuration
	// changed between native checking and the atomic decision transaction.
	for retry := 0; retry < 2; retry++ {
		configuration, err := m.Get(assessmentCtx, project)
		if err != nil {
			return nil, err
		}
		task, err := store.GetAdaptiveTask(assessmentCtx, request.TaskID)
		if err != nil {
			return nil, mapInboxError(err)
		}
		if !configuration.Definition.Enabled || configuration.ContentHash != request.ConfigurationHash || task.Revision != request.TaskRevision {
			return nil, m.closeAssessmentRequest(assessmentCtx, store, request, "superseded", "Task or governance changed before selection assessment")
		}
		cancelled, err := store.CancelledTaskAncestor(assessmentCtx, task.ID)
		if err != nil {
			return nil, mapInboxError(err)
		}
		if cancelled != "" {
			return nil, m.closeAssessmentRequest(assessmentCtx, store, request, "cancelled", "Task or ancestor cancellation prevents routing")
		}
		_, _, revision, rec, err := m.candidateInput(assessmentCtx, project, requestID)
		if err != nil {
			return nil, err
		}
		_, projectHash, err := domain.TaskContent(rec.Config)
		if err != nil {
			return nil, err
		}
		input := domain.AgentManagerAssessment{ProjectID: project, RequestID: requestID, ProposalID: proposalID, ProjectConfigurationHash: projectHash, Candidates: []domain.AgentManagerCandidate{}}
		for _, selection := range proposal.Definition.CandidateSelections() {
			candidate, err := m.candidates.AssessManagerCandidate(assessmentCtx, selection.AgentTypeID, selection.Version, revision.Definition, rec)
			if err != nil {
				return nil, err
			}
			input.Candidates = append(input.Candidates, candidate)
		}
		input.ObservedAt = time.Now().UTC()
		decision, _, err := store.RecordAgentManagerDecision(assessmentCtx, input)
		if err == nil {
			return &decision, nil
		}
		prerequisitesChanged := errors.Is(err, ports.ErrRegistryConflict) || errors.Is(err, ports.ErrRegistryForbidden) || errors.Is(err, ports.ErrRegistryInvalid) || errors.Is(err, ports.ErrRegistryNotFound) || errors.Is(err, ports.ErrAgentManagerConflict) || errors.Is(err, ports.ErrAgentManagerFenced) || errors.Is(err, ports.ErrTaskLeaseFenced)
		if !prerequisitesChanged {
			return nil, mapInboxError(err)
		}
		if existing, found, readErr := store.GetAgentManagerDecision(assessmentCtx, project, requestID, proposalID); readErr != nil {
			return nil, mapInboxError(readErr)
		} else if found {
			return &existing, nil
		}
		if _, found, readErr := store.GetAgentManagerRequestResolution(assessmentCtx, project, requestID); readErr != nil {
			return nil, mapInboxError(readErr)
		} else if found {
			return nil, nil
		}
	}
	return nil, apierr.Conflict("AGENT_MANAGER_SELECTION_CHANGED", "Selection prerequisites changed during assessment; retry the same proposal or inspect its decision", nil)
}

func (m *Manager) closeAssessmentRequest(ctx context.Context, store decisionWorkStore, request domain.AgentManagerRequest, outcome, reason string) error {
	err := store.ResolveAgentManagerRequest(ctx, request.ProjectID, domain.AgentManagerRequestResolution{RequestID: request.ID, Outcome: outcome, Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "manager-selector"}, Reason: reason, CreatedAt: time.Now().UTC()})
	if errors.Is(err, ports.ErrAgentManagerConflict) {
		return nil
	}
	return mapInboxError(err)
}
