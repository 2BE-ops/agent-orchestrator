package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// AgentManagerDeliveryStore atomically seals input and reserves a bounded send.
// Historical reads never imply that an existing claim may be sent again.
type AgentManagerDeliveryStore interface {
	BeginAgentManagerDelivery(context.Context, domain.AgentManagerContextSeal, string) (domain.AgentManagerDelivery, bool, error)
	ValidateAgentManagerDelivery(context.Context, string) error
	ResolveAgentManagerDelivery(context.Context, domain.AgentManagerDeliveryResolution) error
	ListAgentManagerDeliveries(context.Context, domain.ProjectID, string) ([]domain.AgentManagerDelivery, error)
	ListUnresolvedAgentManagerDeliveries(context.Context, string, int) ([]domain.AgentManagerDelivery, error)
	ListDispatchableAgentManagerRequests(context.Context, int64, int) ([]domain.AgentManagerRequest, error)
	AgentManagerDispatchCursor(context.Context) (int64, error)
	SetAgentManagerDispatchCursor(context.Context, int64) error
}

// AgentManagerTransportResult distinguishes a proven refusal from uncertain I/O.
type AgentManagerTransportResult struct {
	State  string
	Reason string
}

// AgentManagerTransport reuses the existing native session and operation guards.
type AgentManagerTransport interface {
	AgentManagerTargetReady(context.Context, domain.SessionID) (bool, error)
	DeliverAgentManagerContext(context.Context, domain.AgentManagerDelivery, domain.AgentManagerContext) AgentManagerTransportResult
}
