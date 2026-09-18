package registry

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
)

// NativeConfiguration is the existing daemon catalog and native readiness
// boundary. Bindings select its provider references, never arbitrary endpoints.
type NativeConfiguration interface {
	Configuration(context.Context, string, domain.SessionMode) (agentsvc.Configuration, error)
	Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error)
	EnsureAgentReadiness(context.Context, string, domain.AgentReadinessPurpose) (domain.AgentReadinessSnapshot, error)
}

// BindingCreateInput names a reusable reference to the harness's native config.
// An empty provider means native defaults; a named provider must be advertised
// in the current catalog. ProjectID optionally restricts where it may be used.
type BindingCreateInput struct {
	Name      string              `json:"name"`
	Harness   domain.AgentHarness `json:"harness"`
	Provider  string              `json:"provider"`
	ProjectID string              `json:"projectId,omitempty"`
	Reason    string              `json:"reason"`
}

// BindingUpdateInput cannot reroute an existing identity or carry credentials.
type BindingUpdateInput struct {
	Name             string `json:"name"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

// CreateBinding creates a human-owned reference after native validation.
func (m *Manager) CreateBinding(ctx context.Context, actor domain.RegistryActor, input BindingCreateInput) (domain.ProviderBinding, error) {
	if actor.Origin != domain.RegistryUser {
		return domain.ProviderBinding{}, mapError(ports.ErrRegistryForbidden)
	}
	if m.native == nil {
		return domain.ProviderBinding{}, apierr.NotImplemented("NATIVE_CONFIGURATION_UNAVAILABLE", "Native configuration service is unavailable")
	}
	binding := domain.ProviderBinding{ID: uuid.NewString(), Name: input.Name, Harness: input.Harness, Provider: input.Provider, ProjectID: input.ProjectID, Enabled: true}
	if err := binding.Validate(); err != nil {
		return domain.ProviderBinding{}, invalid(err)
	}
	mutation := domain.RegistryMutation{Actor: actor, Reason: input.Reason}
	if err := mutation.Validate(); err != nil {
		return domain.ProviderBinding{}, invalid(err)
	}
	catalog, err := m.native.Models(ctx, string(input.Harness), input.ProjectID, true)
	if err != nil {
		return domain.ProviderBinding{}, err
	}
	if input.Provider != "" {
		found := false
		for _, model := range catalog.Models {
			if model.Provider == input.Provider {
				found = true
				break
			}
		}
		if !found || catalog.Stale {
			return domain.ProviderBinding{}, apierr.Invalid("NATIVE_PROVIDER_UNAVAILABLE", "The selected provider is not available in the current native catalog", nil)
		}
	}
	result, err := m.store.CreateProviderBinding(ctx, binding, mutation)
	return result, mapError(err)
}

// Bindings returns a bounded page of references, including disabled ones.
func (m *Manager) Bindings(ctx context.Context, after string, limit int) ([]domain.ProviderBinding, error) {
	if err := validatePage(limit); err != nil {
		return nil, err
	}
	return m.store.ListProviderBindings(ctx, after, limit)
}

// Binding reads one reference without resolving or exposing authentication.
func (m *Manager) Binding(ctx context.Context, id string) (domain.ProviderBinding, error) {
	result, err := m.store.GetProviderBinding(ctx, id)
	return result, mapError(err)
}

// UpdateBinding changes descriptive state; the native target stays immutable.
func (m *Manager) UpdateBinding(ctx context.Context, actor domain.RegistryActor, id string, input BindingUpdateInput) (domain.ProviderBinding, error) {
	mutation := domain.RegistryMutation{Actor: actor, Reason: input.Reason, ExpectedRevision: input.ExpectedRevision}
	if err := validateMutation(mutation); err != nil {
		return domain.ProviderBinding{}, err
	}
	if err := (domain.RegistryMetadata{Name: input.Name}).Validate(); err != nil {
		return domain.ProviderBinding{}, invalid(err)
	}
	result, err := m.store.UpdateProviderBinding(ctx, id, input.Name, input.Enabled, mutation)
	return result, mapError(err)
}

// BindingAudit returns the chronological, non-secret changes to a reference.
func (m *Manager) BindingAudit(ctx context.Context, id string, after int64, limit int) ([]domain.ProviderBindingAudit, error) {
	if err := validatePage(limit); err != nil {
		return nil, err
	}
	if _, err := m.Binding(ctx, id); err != nil {
		return nil, err
	}
	return m.store.ListProviderBindingAudit(ctx, id, after, limit)
}

// ResolveBinding validates the reference against the target project at each
// fresh launch. A missing/disabled reference never falls back to native default.
func (m *Manager) ResolveBinding(ctx context.Context, definition domain.AgentTypeDefinition, projectID string) (*domain.ProviderBinding, error) {
	if definition.ProviderBindingID == "" {
		if definition.ProviderBindingRequired {
			return nil, apierr.Invalid("PROVIDER_REBIND_REQUIRED", "Select a local provider binding before launch", nil)
		}
		return nil, nil
	}
	binding, err := m.Binding(ctx, definition.ProviderBindingID)
	if err != nil {
		return nil, err
	}
	if !binding.Enabled || binding.Harness != definition.Harness || (binding.ProjectID != "" && binding.ProjectID != projectID) {
		return nil, apierr.Invalid("PROVIDER_BINDING_INCOMPATIBLE", "Provider binding is disabled or incompatible with this harness/project", nil)
	}
	if binding.Provider != "" && definition.Config.Model == "" {
		return nil, invalid(fmt.Errorf("a named provider binding requires an explicit model"))
	}
	return &binding, nil
}
