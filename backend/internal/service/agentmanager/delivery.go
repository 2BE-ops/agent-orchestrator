package agentmanager

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Contexts returns bounded historical inputs independently of live ownership.
func (m *Manager) Contexts(ctx context.Context, project domain.ProjectID, requestID string) ([]domain.AgentManagerContext, error) {
	if _, err := m.Request(ctx, project, requestID); err != nil {
		return nil, err
	}
	store, ok := m.store.(ports.AgentManagerContextStore)
	if !ok {
		return nil, apierr.NotImplemented("AGENT_MANAGER_CONTEXT_UNAVAILABLE", "Manager input persistence is unavailable")
	}
	items, err := store.ListAgentManagerContexts(ctx, project, requestID)
	return items, mapInboxError(err)
}

// Context reads exact classified native input rather than rebuilding a prompt.
func (m *Manager) Context(ctx context.Context, project domain.ProjectID, requestID, id string) (domain.AgentManagerContext, error) {
	if err := validateProject(domain.ProjectID(id)); err != nil {
		return domain.AgentManagerContext{}, err
	}
	if _, err := m.Request(ctx, project, requestID); err != nil {
		return domain.AgentManagerContext{}, err
	}
	store, ok := m.store.(ports.AgentManagerContextStore)
	if !ok {
		return domain.AgentManagerContext{}, apierr.NotImplemented("AGENT_MANAGER_CONTEXT_UNAVAILABLE", "Manager input persistence is unavailable")
	}
	item, err := store.GetAgentManagerContext(ctx, project, requestID, id)
	return item, mapInboxError(err)
}

// Deliveries exposes transport observations, never implied receipt or success.
func (m *Manager) Deliveries(ctx context.Context, project domain.ProjectID, requestID string) ([]domain.AgentManagerDelivery, error) {
	if _, err := m.Request(ctx, project, requestID); err != nil {
		return nil, err
	}
	store, ok := m.store.(ports.AgentManagerDeliveryStore)
	if !ok {
		return nil, apierr.NotImplemented("AGENT_MANAGER_DELIVERY_UNAVAILABLE", "Manager delivery persistence is unavailable")
	}
	items, err := store.ListAgentManagerDeliveries(ctx, project, requestID)
	return items, mapInboxError(err)
}
