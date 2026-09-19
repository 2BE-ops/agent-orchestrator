package domain

import (
	"encoding/hex"
	"fmt"
	"time"
)

// TaskDelegation retains the exact text delivered for a native execution. The
// artifact is an input receipt, not proof that a native send or launch succeeded.
type TaskDelegation struct {
	SchemaVersion        int          `json:"schemaVersion"`
	AttemptID            string       `json:"attemptId"`
	Number               int64        `json:"number"`
	SessionID            SessionID    `json:"sessionId"`
	ExecutionOperationID string       `json:"executionOperationId"`
	ConfigurationHash    string       `json:"configurationHash"`
	ContextHash          string       `json:"contextHash"`
	MaxContextClass      ContextClass `json:"maxContextClass" enum:"technical,engagement,mission"`
	Classification       ContextClass `json:"classification" enum:"technical,engagement,mission"`
	EngagementID         string       `json:"engagementId,omitempty"`
	SystemPrompt         string       `json:"systemPrompt"`
	Prompt               string       `json:"prompt"`
	CreatedAt            time.Time    `json:"createdAt"`
	ContentHash          string       `json:"contentHash"`
}

// Hash seals content, source context and exact generation attribution together.
func (d TaskDelegation) Hash() string {
	d.ContentHash = ""
	_, hash, err := TaskContent(d)
	if err != nil {
		return ""
	}
	return hash
}

// Validate bounds an inspectable artifact without accepting caller authority.
func (d TaskDelegation) Validate() error {
	if d.SchemaVersion != 1 || d.Number < 1 || d.Number > 1000 || d.CreatedAt.IsZero() {
		return fmt.Errorf("invalid delegation version")
	}
	for _, id := range []string{d.AttemptID, string(d.SessionID), d.ExecutionOperationID} {
		if !messageIdentity(id) {
			return fmt.Errorf("invalid delegation identity")
		}
	}
	for _, hash := range []string{d.ConfigurationHash, d.ContextHash, d.ContentHash} {
		if len(hash) != 64 {
			return fmt.Errorf("invalid delegation provenance hash")
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return fmt.Errorf("invalid delegation provenance hash")
		}
	}
	if d.MaxContextClass == "" || d.Classification == "" || !CanEmbedContext(d.MaxContextClass, d.EngagementID, d.Classification, d.EngagementID) {
		return fmt.Errorf("delegation exceeds classified context boundary")
	}
	if d.Prompt == "" || len(d.SystemPrompt)+len(d.Prompt) > 256<<10 || d.ContentHash != d.Hash() {
		return fmt.Errorf("invalid delegation content or hash")
	}
	return nil
}
