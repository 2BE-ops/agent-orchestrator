package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// AgentManagerPolicy is user-owned governance, independent of an individual
// registry entry's select/modify/version permissions. Both must permit actions.
type AgentManagerPolicy struct {
	Optimization        string `json:"optimization" enum:"quality,balanced,speed,usage"`
	AllowCreateTypes    bool   `json:"allowCreateTypes"`
	AllowCreateSkills   bool   `json:"allowCreateSkills"`
	AllowCreateVersions bool   `json:"allowCreateVersions"`
	MaxCreatedTypes     int    `json:"maxCreatedTypes"`
	MaxCreatedSkills    int    `json:"maxCreatedSkills"`
	MaxVersionsPerEntry int    `json:"maxVersionsPerEntry"`
	MaxPendingRequests  int    `json:"maxPendingRequests"`
	MaxProposalAttempts int    `json:"maxProposalAttempts"`
}

// DefaultAgentManagerPolicy permits selection while keeping evolution opt-in.
func DefaultAgentManagerPolicy() AgentManagerPolicy {
	return AgentManagerPolicy{Optimization: "balanced", MaxCreatedTypes: 8, MaxCreatedSkills: 16, MaxVersionsPerEntry: 8, MaxPendingRequests: 100, MaxProposalAttempts: 3}
}

// AgentManagerDefinition pins future native controllers to an exact Agent Type.
// Editing this desired configuration never rewrites a running controller's pins.
type AgentManagerDefinition struct {
	SchemaVersion    int                `json:"schemaVersion"`
	Enabled          bool               `json:"enabled"`
	AgentTypeID      string             `json:"agentTypeId"`
	AgentTypeVersion int64              `json:"agentTypeVersion"`
	Policy           AgentManagerPolicy `json:"policy"`
}

// Validate bounds governance and rejects floating native controller versions.
func (d AgentManagerDefinition) Validate() error {
	if d.SchemaVersion != 1 || !registryText(d.AgentTypeID, 200, true) || strings.IndexFunc(d.AgentTypeID, unicode.IsControl) >= 0 || d.AgentTypeVersion < 1 {
		return fmt.Errorf("manager configuration requires schema 1 and an exact Agent Type version")
	}
	p := d.Policy
	switch p.Optimization {
	case "quality", "balanced", "speed", "usage":
	default:
		return fmt.Errorf("invalid manager optimization preference")
	}
	if p.MaxCreatedTypes < 0 || p.MaxCreatedTypes > 32 || p.MaxCreatedSkills < 0 || p.MaxCreatedSkills > 64 || p.MaxVersionsPerEntry < 0 || p.MaxVersionsPerEntry > 32 || p.MaxPendingRequests < 1 || p.MaxPendingRequests > 1000 || p.MaxProposalAttempts < 1 || p.MaxProposalAttempts > 5 {
		return fmt.Errorf("manager limits exceed supported bounds")
	}
	if (p.AllowCreateTypes && p.MaxCreatedTypes == 0) || (p.AllowCreateSkills && p.MaxCreatedSkills == 0) || (p.AllowCreateVersions && p.MaxVersionsPerEntry == 0) {
		return fmt.Errorf("enabled manager creation requires a positive limit")
	}
	return nil
}

// AgentManagerConfiguration is immutable desired policy and controller history.
// The project is the stable manager identity; native ownership is separate.
type AgentManagerConfiguration struct {
	ProjectID      ProjectID              `json:"projectId"`
	Number         int64                  `json:"number"`
	Definition     AgentManagerDefinition `json:"definition"`
	ControllerType WorkerDefinitionRef    `json:"controllerType"`
	Actor          AdaptiveActor          `json:"actor"`
	Reason         string                 `json:"reason"`
	CreatedAt      time.Time              `json:"createdAt"`
	ContentHash    string                 `json:"contentHash"`
}

// Hash seals controller references, policy and the user's change provenance.
func (c AgentManagerConfiguration) Hash() string {
	c.ContentHash = ""
	_, hash, err := TaskContent(c)
	if err != nil {
		return ""
	}
	return hash
}

// Validate verifies an inspectable configuration without consulting live policy.
func (c AgentManagerConfiguration) Validate() error {
	if err := c.Definition.Validate(); err != nil {
		return err
	}
	if !registryText(string(c.ProjectID), 200, true) || strings.IndexFunc(string(c.ProjectID), unicode.IsControl) >= 0 || c.Number < 1 || c.Number > 1000 || c.CreatedAt.IsZero() || c.ContentHash != c.Hash() || len(c.ContentHash) != 64 || c.ControllerType.ID != c.Definition.AgentTypeID || c.ControllerType.Version != c.Definition.AgentTypeVersion || len(c.ControllerType.ContentHash) != 64 || !registryText(c.ControllerType.Name, 120, true) {
		return fmt.Errorf("manager configuration provenance is invalid")
	}
	if c.Actor.Kind != "USER" || c.Actor.SessionID != "" {
		return fmt.Errorf("manager governance must be user-authored")
	}
	if err := (TaskMutation{Actor: c.Actor, Reason: c.Reason}).ValidatePlanning(); err != nil {
		return err
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(encoded) > 64<<10 {
		return fmt.Errorf("manager configuration exceeds 64 KiB")
	}
	return nil
}

// AgentManagerAudit is a durable policy action independent of CDC retention.
type AgentManagerAudit struct {
	Sequence             int64         `json:"sequence"`
	ProjectID            ProjectID     `json:"projectId"`
	ConfigurationVersion int64         `json:"configurationVersion"`
	Action               string        `json:"action"`
	Actor                AdaptiveActor `json:"actor"`
	Reason               string        `json:"reason"`
	CreatedAt            time.Time     `json:"createdAt"`
}
