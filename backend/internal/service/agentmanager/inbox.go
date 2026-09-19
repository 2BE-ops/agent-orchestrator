package agentmanager

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// EnqueueInput is retryable routing intent without caller-supplied authority.
type EnqueueInput struct {
	ID                   string `json:"id"`
	TaskID               string `json:"taskId"`
	TaskRevision         int64  `json:"taskRevision"`
	ConfigurationVersion int64  `json:"configurationVersion"`
	Reason               string `json:"reason"`
}

// ResolveInput closes routing work without changing a task, lease or controller.
type ResolveInput struct {
	Outcome string `json:"outcome" enum:"cancelled,superseded,needs_human"`
	Reason  string `json:"reason"`
}

func mapInboxError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrAgentManagerNotFound), errors.Is(err, ports.ErrTaskNotFound):
		return apierr.NotFound("AGENT_MANAGER_REQUEST_NOT_FOUND", "Manager request, task or governing configuration was not found in this project")
	case errors.Is(err, ports.ErrAgentManagerConflict), errors.Is(err, ports.ErrTaskConflict):
		return apierr.Conflict("AGENT_MANAGER_REQUEST_CONFLICT", "Routing intent changed, a retry differs, or the inbox limit was reached; inspect current work", nil)
	case errors.Is(err, ports.ErrAgentManagerFenced), errors.Is(err, ports.ErrTaskLeaseFenced):
		return apierr.Conflict("AGENT_MANAGER_WORK_FENCED", "Current Manager governance or task intent prevents new routing work", nil)
	case errors.Is(err, ports.ErrTaskForbidden), errors.Is(err, ports.ErrAgentManagerForbidden):
		return apierr.Forbidden("AGENT_MANAGER_INBOX_FORBIDDEN", "Routing intent requires trusted user, orchestrator or system context")
	case errors.Is(err, ports.ErrAgentManagerInvalid), errors.Is(err, ports.ErrTaskInvalid):
		return apierr.Invalid("INVALID_AGENT_MANAGER_REQUEST", err.Error(), nil)
	default:
		return err
	}
}

func (m *Manager) inboxStore() (ports.AgentManagerInboxStore, error) {
	store, ok := m.store.(ports.AgentManagerInboxStore)
	if !ok {
		return nil, apierr.NotImplemented("AGENT_MANAGER_INBOX_UNAVAILABLE", "Manager inbox persistence is unavailable")
	}
	return store, nil
}

func validateInboxActor(actor domain.AdaptiveActor) error {
	if actor.Kind != "USER" && actor.Kind != "ORCHESTRATOR" && actor.Kind != "SYSTEM" {
		return mapInboxError(ports.ErrTaskForbidden)
	}
	if actor.Kind != "ORCHESTRATOR" && actor.SessionID != "" {
		return mapInboxError(ports.ErrTaskForbidden)
	}
	return nil
}

// Enqueue records exact routing intent. It never starts a native controller.
func (m *Manager) Enqueue(ctx context.Context, actor domain.AdaptiveActor, project domain.ProjectID, input EnqueueInput) (domain.AgentManagerRequest, error) {
	if err := validateInboxActor(actor); err != nil {
		return domain.AgentManagerRequest{}, err
	}
	request := domain.AgentManagerEnqueue{ID: input.ID, ProjectID: project, TaskID: input.TaskID, TaskRevision: input.TaskRevision, ConfigurationVersion: input.ConfigurationVersion, Actor: actor, Reason: input.Reason, Now: time.Now().UTC()}
	if err := request.Validate(); err != nil {
		return domain.AgentManagerRequest{}, apierr.Invalid("INVALID_AGENT_MANAGER_REQUEST", err.Error(), nil)
	}
	store, err := m.inboxStore()
	if err != nil {
		return domain.AgentManagerRequest{}, err
	}
	result, _, err := store.EnqueueAgentManagerRequest(ctx, request)
	return result, mapInboxError(err)
}

// Request reads sealed routing intent scoped to the project.
func (m *Manager) Request(ctx context.Context, project domain.ProjectID, id string) (domain.AgentManagerRequest, error) {
	if err := validateProject(project); err != nil {
		return domain.AgentManagerRequest{}, err
	}
	if err := validateProject(domain.ProjectID(id)); err != nil {
		return domain.AgentManagerRequest{}, apierr.Invalid("INVALID_AGENT_MANAGER_REQUEST", "Request ID must be nonempty, bounded and contain no control characters", nil)
	}
	store, err := m.inboxStore()
	if err != nil {
		return domain.AgentManagerRequest{}, err
	}
	result, err := store.GetAgentManagerRequest(ctx, project, id)
	return result, mapInboxError(err)
}

// Requests pages retained history or pending work without acknowledging delivery.
func (m *Manager) Requests(ctx context.Context, project domain.ProjectID, after int64, limit int, pendingOnly bool) ([]domain.AgentManagerRequest, error) {
	if err := validateProject(project); err != nil {
		return nil, err
	}
	if err := validatePage(after, limit); err != nil {
		return nil, err
	}
	store, err := m.inboxStore()
	if err != nil {
		return nil, err
	}
	items, err := store.ListAgentManagerRequests(ctx, project, after, limit, pendingOnly)
	return items, mapInboxError(err)
}

// Resolve closes pending routing intent and returns its original durable receipt.
func (m *Manager) Resolve(ctx context.Context, actor domain.AdaptiveActor, project domain.ProjectID, id string, input ResolveInput) (domain.AgentManagerRequestResolution, error) {
	if err := validateInboxActor(actor); err != nil {
		return domain.AgentManagerRequestResolution{}, err
	}
	if err := validateProject(project); err != nil {
		return domain.AgentManagerRequestResolution{}, err
	}
	resolution := domain.AgentManagerRequestResolution{RequestID: id, Outcome: input.Outcome, Actor: actor, Reason: input.Reason, CreatedAt: time.Now().UTC()}
	if err := resolution.Validate(); err != nil {
		return domain.AgentManagerRequestResolution{}, apierr.Invalid("INVALID_AGENT_MANAGER_REQUEST", err.Error(), nil)
	}
	store, err := m.inboxStore()
	if err != nil {
		return domain.AgentManagerRequestResolution{}, err
	}
	if err := store.ResolveAgentManagerRequest(ctx, project, resolution); err != nil {
		return domain.AgentManagerRequestResolution{}, mapInboxError(err)
	}
	retained, found, err := store.GetAgentManagerRequestResolution(ctx, project, id)
	if err == nil && !found {
		return domain.AgentManagerRequestResolution{}, mapInboxError(ports.ErrAgentManagerNotFound)
	}
	return retained, mapInboxError(err)
}

// Resolution returns a terminal receipt or nil for still-pending routing intent.
func (m *Manager) Resolution(ctx context.Context, project domain.ProjectID, id string) (*domain.AgentManagerRequestResolution, error) {
	if _, err := m.Request(ctx, project, id); err != nil {
		return nil, err
	}
	store, err := m.inboxStore()
	if err != nil {
		return nil, err
	}
	result, found, err := store.GetAgentManagerRequestResolution(ctx, project, id)
	if err != nil || !found {
		return nil, mapInboxError(err)
	}
	return &result, nil
}
