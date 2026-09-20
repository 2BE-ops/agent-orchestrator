package registry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// PortableBundle is an explicit allowlist, not a serialization of local records.
// It contains no IDs, actors, account references, endpoints or credentials.
type PortableBundle struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Kind          domain.RegistryKind     `json:"kind" enum:"agent_type,skill"`
	Name          string                  `json:"name"`
	Description   string                  `json:"description"`
	AgentType     *PortableAgentType      `json:"agentType,omitempty"`
	Skill         *domain.SkillDefinition `json:"skill,omitempty"`
}

// PortableAgentType includes ordered snapshots of its exact pinned Skills.
type PortableAgentType struct {
	Harness                 domain.AgentHarness   `json:"harness"`
	SessionMode             domain.SessionMode    `json:"sessionMode,omitempty"`
	Model                   string                `json:"model,omitempty"`
	Effort                  string                `json:"effort,omitempty"`
	Mode                    string                `json:"mode,omitempty"`
	Permissions             domain.PermissionMode `json:"permissions,omitempty"`
	RequiresProviderBinding bool                  `json:"requiresProviderBinding,omitempty"`
	Instructions            string                `json:"instructions"`
	Capabilities            []string              `json:"capabilities"`
	MaxParallelWorkers      int                   `json:"maxParallelWorkers"`
	Skills                  []PortableSkill       `json:"skills"`
}

// PortableSkill is inert authored content, not a reference to native user state.
type PortableSkill struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Definition  domain.SkillDefinition `json:"definition"`
}

// ImportInput always creates new local identities with automatic management off.
type ImportInput struct {
	Bundle PortableBundle `json:"bundle"`
	Reason string         `json:"reason"`
}

// Imported contains the new root and any new local dependency identities.
type Imported struct {
	Root         View
	Skills       []domain.RegistryEntry
	Requirements []string
}

const portableBundleLimit = 1 << 20

// Export resolves the requested immutable version; active pointers are not
// followed for its dependencies. Merely exporting cannot alter native state.
func (m *Manager) Export(ctx context.Context, kind domain.RegistryKind, id string, number int64) (PortableBundle, error) {
	entry, err := m.entry(ctx, kind, id)
	if err != nil {
		return PortableBundle{}, err
	}
	version, err := m.Version(ctx, kind, id, number)
	if err != nil {
		return PortableBundle{}, err
	}
	bundle := PortableBundle{SchemaVersion: 1, Kind: kind, Name: entry.Metadata.Name, Description: entry.Metadata.Description}
	definition := version.Definition.NormalizeLists()
	if kind == domain.RegistrySkill {
		bundle.Skill = definition.Skill
	} else {
		a := definition.AgentType
		portable := &PortableAgentType{Harness: a.Harness, SessionMode: a.SessionMode,
			Model: a.Config.Model, Effort: a.Config.Effort, Mode: a.Config.Mode, Permissions: a.Config.Permissions,
			RequiresProviderBinding: a.ProviderBindingRequired || a.ProviderBindingID != "",
			Instructions:            a.Instructions, Capabilities: a.Capabilities, MaxParallelWorkers: a.MaxParallelWorkers, Skills: []PortableSkill{}}
		for _, pin := range a.Skills {
			skill, err := m.entry(ctx, domain.RegistrySkill, pin.ID)
			if err != nil {
				return PortableBundle{}, err
			}
			version, err := m.store.GetRegistryVersion(ctx, pin.ID, pin.Version)
			if err != nil {
				return PortableBundle{}, mapError(err)
			}
			portable.Skills = append(portable.Skills, PortableSkill{Name: skill.Metadata.Name, Description: skill.Metadata.Description, Definition: *version.Definition.NormalizeLists().Skill})
		}
		bundle.AgentType = portable
	}
	if _, _, err := bundle.prepare(); err != nil {
		return PortableBundle{}, invalid(err)
	}
	return bundle, nil
}

// Import validates the entire bundle before one atomic store transaction.
// Automatic selection/modification/versioning and enabled state start false,
// regardless of the exported source's policy. Resources are never executed.
func (m *Manager) Import(ctx context.Context, actor domain.RegistryActor, kind domain.RegistryKind, input ImportInput) (Imported, error) {
	if input.Bundle.Kind != kind {
		return Imported{}, invalid(fmt.Errorf("bundle kind does not match destination"))
	}
	inputs, requirements, err := input.Bundle.prepare()
	if err != nil {
		return Imported{}, invalid(err)
	}
	mutation := domain.RegistryMutation{Actor: actor, Reason: input.Reason}
	if err := mutation.Validate(); err != nil {
		return Imported{}, invalid(err)
	}
	entries, err := m.store.CreateRegistryEntries(ctx, inputs, mutation)
	if err != nil {
		return Imported{}, mapError(err)
	}
	root := entries[len(entries)-1]
	version, err := m.store.GetRegistryVersion(ctx, root.ID, 1)
	if err != nil {
		return Imported{}, mapError(err)
	}
	return Imported{Root: View{Entry: root, Version: version}, Skills: entries[:len(entries)-1], Requirements: requirements}, nil
}

func (b PortableBundle) prepare() ([]domain.RegistryCreate, []string, error) {
	if b.SchemaVersion != 1 {
		return nil, nil, fmt.Errorf("unsupported portable schema version %d", b.SchemaVersion)
	}
	encoded, err := json.Marshal(b)
	if err != nil || len(encoded) > portableBundleLimit {
		return nil, nil, fmt.Errorf("portable bundle exceeds 1 MiB")
	}
	root := domain.RegistryCreate{ID: uuid.NewString(), Kind: b.Kind, Metadata: domain.RegistryMetadata{Name: b.Name, Description: b.Description}}
	inputs := make([]domain.RegistryCreate, 0, 33)
	requirements := []string{"Review and enable imported definitions; manager permissions start off."}
	switch b.Kind {
	case domain.RegistrySkill:
		if b.Skill == nil || b.AgentType != nil {
			return nil, nil, fmt.Errorf("skill bundle requires only skill content")
		}
		root.Definition.Skill = b.Skill
	case domain.RegistryAgentType:
		if b.AgentType == nil || b.Skill != nil {
			return nil, nil, fmt.Errorf("agent type bundle requires only agent type content")
		}
		a := b.AgentType
		if len(a.Skills) > 32 {
			return nil, nil, fmt.Errorf("at most 32 skills may be imported")
		}
		definition := &domain.AgentTypeDefinition{Harness: a.Harness, SessionMode: a.SessionMode,
			Config:                  domain.AgentConfig{Model: a.Model, Effort: a.Effort, Mode: a.Mode, Permissions: a.Permissions},
			ProviderBindingRequired: a.RequiresProviderBinding, Instructions: a.Instructions,
			Capabilities: a.Capabilities, MaxParallelWorkers: a.MaxParallelWorkers, Skills: []domain.SkillVersionRef{}}
		if a.RequiresProviderBinding {
			requirements = append(requirements, "Select a local provider binding before launch.")
		}
		for _, skill := range a.Skills {
			input := domain.RegistryCreate{ID: uuid.NewString(), Kind: domain.RegistrySkill,
				Metadata: domain.RegistryMetadata{Name: skill.Name, Description: skill.Description}, Definition: domain.RegistryDefinition{Skill: &skill.Definition}}
			inputs = append(inputs, input)
			definition.Skills = append(definition.Skills, domain.SkillVersionRef{ID: input.ID, Version: 1})
		}
		root.Definition.AgentType = definition
	default:
		return nil, nil, fmt.Errorf("unsupported bundle kind")
	}
	inputs = append(inputs, root)
	for _, input := range inputs {
		if err := input.Metadata.Validate(); err != nil {
			return nil, nil, err
		}
		if err := input.Definition.Validate(input.Kind); err != nil {
			return nil, nil, err
		}
		if skill := input.Definition.Skill; skill != nil {
			for _, tool := range skill.RequiredTools {
				requirements = append(requirements, "Required tool: "+tool)
			}
			for _, server := range skill.RequiredMCPServers {
				requirements = append(requirements, "Required MCP server: "+server)
			}
		}
	}
	return inputs, requirements, nil
}
