package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var _ ports.WorkerConfigurationResolver = (*Manager)(nil)

// ResolveWorker freezes a definition and its ordered Skills after merging only
// compatible project defaults and explicit one-off overrides. The manager adds
// its standing project instructions and seals the result before seed insertion.
func (m *Manager) ResolveWorker(ctx context.Context, selection domain.WorkerSelection, project domain.ProjectRecord, defaultMode domain.SessionMode, actor domain.RegistryActor) (domain.WorkerConfiguration, error) {
	entry, err := m.entry(ctx, domain.RegistryAgentType, selection.AgentTypeID)
	if err != nil {
		return domain.WorkerConfiguration{}, err
	}
	if !entry.Metadata.Enabled {
		return domain.WorkerConfiguration{}, apierr.Invalid("AGENT_TYPE_DISABLED", "Enable this Agent Type before launch", nil)
	}
	if actor.Origin == domain.RegistryManager && !entry.Metadata.Policy.ManagerCanSelect {
		return domain.WorkerConfiguration{}, mapError(ports.ErrRegistryForbidden)
	}
	versionNumber := selection.Version
	if versionNumber == 0 {
		versionNumber = entry.ActiveVersion
	}
	version, err := m.Version(ctx, domain.RegistryAgentType, entry.ID, versionNumber)
	if err != nil {
		return domain.WorkerConfiguration{}, err
	}
	definition := resolveWorkerOptions(*version.Definition.AgentType, selection.Overrides, project.Config, defaultMode)
	check, err := m.CheckConfiguration(ctx, definition, project.ID)
	if err != nil {
		return domain.WorkerConfiguration{}, err
	}
	if !check.Ready {
		return domain.WorkerConfiguration{}, configurationUnavailable(check)
	}
	binding, err := m.ResolveBinding(ctx, definition, project.ID)
	if err != nil {
		return domain.WorkerConfiguration{}, err
	}
	result := domain.WorkerConfiguration{SchemaVersion: 1, AgentType: domain.WorkerDefinitionRef{ID: entry.ID, Version: version.Number, Name: entry.Metadata.Name, ContentHash: version.ContentHash}, Selection: selection, Effective: definition, Skills: []domain.WorkerSkillSnapshot{}, Provider: binding, Origin: actor.Origin, ActorID: actor.ID, CatalogFingerprint: check.CatalogFingerprint, CreatedAt: time.Now().UTC()}
	for _, pin := range definition.Skills {
		skill, err := m.entry(ctx, domain.RegistrySkill, pin.ID)
		if err != nil {
			return result, err
		}
		if actor.Origin == domain.RegistryManager && !skill.Metadata.Policy.ManagerCanSelect {
			return result, mapError(ports.ErrRegistryForbidden)
		}
		version, err := m.Version(ctx, domain.RegistrySkill, pin.ID, pin.Version)
		if err != nil {
			return result, err
		}
		result.Skills = append(result.Skills, domain.WorkerSkillSnapshot{Reference: domain.WorkerDefinitionRef{ID: pin.ID, Version: pin.Version, Name: skill.Metadata.Name, ContentHash: version.ContentHash}, Definition: *version.Definition.Skill})
	}
	return result, nil
}

func resolveWorkerOptions(definition domain.AgentTypeDefinition, override domain.WorkerOverrides, project domain.ProjectConfig, defaultMode domain.SessionMode) domain.AgentTypeDefinition {
	if override.Harness != nil && *override.Harness != definition.Harness {
		definition.Harness = *override.Harness
		definition.Config.Model = ""
		definition.Config.Effort = ""
		definition.Config.Mode = ""
	}
	base := domain.AgentConfig{Permissions: project.AgentConfig.Permissions}
	role := project.Worker
	if role.Harness == "" || role.Harness == definition.Harness {
		base = mergeWorkerConfig(project.AgentConfig, role.AgentConfig)
	} else if role.AgentConfig.Permissions != "" {
		base.Permissions = role.AgentConfig.Permissions
	}
	definition.Config = mergeWorkerConfig(base, definition.Config)
	if override.Model != nil {
		definition.Config.Model = *override.Model
		definition.Config.Mode = ""
		definition.Config.Effort = ""
	}
	if override.Mode != nil {
		definition.Config.Mode = *override.Mode
		definition.Config.Model = ""
		definition.Config.Effort = ""
	}
	if override.Effort != nil {
		definition.Config.Effort = *override.Effort
	}
	if override.Permissions != nil {
		definition.Config.Permissions = *override.Permissions
	}
	if definition.Config.Permissions == "" {
		definition.Config.Permissions = domain.PermissionModeAuto
	}
	if override.SessionMode != nil {
		definition.SessionMode = *override.SessionMode
	}
	if definition.SessionMode == "" {
		definition.SessionMode = domain.NormalizeSessionMode(defaultMode)
	}
	if override.ProviderBindingID != nil {
		definition.ProviderBindingID = *override.ProviderBindingID
		definition.ProviderBindingRequired = false
	}
	if override.Instructions != nil {
		definition.Instructions = *override.Instructions
	}
	if override.Skills != nil {
		definition.Skills = append([]domain.SkillVersionRef{}, (*override.Skills)...)
	}
	return definition
}

func mergeWorkerConfig(base, override domain.AgentConfig) domain.AgentConfig {
	if override.Model != "" {
		if base.Model != override.Model {
			base.Effort = ""
		}
		base.Model = override.Model
		base.Mode = ""
	}
	if override.Mode != "" {
		if base.Mode != override.Mode {
			base.Effort = ""
		}
		base.Mode = override.Mode
		base.Model = ""
	}
	if override.Effort != "" {
		base.Effort = override.Effort
	}
	if override.Permissions != "" {
		base.Permissions = override.Permissions
	}
	return base
}

// ValidateWorkerRestore checks current native availability against retained
// content, without consulting mutable definition selection/activation policy.
// Active-host adoption must not invoke this fresh-launch operation.
func (m *Manager) ValidateWorkerRestore(ctx context.Context, snapshot domain.WorkerConfiguration, projectID string) error {
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("worker configuration: %w", err)
	}
	check, err := m.checkConfiguration(ctx, snapshot.Effective, projectID, append([]domain.WorkerSkillSnapshot{}, snapshot.Skills...))
	if err != nil {
		return err
	}
	if !check.Ready {
		return configurationUnavailable(check)
	}
	return nil
}

func configurationUnavailable(check ConfigurationCheck) error {
	return apierr.Invalid("WORKER_CONFIGURATION_UNAVAILABLE", "Resolve the Agent Type's native configuration requirements before launch", map[string]any{"issues": check.Issues})
}

// ResolveWorkerChange validates an explicit execution change against retained
// content. Same-harness edits never inherit newly changed project defaults.
func (m *Manager) ResolveWorkerChange(ctx context.Context, current domain.WorkerConfiguration, override domain.WorkerOverrides, project domain.ProjectRecord) (domain.WorkerConfiguration, error) {
	if err := current.Validate(); err != nil {
		return current, err
	}
	projectConfig := domain.ProjectConfig{}
	selection := current.Selection.Overrides
	if override.Harness != nil && *override.Harness != current.Effective.Harness {
		projectConfig = project.Config
		selection.Model = nil
		selection.Mode = nil
		selection.Effort = nil
	}
	current.Effective = resolveWorkerOptions(current.Effective, override, projectConfig, current.Effective.SessionMode)
	if override.Harness != nil {
		selection.Harness = override.Harness
	}
	if override.SessionMode != nil {
		selection.SessionMode = override.SessionMode
	}
	if override.Model != nil {
		selection.Model = override.Model
		selection.Mode = nil
		selection.Effort = nil
	}
	if override.Mode != nil {
		selection.Mode = override.Mode
		selection.Model = nil
		selection.Effort = nil
	}
	if override.Effort != nil {
		selection.Effort = override.Effort
	}
	if override.Permissions != nil {
		selection.Permissions = override.Permissions
	}
	if override.ProviderBindingID != nil {
		selection.ProviderBindingID = override.ProviderBindingID
	}
	if override.Instructions != nil || override.Skills != nil {
		return current, apierr.Invalid("WORKER_CONTENT_IMMUTABLE", "Execution changes retain the original instructions and Skill content", nil)
	}
	current.Selection.Overrides = selection
	check, err := m.checkConfiguration(ctx, current.Effective, project.ID, append([]domain.WorkerSkillSnapshot{}, current.Skills...))
	if err != nil {
		return current, err
	}
	if !check.Ready {
		return current, configurationUnavailable(check)
	}
	current.Provider, err = m.ResolveBinding(ctx, current.Effective, project.ID)
	if err != nil {
		return current, err
	}
	current.CatalogFingerprint = check.CatalogFingerprint
	current.ContentHash = current.Hash()
	return current, current.Validate()
}
