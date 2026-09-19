package ports

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ErrAgentManagerFenced preserves ownership until explicit native reconciliation.
var ErrAgentManagerFenced = errors.New("agent manager controller requires ownership reconciliation")

// AgentManagerControllerStore closes admission, seed and native-generation races.
// A repeated reservation/seed/Begin never authorizes another process launch.
type AgentManagerControllerStore interface {
	ReserveAgentManagerController(context.Context, domain.AgentManagerControllerReservation) (domain.AgentManagerController, bool, error)
	GetAgentManagerController(context.Context, string) (domain.AgentManagerController, error)
	ActiveAgentManagerController(context.Context, domain.ProjectID) (domain.AgentManagerController, bool, error)
	ListAgentManagerControllers(context.Context, domain.ProjectID, string, int) ([]domain.AgentManagerController, error)
	CreateAgentManagerSession(context.Context, domain.AgentManagerControllerToken, domain.SessionRecord, domain.WorkerConfiguration, time.Time) (domain.SessionRecord, bool, error)
	GetAgentManagerDispatch(context.Context, string) (domain.AgentManagerControllerDispatch, bool, error)
	GetAgentManagerDispatchBySession(context.Context, domain.SessionID) (domain.AgentManagerControllerDispatch, bool, error)
	ReleaseAgentManagerController(context.Context, domain.AgentManagerControllerRelease) error
	BeginAgentManagerExecution(context.Context, domain.AgentManagerExecutionOperation) (bool, error)
	PendingAgentManagerExecution(context.Context, domain.SessionID) (domain.AgentManagerExecutionOperation, bool, error)
	ResolveAgentManagerExecution(context.Context, domain.AgentManagerExecutionResolution) error
}
