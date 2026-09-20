package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// WorkerOverrides preserve omission versus an explicit native default or empty
// Skill list. They affect one worker and never mutate its registry definition.
type WorkerOverrides struct {
	Harness           *AgentHarness      `json:"harness,omitempty"`
	SessionMode       *SessionMode       `json:"sessionMode,omitempty"`
	Model             *string            `json:"model,omitempty"`
	Effort            *string            `json:"effort,omitempty"`
	Mode              *string            `json:"mode,omitempty"`
	Permissions       *PermissionMode    `json:"permissions,omitempty"`
	ProviderBindingID *string            `json:"providerBindingId,omitempty"`
	Instructions      *string            `json:"instructions,omitempty"`
	Skills            *[]SkillVersionRef `json:"skills,omitempty"`
}

// WorkerSelection names a reusable definition; zero version resolves active once.
type WorkerSelection struct {
	AgentTypeID string          `json:"agentTypeId"`
	Version     int64           `json:"version,omitempty"`
	Overrides   WorkerOverrides `json:"overrides"`
}

// WorkerDefinitionRef records the exact historical configuration and display name.
type WorkerDefinitionRef struct {
	ID          string `json:"id"`
	Version     int64  `json:"version"`
	Name        string `json:"name"`
	ContentHash string `json:"contentHash"`
}

// WorkerSkillSnapshot retains inert content even if registry availability changes.
type WorkerSkillSnapshot struct {
	Reference  WorkerDefinitionRef `json:"reference"`
	Definition SkillDefinition     `json:"definition"`
}

// WorkerConfiguration is the immutable launch record, inserted atomically with
// the session seed. Native secrets and project environment values never enter it.
type WorkerConfiguration struct {
	SchemaVersion      int                   `json:"schemaVersion"`
	AgentType          WorkerDefinitionRef   `json:"agentType"`
	Selection          WorkerSelection       `json:"selection"`
	Effective          AgentTypeDefinition   `json:"effective"`
	NativeSettings     *ConversationSettings `json:"nativeSettings,omitempty"`
	NativeOptions      []WorkerNativeOption  `json:"nativeOptions,omitempty"`
	Skills             []WorkerSkillSnapshot `json:"skills"`
	Provider           *ProviderBinding      `json:"provider,omitempty"`
	Origin             RegistryOrigin        `json:"origin"`
	ActorID            string                `json:"actorId"`
	SystemPrompt       string                `json:"systemPrompt"`
	CatalogFingerprint string                `json:"catalogFingerprint,omitempty"`
	CreatedAt          time.Time             `json:"createdAt"`
	ContentHash        string                `json:"contentHash"`
}

// Hash hashes the complete launch facts, excluding only the hash field itself.
func (c WorkerConfiguration) Hash() string {
	c.ContentHash = ""
	encoded, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// Validate rejects unsealed, inconsistent or oversized launch records.
func (c WorkerConfiguration) Validate() error {
	if c.SchemaVersion != 1 || c.CreatedAt.IsZero() || len(c.ContentHash) != 64 || c.ContentHash != c.Hash() {
		return fmt.Errorf("worker configuration must be a sealed schema v1 snapshot")
	}
	if c.AgentType.ID == "" || c.AgentType.Version < 1 || len(c.AgentType.ContentHash) != 64 || c.Selection.AgentTypeID != c.AgentType.ID || c.Selection.Version < 0 {
		return fmt.Errorf("worker Agent Type reference is invalid")
	}
	if err := (RegistryMutation{Actor: RegistryActor{Origin: c.Origin, ID: c.ActorID}, Reason: "worker launch"}).Validate(); err != nil {
		return err
	}
	if !c.Effective.SessionMode.Valid() {
		return fmt.Errorf("worker session mode must be resolved")
	}
	if err := ValidateWorkerNativeOptions(c.NativeOptions); err != nil {
		return err
	}
	if c.Effective.ProviderBindingRequired && c.Effective.ProviderBindingID == "" {
		return fmt.Errorf("worker provider rebinding is unresolved")
	}
	if err := (RegistryDefinition{AgentType: &c.Effective}).Validate(RegistryAgentType); err != nil {
		return err
	}
	if len(c.Skills) != len(c.Effective.Skills) {
		return fmt.Errorf("worker Skill snapshot count does not match composition")
	}
	for index, skill := range c.Skills {
		pin := c.Effective.Skills[index]
		if pin.ID != skill.Reference.ID || pin.Version != skill.Reference.Version {
			return fmt.Errorf("worker Skill snapshot order does not match composition")
		}
		definition := RegistryDefinition{Skill: &skill.Definition}
		if err := definition.Validate(RegistrySkill); err != nil {
			return err
		}
		_, hash, err := definition.MarshalContent(RegistrySkill)
		if err != nil || hash != skill.Reference.ContentHash {
			return fmt.Errorf("worker Skill content hash is invalid")
		}
	}
	if c.Provider == nil && c.Effective.ProviderBindingID != "" {
		return fmt.Errorf("worker provider reference is missing")
	}
	if c.Provider != nil && (c.Provider.ID != c.Effective.ProviderBindingID || c.Provider.Harness != c.Effective.Harness || !c.Provider.Enabled) {
		return fmt.Errorf("worker provider reference is incompatible")
	}
	encoded, _ := json.Marshal(c)
	if len(encoded) > 1<<20 {
		return fmt.Errorf("worker configuration exceeds 1 MiB")
	}
	return nil
}
