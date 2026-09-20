// Package agentmanager owns persistent routing-controller governance and tools.
// Native execution remains in AO's shared session and conversation engines.
package agentmanager

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Manager applies governance validation before persistence or native effects.
type Manager struct {
	store      ports.AgentManagerStore
	native     NativeRuntime
	candidates CandidateAssessor
}

// New constructs the Manager boundary over the daemon's existing store.
func New(store ports.AgentManagerStore) *Manager { return &Manager{store: store} }

// NewWithRuntime binds dedicated Manager admission to the shared native engine.
func NewWithRuntime(store ports.AgentManagerStore, native NativeRuntime) *Manager {
	return &Manager{store: store, native: native}
}

// ConfigureInput contains desired policy, never actor authority or process state.
type ConfigureInput struct {
	Definition       domain.AgentManagerDefinition `json:"definition"`
	ExpectedRevision int64                         `json:"expectedRevision"`
	Reason           string                        `json:"reason"`
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrAgentManagerNotFound):
		return apierr.NotFound("AGENT_MANAGER_NOT_FOUND", "Manager, project or exact controller Type version was not found")
	case errors.Is(err, ports.ErrAgentManagerConflict):
		return apierr.Conflict("AGENT_MANAGER_REVISION_CONFLICT", "Manager configuration changed; reload its current revision", nil)
	case errors.Is(err, ports.ErrAgentManagerForbidden):
		return apierr.Forbidden("AGENT_MANAGER_GOVERNANCE_FORBIDDEN", "Only the user may change Manager governance")
	case errors.Is(err, ports.ErrAgentManagerInvalid):
		return apierr.Invalid("INVALID_AGENT_MANAGER", err.Error(), nil)
	default:
		return err
	}
}

func validateProject(id domain.ProjectID) error {
	if strings.TrimSpace(string(id)) == "" || len(id) > 200 || strings.IndexFunc(string(id), unicode.IsControl) >= 0 {
		return mapError(ports.ErrAgentManagerInvalid)
	}
	return nil
}

func validatePage(after int64, limit int) error {
	if after < 0 || limit < 1 || limit > 100 {
		return apierr.Invalid("INVALID_AGENT_MANAGER_PAGE", "Cursor must be non-negative and limit between 1 and 100", nil)
	}
	return nil
}

// Configure changes desired governance without launching or replacing controllers.
func (m *Manager) Configure(ctx context.Context, actor domain.AdaptiveActor, project domain.ProjectID, input ConfigureInput) (domain.AgentManagerConfiguration, error) {
	if actor.Kind != "USER" || actor.SessionID != "" {
		return domain.AgentManagerConfiguration{}, mapError(ports.ErrAgentManagerForbidden)
	}
	if err := validateProject(project); err != nil {
		return domain.AgentManagerConfiguration{}, err
	}
	mutation := domain.TaskMutation{Actor: actor, Reason: input.Reason, ExpectedRevision: input.ExpectedRevision}
	if err := mutation.ValidatePlanning(); err != nil {
		return domain.AgentManagerConfiguration{}, apierr.Invalid("INVALID_AGENT_MANAGER", err.Error(), nil)
	}
	if input.ExpectedRevision >= 1000 {
		return domain.AgentManagerConfiguration{}, mapError(ports.ErrAgentManagerInvalid)
	}
	if err := input.Definition.Validate(); err != nil {
		return domain.AgentManagerConfiguration{}, apierr.Invalid("INVALID_AGENT_MANAGER", err.Error(), nil)
	}
	configuration, err := m.store.ConfigureAgentManager(ctx, project, input.Definition, mutation)
	return configuration, mapError(err)
}

// Get reads current desired governance independently of native ownership.
func (m *Manager) Get(ctx context.Context, project domain.ProjectID) (domain.AgentManagerConfiguration, error) {
	if err := validateProject(project); err != nil {
		return domain.AgentManagerConfiguration{}, err
	}
	configuration, err := m.store.GetAgentManager(ctx, project)
	return configuration, mapError(err)
}

// Configuration reads one retained version without consulting live registry state.
func (m *Manager) Configuration(ctx context.Context, project domain.ProjectID, number int64) (domain.AgentManagerConfiguration, error) {
	if err := validateProject(project); err != nil {
		return domain.AgentManagerConfiguration{}, err
	}
	if number < 1 || number > 1000 {
		return domain.AgentManagerConfiguration{}, mapError(ports.ErrAgentManagerInvalid)
	}
	configuration, err := m.store.GetAgentManagerConfiguration(ctx, project, number)
	return configuration, mapError(err)
}

// Configurations pages immutable user governance history.
func (m *Manager) Configurations(ctx context.Context, project domain.ProjectID, after int64, limit int) ([]domain.AgentManagerConfiguration, error) {
	if err := validateProject(project); err != nil {
		return nil, err
	}
	if err := validatePage(after, limit); err != nil {
		return nil, err
	}
	items, err := m.store.ListAgentManagerConfigurations(ctx, project, after, limit)
	return items, mapError(err)
}

// Audit exposes durable policy provenance independently of CDC retention.
func (m *Manager) Audit(ctx context.Context, project domain.ProjectID, after int64, limit int) ([]domain.AgentManagerAudit, error) {
	if err := validateProject(project); err != nil {
		return nil, err
	}
	if err := validatePage(after, limit); err != nil {
		return nil, err
	}
	items, err := m.store.ListAgentManagerAudit(ctx, project, after, limit)
	return items, mapError(err)
}
