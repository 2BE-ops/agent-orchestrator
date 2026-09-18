package registry

import (
	"context"
	"slices"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ConfigurationIssue distinguishes invalid choices from unverified availability.
type ConfigurationIssue struct {
	Code    string `json:"code"`
	State   string `json:"state" enum:"invalid,unavailable"`
	Message string `json:"message"`
}

// ConfigurationCheck is advisory authoring feedback and a fresh-launch gate.
// Active provider adoption must use its immutable snapshot instead of rerouting.
type ConfigurationCheck struct {
	Ready              bool                          `json:"ready"`
	Issues             []ConfigurationIssue          `json:"issues"`
	CatalogFingerprint string                        `json:"catalogFingerprint,omitempty"`
	BindingRevision    int64                         `json:"bindingRevision,omitempty"`
	Readiness          domain.AgentReadinessSnapshot `json:"readiness"`
}

// CheckInput checks an exact saved version within its intended project scope.
type CheckInput struct {
	Version   int64  `json:"version"`
	ProjectID string `json:"projectId,omitempty"`
}

// Check checks metadata, the exact version and its current native prerequisites.
func (m *Manager) Check(ctx context.Context, id string, input CheckInput) (ConfigurationCheck, error) {
	entry, err := m.entry(ctx, domain.RegistryAgentType, id)
	if err != nil {
		return ConfigurationCheck{}, err
	}
	version, err := m.Version(ctx, domain.RegistryAgentType, id, input.Version)
	if err != nil {
		return ConfigurationCheck{}, err
	}
	var project domain.ProjectRecord
	if input.ProjectID != "" {
		projects, ok := m.store.(interface {
			GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
		})
		if !ok {
			return ConfigurationCheck{}, apierr.NotImplemented("PROJECT_CONFIGURATION_UNAVAILABLE", "Project configuration is unavailable")
		}
		var found bool
		project, found, err = projects.GetProject(ctx, input.ProjectID)
		if err != nil {
			return ConfigurationCheck{}, err
		}
		if !found {
			return ConfigurationCheck{}, apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
		}
	}
	defaultMode := domain.DefaultSessionMode
	if m.defaults != nil {
		defaultMode = m.defaults.DefaultSessionMode(ctx)
	}
	definition := resolveWorkerOptions(*version.Definition.AgentType, domain.WorkerOverrides{}, project.Config, defaultMode)
	result, err := m.CheckConfiguration(ctx, definition, input.ProjectID)
	if !entry.Metadata.Enabled {
		result.Issues = append(result.Issues, ConfigurationIssue{Code: "DEFINITION_DISABLED", State: "invalid", Message: "Enable this Agent Type before launch."})
		result.Ready = false
	}
	return result, err
}

// CheckConfiguration is shared with the worker snapshot/launch path. It never
// starts a worker, writes native settings or substitutes a different provider.
func (m *Manager) CheckConfiguration(ctx context.Context, definition domain.AgentTypeDefinition, projectID string) (ConfigurationCheck, error) {
	return m.checkConfiguration(ctx, definition, projectID, nil)
}

func (m *Manager) checkConfiguration(ctx context.Context, definition domain.AgentTypeDefinition, projectID string, retained []domain.WorkerSkillSnapshot) (ConfigurationCheck, error) {
	result := ConfigurationCheck{Issues: []ConfigurationIssue{}}
	if err := (domain.RegistryDefinition{AgentType: &definition}).Validate(domain.RegistryAgentType); err != nil {
		return result, invalid(err)
	}
	if m.native == nil {
		return result, apierr.NotImplemented("NATIVE_CONFIGURATION_UNAVAILABLE", "Native configuration service is unavailable")
	}
	add := func(code, state, message string) {
		result.Issues = append(result.Issues, ConfigurationIssue{Code: code, State: state, Message: message})
	}
	binding, err := m.ResolveBinding(ctx, definition, projectID)
	if err != nil {
		add("PROVIDER_BINDING_UNAVAILABLE", "invalid", "Select an enabled local binding for this harness and project, with an explicit model for a named provider.")
	}
	if binding != nil {
		result.BindingRevision = binding.Revision
	}
	mode := domain.NormalizeSessionMode(definition.SessionMode)
	configuration, err := m.native.Configuration(ctx, string(definition.Harness), mode)
	if err != nil {
		add("CAPABILITIES_UNAVAILABLE", "unavailable", "Harness configuration capabilities could not be verified.")
	} else if configuration.CapabilityState != "supported" {
		state := "unavailable"
		if configuration.CapabilityState == "unsupported" {
			state = "invalid"
		}
		add("SESSION_MODE_UNAVAILABLE", state, configuration.Warning)
	}
	field := func(key string) bool {
		for _, field := range configuration.Fields {
			if field.Key == key {
				return true
			}
		}
		return false
	}
	catalog, catalogErr := m.native.Models(ctx, string(definition.Harness), projectID, true)
	needsCatalog := definition.Config.Model != "" || definition.Config.Mode != "" || definition.Config.Effort != "" || (binding != nil && binding.Provider != "")
	if catalogErr != nil || catalog.Stale {
		if needsCatalog {
			add("MODELS_UNAVAILABLE", "unavailable", "The native model catalog could not be freshly validated.")
		}
	} else {
		result.CatalogFingerprint = catalog.BinaryVersion
		if definition.Config.Model != "" {
			found := false
			for _, model := range catalog.Models {
				if model.ID == definition.Config.Model {
					found = true
				}
			}
			if !field("model") || catalog.SelectionMode == ports.ModelSelectionModeList || (!found && catalog.CustomModelEntry != ports.CustomModelEntryDirect) {
				add("MODEL_UNSUPPORTED", "invalid", "The selected model is not supported by this harness's native catalog.")
			}
		}
		if definition.Config.Mode != "" {
			found := false
			for _, model := range catalog.Models {
				if model.ID == definition.Config.Mode {
					found = true
				}
			}
			if catalog.SelectionMode != ports.ModelSelectionModeList || !found {
				add("AGENT_MODE_UNSUPPORTED", "invalid", "The selected agent mode is not advertised by this harness.")
			}
		}
		if definition.Config.Effort != "" {
			found := false
			for _, model := range catalog.Models {
				if (model.ID == definition.Config.Model || (definition.Config.Model == "" && model.IsDefault)) && slices.Contains(model.Efforts, definition.Config.Effort) {
					found = true
				}
			}
			if !found {
				add("EFFORT_UNSUPPORTED", "invalid", "The selected reasoning effort is not advertised for this model.")
			}
		}
		if binding != nil && binding.Provider != "" {
			found := false
			for _, model := range catalog.Models {
				if model.ID == definition.Config.Model && model.Provider == binding.Provider {
					found = true
				}
			}
			if !found {
				add("BOUND_PROVIDER_UNAVAILABLE", "invalid", "The selected model is no longer available from the bound native provider.")
			}
		}
	}
	if definition.Config.Permissions != "" {
		found := false
		for _, item := range configuration.Fields {
			if item.Key == "permissions" && slices.Contains(item.Options, string(definition.Config.Permissions)) {
				found = true
			}
		}
		if !found {
			add("PERMISSIONS_UNSUPPORTED", "invalid", "This harness does not advertise the selected permission mode.")
		}
	}
	if mode == domain.SessionModeChat && configuration.CapabilityState == "supported" {
		caps := ports.ChatCapabilities{}
		for _, name := range configuration.ChatCapabilities {
			caps[ports.ChatCapability(name)] = true
		}
		if len(ports.MissingCapabilitiesForPermissions(caps, definition.Config.Permissions)) != 0 {
			add("CHAT_CAPABILITIES_MISSING", "invalid", "The Chat driver lacks capabilities required for these permissions.")
		}
	}
	skills := make([]domain.SkillDefinition, 0, len(definition.Skills))
	if retained != nil {
		for _, skill := range retained {
			skills = append(skills, skill.Definition)
		}
	} else {
		for _, pin := range definition.Skills {
			entry, err := m.entry(ctx, domain.RegistrySkill, pin.ID)
			if err != nil || !entry.Metadata.Enabled {
				add("SKILL_UNAVAILABLE", "invalid", "A pinned Skill is missing or disabled: "+pin.ID)
				continue
			}
			version, err := m.store.GetRegistryVersion(ctx, pin.ID, pin.Version)
			if err != nil {
				add("SKILL_VERSION_UNAVAILABLE", "invalid", "A pinned Skill version could not be loaded: "+pin.ID)
				continue
			}
			skills = append(skills, *version.Definition.Skill)
		}
	}
	for _, skill := range skills {
		for _, tool := range skill.RequiredTools {
			if mode != domain.SessionModeChat || configuration.CapabilityState != "supported" || !slices.Contains(configuration.ChatCapabilities, tool) {
				add("SKILL_TOOL_UNVERIFIED", "unavailable", "The harness has not advertised required tool capability: "+tool)
			}
		}
		for _, server := range skill.RequiredMCPServers {
			add("SKILL_MCP_UNVERIFIED", "unavailable", "Native MCP server availability must be verified before this Skill can launch: "+server)
		}
	}
	readiness, err := m.native.EnsureAgentReadiness(ctx, string(definition.Harness), domain.AgentReadinessPurposeLaunch)
	result.Readiness = readiness
	if err != nil || readiness.EffectiveReadiness != domain.AgentReadinessReady {
		add("NATIVE_READINESS_UNAVAILABLE", "unavailable", "Check native harness installation and authentication before launch.")
	}
	result.Ready = len(result.Issues) == 0
	return result, nil
}
