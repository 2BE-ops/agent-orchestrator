package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// AgentManagerRegistryStore commits normal registry writes, policy quotas and
// native attribution in one transaction. Reads expose retained exact receipts.
type AgentManagerRegistryStore interface {
	ApplyAgentManagerRegistryAction(context.Context, domain.AgentManagerRegistrySubmission) (domain.AgentManagerRegistryReceipt, bool, error)
	GetAgentManagerRegistryReceipt(context.Context, domain.ProjectID, string, string) (domain.AgentManagerRegistryReceipt, error)
	ListAgentManagerRegistryReceipts(context.Context, domain.ProjectID, string, string, int) ([]domain.AgentManagerRegistryReceipt, error)
}
