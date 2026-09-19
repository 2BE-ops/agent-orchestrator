package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// AgentManagerRegistryAction is authorable content, never an actor, ownership
// policy, activation instruction or claimed compatibility verdict.
type AgentManagerRegistryAction struct {
	Action           string             `json:"action" enum:"create,append_version"`
	Kind             RegistryKind       `json:"kind" enum:"agent_type,skill"`
	Name             string             `json:"name,omitempty"`
	Description      string             `json:"description,omitempty"`
	EntryID          string             `json:"entryId,omitempty"`
	ExpectedRevision int64              `json:"expectedRevision,omitempty"`
	Definition       RegistryDefinition `json:"definition"`
	Reason           string             `json:"reason"`
}

// Validate shares normal registry authoring validation and bounds native output.
func (a AgentManagerRegistryAction) Validate() error {
	if err := a.Definition.Validate(a.Kind); err != nil {
		return err
	}
	if err := (RegistryMutation{Actor: RegistryActor{Origin: RegistryManager, ID: "manager"}, Reason: a.Reason}).Validate(); err != nil {
		return err
	}
	switch a.Action {
	case "create":
		if a.EntryID != "" || a.ExpectedRevision != 0 {
			return fmt.Errorf("creation cannot claim an existing identity or revision")
		}
		if err := (RegistryMetadata{Name: a.Name, Description: a.Description}).Validate(); err != nil {
			return err
		}
	case "append_version":
		if !managerRegistryID(a.EntryID) || a.ExpectedRevision < 1 || a.Name != "" || a.Description != "" {
			return fmt.Errorf("version creation requires an exact identity and revision without metadata edits")
		}
	default:
		return fmt.Errorf("unsupported Manager registry action")
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if len(encoded) > 256<<10 {
		return fmt.Errorf("manager registry action exceeds 256 KiB")
	}
	return nil
}

func managerRegistryID(value string) bool {
	return registryText(value, 200, true) && strings.IndexFunc(value, unicode.IsControl) < 0
}

// AgentManagerRegistrySubmission carries trusted session facts and service IDs.
// It is deliberately not an HTTP input; native callers cannot supply authority.
type AgentManagerRegistrySubmission struct {
	ID, CreatedEntryID string
	ProjectID          ProjectID
	RequestID          string
	SessionID          SessionID
	SourceOwner        SessionControllerOwner
	IdempotencyKey     string
	Action             AgentManagerRegistryAction
	Now                time.Time
}

// Validate rejects invalid submissions before any mutation or quota charge.
func (s AgentManagerRegistrySubmission) Validate() error {
	for _, id := range []string{s.ID, string(s.ProjectID), s.RequestID, string(s.SessionID), s.IdempotencyKey} {
		if !managerRegistryID(id) {
			return fmt.Errorf("manager registry submission requires bounded identities")
		}
	}
	if s.Now.IsZero() || (s.Action.Action == "create" && !managerRegistryID(s.CreatedEntryID)) || (s.Action.Action != "create" && s.CreatedEntryID != "") {
		return fmt.Errorf("manager registry submission requires a timestamp and creation identity only for creation")
	}
	return s.Action.Validate()
}

// AgentManagerRegistryReceipt seals one applied native action and its exact
// version. Replaying it never activates, rewrites or creates another version.
type AgentManagerRegistryReceipt struct {
	SchemaVersion           int                        `json:"schemaVersion"`
	ID                      string                     `json:"id"`
	ProjectID               ProjectID                  `json:"projectId"`
	RequestID               string                     `json:"requestId"`
	RequestHash             string                     `json:"requestHash"`
	ConfigurationHash       string                     `json:"configurationHash"`
	ControllerID            string                     `json:"controllerId"`
	SessionID               SessionID                  `json:"sessionId"`
	NativeGeneration        string                     `json:"nativeGeneration"`
	WorkerConfigurationHash string                     `json:"workerConfigurationHash"`
	ContextID               string                     `json:"contextId"`
	ContextHash             string                     `json:"contextHash"`
	ConversationContextHash string                     `json:"conversationContextHash"`
	Classification          ContextClass               `json:"classification" enum:"technical,engagement,mission"`
	EngagementID            string                     `json:"engagementId,omitempty"`
	Action                  AgentManagerRegistryAction `json:"action"`
	Target                  WorkerDefinitionRef        `json:"target"`
	MetadataRevision        int64                      `json:"metadataRevision"`
	CreatedAt               time.Time                  `json:"createdAt"`
	ContentHash             string                     `json:"contentHash"`
}

// Hash seals content and native provenance independently of mutable registry data.
func (r AgentManagerRegistryReceipt) Hash() string {
	r.ContentHash = ""
	_, hash, err := TaskContent(r)
	if err != nil {
		return ""
	}
	return hash
}

// Validate never infers declassification from a Type's receiving clearance.
// Reusable registry instructions are technical; classified native conversations
// must retain recommendations for review instead of publishing reusable text.
func (r AgentManagerRegistryReceipt) Validate() error {
	if err := r.Action.Validate(); err != nil {
		return err
	}
	for _, id := range []string{r.ID, string(r.ProjectID), r.RequestID, r.ControllerID, string(r.SessionID), r.NativeGeneration, r.ContextID, r.Target.ID} {
		if !managerRegistryID(id) {
			return fmt.Errorf("manager registry receipt has invalid provenance")
		}
	}
	for _, hash := range []string{r.RequestHash, r.ConfigurationHash, r.WorkerConfigurationHash, r.ContextHash, r.ConversationContextHash, r.Target.ContentHash, r.ContentHash} {
		if len(hash) != 64 {
			return fmt.Errorf("manager registry receipt requires sealed references")
		}
	}
	if r.SchemaVersion != 1 || r.Classification != ContextTechnical || r.CreatedAt.IsZero() || r.MetadataRevision < 1 || r.Target.Version < 1 || !registryText(r.Target.Name, 120, true) || r.ContentHash != r.Hash() {
		return fmt.Errorf("manager registry receipt is invalid")
	}
	if err := ValidateContextScope(r.Classification, r.EngagementID); err != nil {
		return err
	}
	if r.Action.Action == "create" && (r.Target.Version != 1 || r.MetadataRevision != 1 || r.Target.Name != r.Action.Name) {
		return fmt.Errorf("manager creation receipt must identify the first version")
	}
	if r.Action.Action == "append_version" && (r.Target.ID != r.Action.EntryID || r.Target.Version < 2 || r.MetadataRevision != r.Action.ExpectedRevision+1) {
		return fmt.Errorf("manager version receipt must identify the appended revision")
	}
	_, expectedHash, err := r.Action.Definition.MarshalContent(r.Action.Kind)
	if err != nil || r.Target.ContentHash != expectedHash {
		return fmt.Errorf("manager registry target differs from submitted definition")
	}
	return nil
}
