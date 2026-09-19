package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// AgentManagerDecisionStore retains trusted checks atomically with routing
// resolution/audit. It never acquires a worker lease or executes native I/O.
type AgentManagerDecisionStore interface {
	RecordAgentManagerDecision(context.Context, domain.AgentManagerAssessment) (domain.AgentManagerDecision, bool, error)
	GetAgentManagerDecision(context.Context, domain.ProjectID, string, string) (domain.AgentManagerDecision, bool, error)
	ListAgentManagerDecisions(context.Context, domain.ProjectID, string) ([]domain.AgentManagerDecision, error)
}

// AgentManagerUnassessedProposal is a paging reference, never unclassified content.
type AgentManagerUnassessedProposal struct {
	ProjectID  domain.ProjectID
	RequestID  string
	ProposalID string
}

// AgentManagerDecisionRecoveryStore provides a fair restart-safe scan. Parser
// failures and terminal requests already have their own bounded resolution.
type AgentManagerDecisionRecoveryStore interface {
	ListUnassessedAgentManagerProposals(context.Context, string, int) ([]AgentManagerUnassessedProposal, error)
	AgentManagerDecisionCursor(context.Context) (string, error)
	SetAgentManagerDecisionCursor(context.Context, string) error
}
