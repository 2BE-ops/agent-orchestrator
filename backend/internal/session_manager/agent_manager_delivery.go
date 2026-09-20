package sessionmanager

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var _ ports.AgentManagerTransport = (*Manager)(nil)

// AgentManagerTargetReady is advisory and consumes no delivery attempt.
func (m *Manager) AgentManagerTargetReady(ctx context.Context, id domain.SessionID) (bool, error) {
	if m.SessionMutationInProgress(id) {
		return false, nil
	}
	rec, found, err := m.store.GetSession(ctx, id)
	if err != nil || !found {
		return false, err
	}
	return m.nativeContextRecipientReady(ctx, rec, domain.KindAgentManager)
}

// DeliverAgentManagerContext writes the exact sealed prompt through the shared
// native guard. A stale, unclassified or altered receipt cannot reach transport.
func (m *Manager) DeliverAgentManagerContext(ctx context.Context, delivery domain.AgentManagerDelivery, sealed domain.AgentManagerContext) ports.AgentManagerTransportResult {
	refuse := func(reason string) ports.AgentManagerTransportResult {
		return ports.AgentManagerTransportResult{State: "not_sent", Reason: reason}
	}
	if delivery.State != "dispatching" || delivery.RequestID != sealed.RequestID || delivery.ContextID != sealed.ID || delivery.ControllerID != sealed.ControllerID || delivery.SessionID != sealed.SessionID || delivery.NativeGeneration != sealed.NativeGeneration || delivery.DeliveryKey != "adaptive-manager:"+sealed.RequestID {
		return refuse("Manager delivery reservation does not match its sealed input")
	}
	if err := sealed.Validate(); err != nil {
		return refuse("Manager input is invalid or its sealed classification changed")
	}
	rec, found, err := m.store.GetSession(ctx, delivery.SessionID)
	if err != nil || !found {
		return refuse("Manager recipient could not be read before transport")
	}
	contexts, ok := m.store.(ports.AgentManagerContextStore)
	if !ok {
		return refuse("Manager input persistence is unavailable")
	}
	retained, err := contexts.GetAgentManagerContext(ctx, rec.ProjectID, sealed.RequestID, sealed.ID)
	if err != nil || retained.ContentHash != sealed.ContentHash {
		return refuse("Manager input does not match its retained receipt")
	}
	journal, ok := m.store.(ports.AgentManagerDeliveryStore)
	if !ok {
		return refuse("Manager delivery persistence is unavailable")
	}
	deliveries, err := journal.ListAgentManagerDeliveries(ctx, rec.ProjectID, sealed.RequestID)
	if err != nil {
		return refuse("Manager delivery reservation could not be read")
	}
	reserved := false
	for _, item := range deliveries {
		if item.ID == delivery.ID && item.ContextID == delivery.ContextID && item.ControllerID == delivery.ControllerID && item.SessionID == delivery.SessionID && item.Owner == delivery.Owner && item.Number == delivery.Number && item.DeliveryKey == delivery.DeliveryKey && item.State == "dispatching" {
			reserved = true
		}
	}
	if !reserved {
		return refuse("Manager delivery is no longer reserved")
	}
	check := func(checkCtx context.Context) error {
		return journal.ValidateAgentManagerDelivery(checkCtx, delivery.ID)
	}
	state, reason := m.deliverNativeContext(ctx, nativeContextDelivery{SessionID: delivery.SessionID, ProjectID: rec.ProjectID, Owner: delivery.Owner, Kind: domain.KindAgentManager, Operation: agentOperationManagerMessage, Prompt: sealed.Prompt, DeliveryKey: delivery.DeliveryKey, Check: check})
	return ports.AgentManagerTransportResult{State: state, Reason: reason}
}
