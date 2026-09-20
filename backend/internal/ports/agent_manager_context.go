package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// AgentManagerContextStore seals bounded native input before transport and
// exposes immutable history independently of live controller availability.
type AgentManagerContextStore interface {
	SealAgentManagerContext(context.Context, domain.AgentManagerContextSeal) (domain.AgentManagerContext, bool, error)
	GetAgentManagerContext(context.Context, domain.ProjectID, string, string) (domain.AgentManagerContext, error)
	ListAgentManagerContexts(context.Context, domain.ProjectID, string) ([]domain.AgentManagerContext, error)
}
