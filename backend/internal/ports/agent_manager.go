package ports

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Manager governance errors preserve policy and revision boundaries.
var (
	ErrAgentManagerNotFound  = errors.New("agent manager or configuration not found")
	ErrAgentManagerConflict  = errors.New("agent manager configuration changed")
	ErrAgentManagerForbidden = errors.New("agent manager governing policy requires user authority")
	ErrAgentManagerInvalid   = errors.New("invalid agent manager configuration")
)

// AgentManagerStore retains user-owned manager governance and exact history.
type AgentManagerStore interface {
	ConfigureAgentManager(context.Context, domain.ProjectID, domain.AgentManagerDefinition, domain.TaskMutation) (domain.AgentManagerConfiguration, error)
	GetAgentManager(context.Context, domain.ProjectID) (domain.AgentManagerConfiguration, error)
	GetAgentManagerConfiguration(context.Context, domain.ProjectID, int64) (domain.AgentManagerConfiguration, error)
	ListAgentManagerConfigurations(context.Context, domain.ProjectID, int64, int) ([]domain.AgentManagerConfiguration, error)
	ListAgentManagerAudit(context.Context, domain.ProjectID, int64, int) ([]domain.AgentManagerAudit, error)
}
