package agentmanager

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ProposalInput is native output and retry identity, never claimed authority.
type ProposalInput struct {
	SourceGeneration string `json:"sourceGeneration"`
	IdempotencyKey   string `json:"idempotencyKey"`
	Raw              string `json:"raw"`
}

// ProposalReceipt retains native output plus optional deterministic assessment.
// A selected routing outcome is not a worker launch or task completion receipt.
type ProposalReceipt struct {
	Proposal       domain.AgentManagerProposal  `json:"proposal"`
	Created        bool                         `json:"created"`
	Decision       *domain.AgentManagerDecision `json:"decision,omitempty"`
	RoutingOutcome string                       `json:"routingOutcome,omitempty" enum:"cancelled,superseded,needs_human,selected"`
}

type nativeProposalStore interface {
	ports.AgentManagerProposalStore
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
}

func (m *Manager) proposalStore() (nativeProposalStore, error) {
	store, ok := m.store.(nativeProposalStore)
	if !ok {
		return nil, apierr.NotImplemented("AGENT_MANAGER_PROPOSALS_UNAVAILABLE", "Manager proposal persistence is unavailable")
	}
	return store, nil
}

// Propose derives native attribution from session facts and rechecks it in the
// store transaction. Raw malformed/empty output remains a counted proposal.
func (m *Manager) Propose(ctx context.Context, sessionID domain.SessionID, requestID string, input ProposalInput) (ProposalReceipt, error) {
	var receipt ProposalReceipt
	if err := validateProject(domain.ProjectID(input.SourceGeneration)); err != nil {
		return receipt, apierr.Invalid("INVALID_MANAGER_GENERATION", "A bounded native sourceGeneration is required", nil)
	}
	submission := domain.AgentManagerProposalSubmission{ID: uuid.NewString(), ProjectID: "pending-session-lookup", RequestID: requestID, IdempotencyKey: input.IdempotencyKey, SessionID: sessionID, Raw: input.Raw, Now: time.Now().UTC()}
	if err := submission.Validate(); err != nil {
		return receipt, apierr.Invalid("INVALID_AGENT_MANAGER_PROPOSAL", err.Error(), nil)
	}
	store, err := m.proposalStore()
	if err != nil {
		return receipt, err
	}
	rec, found, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return receipt, err
	}
	if !found {
		return receipt, mapInboxError(ports.ErrAgentManagerNotFound)
	}
	owner := rec.ControllerOwner()
	generation := owner.RuntimeLaunchID
	if owner.Mode == domain.SessionModeChat {
		generation = owner.ControllerGeneration
	}
	if rec.Kind != domain.KindAgentManager || rec.ProjectID == "" || rec.IsTerminated || generation != input.SourceGeneration {
		return receipt, apierr.Conflict("AGENT_MANAGER_OWNER_CHANGED", "The proposal source is not the current live Manager generation", nil)
	}
	submission.ProjectID, submission.SourceOwner = rec.ProjectID, owner
	receipt.Proposal, receipt.Created, err = store.SubmitAgentManagerProposal(ctx, submission)
	if err != nil {
		return receipt, mapInboxError(err)
	}
	if m.candidates != nil {
		receipt.Decision, err = m.AssessProposal(ctx, rec.ProjectID, requestID, receipt.Proposal.ID)
		if err != nil {
			return receipt, err
		}
		var resolution *domain.AgentManagerRequestResolution
		resolution, err = m.Resolution(ctx, rec.ProjectID, requestID)
		// Native feedback carries only the terminal code, never an unclassified
		// caller's free-form resolution reason or claimed actor identity.
		if resolution != nil {
			receipt.RoutingOutcome = resolution.Outcome
		}
	}
	return receipt, err
}

// Proposals reads at most five retained parser/selection claims within a project.
func (m *Manager) Proposals(ctx context.Context, project domain.ProjectID, requestID string) ([]domain.AgentManagerProposal, error) {
	if _, err := m.Request(ctx, project, requestID); err != nil {
		return nil, err
	}
	store, err := m.proposalStore()
	if err != nil {
		return nil, err
	}
	items, err := store.ListAgentManagerProposals(ctx, project, requestID)
	return items, mapInboxError(err)
}

// Proposal reads exact historical native output independently of live ownership.
func (m *Manager) Proposal(ctx context.Context, project domain.ProjectID, requestID, proposalID string) (domain.AgentManagerProposal, error) {
	if err := validateProject(domain.ProjectID(proposalID)); err != nil {
		return domain.AgentManagerProposal{}, apierr.Invalid("INVALID_AGENT_MANAGER_PROPOSAL", "A bounded proposal ID is required", nil)
	}
	if _, err := m.Request(ctx, project, requestID); err != nil {
		return domain.AgentManagerProposal{}, err
	}
	store, err := m.proposalStore()
	if err != nil {
		return domain.AgentManagerProposal{}, err
	}
	proposal, err := store.GetAgentManagerProposal(ctx, project, requestID, proposalID)
	return proposal, mapInboxError(err)
}
