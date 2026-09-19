package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// AgentManagerInboxStore retains work independently of native controller life.
// A list/read does not acknowledge delivery or authorize a worker launch.
type AgentManagerInboxStore interface {
	EnqueueAgentManagerRequest(context.Context, domain.AgentManagerEnqueue) (domain.AgentManagerRequest, bool, error)
	GetAgentManagerRequest(context.Context, domain.ProjectID, string) (domain.AgentManagerRequest, error)
	ListAgentManagerRequests(context.Context, domain.ProjectID, int64, int, bool) ([]domain.AgentManagerRequest, error)
	ResolveAgentManagerRequest(context.Context, domain.ProjectID, domain.AgentManagerRequestResolution) error
	GetAgentManagerRequestResolution(context.Context, domain.ProjectID, string) (domain.AgentManagerRequestResolution, bool, error)
}
