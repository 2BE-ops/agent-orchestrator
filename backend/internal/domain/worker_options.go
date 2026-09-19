package domain

// ResolveWorkerOptions merges compatible project defaults and explicit one-off
// choices. Both native launch and atomic Manager decision validation use this
// pure resolver, so persistence cannot accept a different effective configuration.
func ResolveWorkerOptions(definition AgentTypeDefinition, override WorkerOverrides, project ProjectConfig, defaultMode SessionMode) AgentTypeDefinition {
	if override.Harness != nil && *override.Harness != definition.Harness {
		definition.Harness = *override.Harness
		definition.Config.Model = ""
		definition.Config.Effort = ""
		definition.Config.Mode = ""
	}
	base := AgentConfig{Permissions: project.AgentConfig.Permissions}
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
		definition.Config.Permissions = PermissionModeAuto
	}
	if override.SessionMode != nil {
		definition.SessionMode = *override.SessionMode
	}
	if definition.SessionMode == "" {
		definition.SessionMode = NormalizeSessionMode(defaultMode)
	}
	if override.ProviderBindingID != nil {
		definition.ProviderBindingID = *override.ProviderBindingID
		definition.ProviderBindingRequired = false
	}
	if override.Instructions != nil {
		definition.Instructions = *override.Instructions
	}
	if override.Skills != nil {
		definition.Skills = append([]SkillVersionRef{}, (*override.Skills)...)
	}
	return definition
}

func mergeWorkerConfig(base, override AgentConfig) AgentConfig {
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
