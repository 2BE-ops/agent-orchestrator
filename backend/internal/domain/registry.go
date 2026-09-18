package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// RegistryKind separates reusable worker configurations from composable skills.
type RegistryKind string

// Registry kinds are independent of native provider skill discovery.
const (
	RegistryAgentType RegistryKind = "agent_type"
	RegistrySkill     RegistryKind = "skill"
)

// RegistryActor is supplied by the application action context, never by a
// definition's import payload. It records provenance, not OS-level isolation.
type RegistryActor struct {
	Origin RegistryOrigin
	ID     string
}

// RegistryOrigin identifies who created a definition or performed an action.
type RegistryOrigin string

// The supported registry actors.
const (
	RegistryUser    RegistryOrigin = "USER"
	RegistryManager RegistryOrigin = "AGENT_MANAGER"
	RegistrySystem  RegistryOrigin = "SYSTEM"
)

// RegistryPolicy controls three independently authorized manager actions.
type RegistryPolicy struct {
	ManagerCanSelect  bool `json:"managerCanSelect"`
	ManagerCanModify  bool `json:"managerCanModify"`
	ManagerCanVersion bool `json:"managerCanVersion"`
}

// RegistryMetadata is mutable descriptive/ownership policy, protected by a revision.
type RegistryMetadata struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Enabled     bool           `json:"enabled"`
	Policy      RegistryPolicy `json:"policy"`
}

// RegistryEntry is the stable identity shared by an Agent Type or authored Skill.
// Editing configuration appends a version; editing metadata increments Revision.
type RegistryEntry struct {
	ID            string
	Kind          RegistryKind
	Origin        RegistryOrigin
	CreatedBy     string
	Metadata      RegistryMetadata
	Revision      int64
	ActiveVersion int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SkillVersionRef pins an exact authored Skill version, in composition order.
type SkillVersionRef struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

// AgentTypeDefinition packages AO's existing harness/config vocabulary. Adapter
// capability and readiness checks still run in services before launch.
type AgentTypeDefinition struct {
	Harness            AgentHarness      `json:"harness"`
	SessionMode        SessionMode       `json:"sessionMode,omitempty"`
	Config             AgentConfig       `json:"config"`
	ProviderBindingID  string            `json:"providerBindingId,omitempty"`
	Instructions       string            `json:"instructions"`
	Capabilities       []string          `json:"capabilities"`
	Skills             []SkillVersionRef `json:"skills"`
	MaxParallelWorkers int               `json:"maxParallelWorkers"`
}

// SkillResource is inert UTF-8 content materialized only under an AO-owned
// session directory. It is never executed or imported into native user settings.
type SkillResource struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// SkillDefinition is a versioned authored skill, separate from live discovery.
type SkillDefinition struct {
	Instructions       string          `json:"instructions"`
	Capabilities       []string        `json:"capabilities"`
	RequiredTools      []string        `json:"requiredTools"`
	RequiredMCPServers []string        `json:"requiredMcpServers"`
	Resources          []SkillResource `json:"resources"`
}

// RegistryDefinition is a closed, typed union; exactly one member must match Kind.
type RegistryDefinition struct {
	AgentType *AgentTypeDefinition `json:"agentType,omitempty"`
	Skill     *SkillDefinition     `json:"skill,omitempty"`
}

// RegistryVersion is immutable, including its creator, parent and content hash.
type RegistryVersion struct {
	EntryID       string
	Number        int64
	ParentVersion int64
	Definition    RegistryDefinition
	ContentHash   string
	Actor         RegistryActor
	Reason        string
	CreatedAt     time.Time
}

// RegistryAudit is a durable semantic record, independent of CDC retention.
type RegistryAudit struct {
	Sequence      int64
	EntryID       string
	Revision      int64
	VersionNumber int64
	Action        string
	Actor         RegistryActor
	Reason        string
	CreatedAt     time.Time
}

// RegistryMutation identifies the trusted caller and its optimistic concurrency
// precondition. Reasons are required for configuration and policy history.
type RegistryMutation struct {
	Actor            RegistryActor
	ExpectedRevision int64
	Reason           string
}

// Validate checks identity and bounded explanatory content before persistence.
func (m RegistryMutation) Validate() error {
	switch m.Actor.Origin {
	case RegistryUser, RegistryManager, RegistrySystem:
	default:
		return fmt.Errorf("invalid registry actor")
	}
	if !registryText(m.Actor.ID, 200, true) || !registryText(m.Reason, 2000, true) || m.ExpectedRevision < 0 {
		return fmt.Errorf("registry actor, reason or expected revision is invalid")
	}
	return nil
}

// Validate rejects empty names and oversized registry metadata.
func (m RegistryMetadata) Validate() error {
	if !registryText(m.Name, 120, true) || !registryText(m.Description, 4000, false) {
		return fmt.Errorf("registry name or description is invalid")
	}
	return nil
}

// Validate enforces structural validity without inventing provider capabilities.
func (d RegistryDefinition) Validate(kind RegistryKind) error {
	switch kind {
	case RegistryAgentType:
		if d.AgentType == nil || d.Skill != nil {
			return fmt.Errorf("agent type definition required")
		}
		a := d.AgentType
		if !a.Harness.IsKnown() {
			return fmt.Errorf("unknown harness %q", a.Harness)
		}
		if _, err := ParseSessionMode(string(a.SessionMode)); err != nil {
			return err
		}
		if err := a.Config.Validate(); err != nil {
			return err
		}
		if a.MaxParallelWorkers < 1 || a.MaxParallelWorkers > 1000 {
			return fmt.Errorf("maximum parallel workers must be between 1 and 1000")
		}
		if !registryText(a.Instructions, 65536, false) || !registryText(a.ProviderBindingID, 200, false) ||
			!registryText(a.Config.Model, 256, false) || !registryText(a.Config.Effort, 100, false) {
			return fmt.Errorf("agent configuration exceeds content limits")
		}
		if err := registryTags(a.Capabilities); err != nil {
			return err
		}
		if len(a.Skills) > 32 {
			return fmt.Errorf("at most 32 skills may be attached")
		}
		seen := make(map[string]bool, len(a.Skills))
		for _, ref := range a.Skills {
			if !registryText(ref.ID, 200, true) || ref.Version < 1 || seen[ref.ID] {
				return fmt.Errorf("invalid or duplicate pinned skill")
			}
			seen[ref.ID] = true
		}
	case RegistrySkill:
		if d.Skill == nil || d.AgentType != nil {
			return fmt.Errorf("skill definition required")
		}
		s := d.Skill
		if !registryText(s.Instructions, 65536, true) {
			return fmt.Errorf("skill instructions are required and limited to 65536 bytes")
		}
		for _, tags := range [][]string{s.Capabilities, s.RequiredTools, s.RequiredMCPServers} {
			if err := registryTags(tags); err != nil {
				return err
			}
		}
		if len(s.Resources) > 32 {
			return fmt.Errorf("at most 32 skill resources are supported")
		}
		seen := make(map[string]bool, len(s.Resources))
		total := 0
		for _, r := range s.Resources {
			key := strings.ToLower(r.Path)
			if !safeSkillResourcePath(r.Path) || seen[key] || !registryText(r.Content, 65536, false) {
				return fmt.Errorf("invalid, duplicate or oversized skill resource")
			}
			seen[key] = true
			total += len(r.Content)
		}
		if total > 262144 {
			return fmt.Errorf("skill resources exceed 262144 bytes")
		}
	default:
		return fmt.Errorf("unknown registry kind %q", kind)
	}
	return nil
}

// MarshalContent returns the exact stored bytes and their reproducibility hash.
func (d RegistryDefinition) MarshalContent(kind RegistryKind) ([]byte, string, error) {
	if err := d.Validate(kind); err != nil {
		return nil, "", err
	}
	content, err := json.Marshal(d)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(content)
	return content, hex.EncodeToString(hash[:]), nil
}

func registryText(value string, limit int, required bool) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && len(value) <= limit && (!required || strings.TrimSpace(value) != "")
}

func registryTags(tags []string) error {
	if len(tags) > 64 {
		return fmt.Errorf("at most 64 capabilities or requirements are supported")
	}
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if !registryText(tag, 120, true) || seen[tag] {
			return fmt.Errorf("invalid or duplicate capability or requirement")
		}
		seen[tag] = true
	}
	return nil
}

func safeSkillResourcePath(value string) bool {
	if !registryText(value, 240, true) || value == "." || path.IsAbs(value) || path.Clean(value) != value ||
		strings.ContainsAny(value, "\\:< >\"|?*") || strings.IndexFunc(value, unicode.IsControl) >= 0 ||
		strings.EqualFold(value, "SKILL.md") || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if strings.HasSuffix(part, ".") || strings.HasPrefix(part, ".") {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
			(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9') {
			return false
		}
	}
	return true
}
