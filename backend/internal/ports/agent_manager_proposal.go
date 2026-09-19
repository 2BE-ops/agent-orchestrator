package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// AgentManagerProposalStore preserves native output without granting a launch.
type AgentManagerProposalStore interface {
	SubmitAgentManagerProposal(context.Context, domain.AgentManagerProposalSubmission) (domain.AgentManagerProposal, bool, error)
	GetAgentManagerProposal(context.Context, domain.ProjectID, string, string) (domain.AgentManagerProposal, error)
	ListAgentManagerProposals(context.Context, domain.ProjectID, string) ([]domain.AgentManagerProposal, error)
}
