package registry

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// AssessManagerCandidate applies the same native configuration gate as worker
// launch, after checking Manager ownership and the exact version's clearance.
// Required capabilities are explicit prerequisites, not an LLM quality score.
func (m *Manager) AssessManagerCandidate(ctx context.Context, id string, number int64, task domain.TaskDefinition, project domain.ProjectRecord) (domain.AgentManagerCandidate, error) {
	result := domain.AgentManagerCandidate{AgentType: domain.WorkerDefinitionRef{ID: id, Version: number}, MaxContextClass: domain.ContextTechnical, Capabilities: []string{}, Skills: []domain.AgentManagerCandidateSkill{}, MissingCapabilities: []string{}, Issues: []domain.AgentManagerCandidateIssue{}}
	if strings.TrimSpace(id) == "" || len(id) > 200 || strings.IndexFunc(id, unicode.IsControl) >= 0 || number < 1 {
		return result, apierr.Invalid("INVALID_MANAGER_CANDIDATE", "An exact Type identity and version are required", nil)
	}
	if err := task.Validate(); err != nil {
		return result, invalid(err)
	}
	add := func(code, state string) {
		result.Issues = append(result.Issues, domain.AgentManagerCandidateIssue{Code: code, State: state})
	}
	entry, err := m.store.GetRegistryEntry(ctx, id)
	if errors.Is(err, ports.ErrRegistryNotFound) || (err == nil && entry.Kind != domain.RegistryAgentType) {
		add("AGENT_TYPE_NOT_FOUND", "invalid")
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.MetadataRevision, result.AgentType.Name = entry.Revision, entry.Metadata.Name
	if !entry.Metadata.Enabled {
		add("AGENT_TYPE_DISABLED", "invalid")
	}
	if !entry.Metadata.Policy.ManagerCanSelect {
		add("MANAGER_SELECTION_FORBIDDEN", "invalid")
	}
	version, err := m.store.GetRegistryVersion(ctx, id, number)
	if errors.Is(err, ports.ErrRegistryNotFound) {
		add("AGENT_TYPE_VERSION_NOT_FOUND", "invalid")
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err := version.Definition.Validate(domain.RegistryAgentType); err != nil {
		return result, err
	}
	result.AgentType.ContentHash = version.ContentHash
	defaultMode := domain.DefaultSessionMode
	if m.defaults != nil {
		defaultMode = m.defaults.DefaultSessionMode(ctx)
	}
	definition := resolveWorkerOptions(*version.Definition.AgentType, domain.WorkerOverrides{}, project.Config, defaultMode)
	result.MaxContextClass, result.Harness, result.SessionMode = definition.MaxContextClass.Effective(), definition.Harness, definition.SessionMode
	result.Config, result.ProviderBindingID = definition.Config, definition.ProviderBindingID
	result.Capabilities = append(result.Capabilities, definition.Capabilities...)
	if !result.MaxContextClass.Allows(task.Classification) {
		add("CONTEXT_CLEARANCE_EXCEEDED", "invalid")
	}
	// Never probe native providers for a statically prohibited Type.
	if len(result.Issues) > 0 {
		return result, nil
	}
	for _, pin := range definition.Skills {
		skill, err := m.store.GetRegistryEntry(ctx, pin.ID)
		if errors.Is(err, ports.ErrRegistryNotFound) || (err == nil && skill.Kind != domain.RegistrySkill) {
			add("SKILL_NOT_FOUND", "invalid")
			continue
		}
		if err != nil {
			return result, err
		}
		if !skill.Metadata.Enabled {
			add("SKILL_DISABLED", "invalid")
		}
		if !skill.Metadata.Policy.ManagerCanSelect {
			add("MANAGER_SKILL_SELECTION_FORBIDDEN", "invalid")
		}
		version, err := m.store.GetRegistryVersion(ctx, pin.ID, pin.Version)
		if errors.Is(err, ports.ErrRegistryNotFound) {
			add("SKILL_VERSION_NOT_FOUND", "invalid")
			continue
		}
		if err != nil {
			return result, err
		}
		if err := version.Definition.Validate(domain.RegistrySkill); err != nil {
			return result, err
		}
		result.Skills = append(result.Skills, domain.AgentManagerCandidateSkill{Reference: domain.WorkerDefinitionRef{ID: pin.ID, Version: pin.Version, Name: skill.Metadata.Name, ContentHash: version.ContentHash}, MetadataRevision: skill.Revision})
		for _, capability := range version.Definition.Skill.Capabilities {
			if !slices.Contains(result.Capabilities, capability) {
				result.Capabilities = append(result.Capabilities, capability)
			}
		}
	}
	for _, required := range task.RequiredCapabilities {
		if !slices.Contains(result.Capabilities, required) {
			result.MissingCapabilities = append(result.MissingCapabilities, required)
		}
	}
	if len(result.MissingCapabilities) > 0 {
		add("REQUIRED_CAPABILITIES_MISSING", "invalid")
	}
	if len(result.Issues) > 0 {
		return result, nil
	}
	if m.native == nil {
		add("NATIVE_CONFIGURATION_UNAVAILABLE", "unavailable")
		return result, nil
	}
	check, err := m.CheckConfiguration(ctx, definition, project.ID)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, issue := range check.Issues {
		add(issue.Code, issue.State)
	}
	result.Eligible = check.Ready && len(result.Issues) == 0
	result.CatalogFingerprint, result.BindingRevision = check.CatalogFingerprint, check.BindingRevision
	return result, nil
}
