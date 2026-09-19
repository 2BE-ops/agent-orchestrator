package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

// AgentManagerCandidateReason is the native Manager's explanation, not proof of
// compatibility or permission. The deterministic selection service checks those.
type AgentManagerCandidateReason struct {
	AgentTypeID string `json:"agentTypeId"`
	Version     int64  `json:"version"`
	Reason      string `json:"reason"`
}

// AgentManagerProposalDefinition is a versioned semantic routing proposal.
// Composition/evolution use policy-checked registry tools; their resulting exact
// definitions can then be proposed here. No proposal itself authorizes execution.
type AgentManagerProposalDefinition struct {
	SchemaVersion    int                           `json:"schemaVersion"`
	Action           string                        `json:"action" enum:"select_existing,needs_human"`
	AgentTypeID      string                        `json:"agentTypeId,omitempty"`
	AgentTypeVersion int64                         `json:"agentTypeVersion,omitempty"`
	Rationale        string                        `json:"rationale"`
	Candidates       []AgentManagerCandidateReason `json:"candidates"`
}

// Validate bounds semantic explanations and requires an exact selected version.
func (d AgentManagerProposalDefinition) Validate() error {
	if d.SchemaVersion != 1 || !resultText(d.Rationale, 8000, true) || d.Candidates == nil || len(d.Candidates) > 32 {
		return fmt.Errorf("proposal requires schema 1, a rationale and at most 32 candidate explanations")
	}
	switch d.Action {
	case "select_existing":
		if !messageIdentity(d.AgentTypeID) || d.AgentTypeVersion < 1 {
			return fmt.Errorf("selection requires an exact Agent Type version")
		}
	case "needs_human":
		if d.AgentTypeID != "" || d.AgentTypeVersion != 0 {
			return fmt.Errorf("human escalation cannot also select a worker")
		}
	default:
		return fmt.Errorf("unknown Manager proposal action")
	}
	seen := map[struct {
		id      string
		version int64
	}]bool{}
	for _, candidate := range d.Candidates {
		key := struct {
			id      string
			version int64
		}{candidate.AgentTypeID, candidate.Version}
		if !messageIdentity(candidate.AgentTypeID) || candidate.Version < 1 || !resultText(candidate.Reason, 1000, true) || seen[key] {
			return fmt.Errorf("invalid or duplicate candidate explanation")
		}
		seen[key] = true
	}
	return nil
}

// ParseAgentManagerProposal retains malformed output as a rejected proposal at
// the storage boundary; it does not heuristically extract JSON from prose.
func ParseAgentManagerProposal(raw string) (*AgentManagerProposalDefinition, error) {
	if raw == "" || len(raw) > 64<<10 || !utf8.ValidString(raw) {
		return nil, fmt.Errorf("proposal must be 1 to 65536 bytes of UTF-8 JSON")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var definition AgentManagerProposalDefinition
	if err := decoder.Decode(&definition); err != nil {
		return nil, fmt.Errorf("proposal must be one schema-v1 JSON object without unknown fields")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("proposal has trailing content")
	}
	if err := definition.Validate(); err != nil {
		return nil, err
	}
	return &definition, nil
}

// AgentManagerProposal is immutable native output and parser outcome. A parsed
// selection remains unapplied until separately checked against current policy.
type AgentManagerProposal struct {
	ID                string                          `json:"id"`
	RequestID         string                          `json:"requestId"`
	Number            int64                           `json:"number"`
	ControllerID      string                          `json:"controllerId"`
	SessionID         SessionID                       `json:"sessionId"`
	NativeGeneration  string                          `json:"nativeGeneration"`
	ConfigurationHash string                          `json:"configurationHash"`
	RequestHash       string                          `json:"requestHash"`
	Raw               string                          `json:"raw"`
	Definition        *AgentManagerProposalDefinition `json:"definition"`
	ValidationError   string                          `json:"validationError,omitempty"`
	CreatedAt         time.Time                       `json:"createdAt"`
	ContentHash       string                          `json:"contentHash"`
}

// Hash seals exact output, its native attribution and historical parser result.
func (p AgentManagerProposal) Hash() string {
	p.ContentHash = ""
	_, hash, _ := TaskContent(p)
	return hash
}

// Validate checks retained history without reparsing under newer parser rules.
func (p AgentManagerProposal) Validate() error {
	for _, id := range []string{p.ID, p.RequestID, p.ControllerID, string(p.SessionID), p.NativeGeneration} {
		if !messageIdentity(id) {
			return fmt.Errorf("invalid Manager proposal identity")
		}
	}
	if p.Number < 1 || p.Number > 5 || p.CreatedAt.IsZero() || len(p.Raw) > 64<<10 || !utf8.ValidString(p.Raw) || !validArtifactHash(p.ConfigurationHash) || !validArtifactHash(p.RequestHash) || !validArtifactHash(p.ContentHash) || p.ContentHash != p.Hash() {
		return fmt.Errorf("invalid sealed Manager proposal")
	}
	if p.Definition == nil {
		if !resultText(p.ValidationError, 2000, true) {
			return fmt.Errorf("malformed proposal requires its parser rejection")
		}
	} else {
		if p.ValidationError != "" {
			return fmt.Errorf("parsed proposal cannot also have a parser rejection")
		}
		return p.Definition.Validate()
	}
	return nil
}

// AgentManagerProposalSubmission carries trusted native owner context. Public
// JSON must never provide SourceOwner, controller identity or policy attribution.
type AgentManagerProposalSubmission struct {
	ID             string
	ProjectID      ProjectID
	RequestID      string
	IdempotencyKey string
	SessionID      SessionID
	SourceOwner    SessionControllerOwner
	Raw            string
	Now            time.Time
}

// Validate bounds output before attribution and parser validation occur in storage.
func (p AgentManagerProposalSubmission) Validate() error {
	for _, id := range []string{p.ID, string(p.ProjectID), p.RequestID, p.IdempotencyKey, string(p.SessionID)} {
		if !messageIdentity(id) {
			return fmt.Errorf("invalid native Manager proposal identity")
		}
	}
	if p.Now.IsZero() || len(p.Raw) > 64<<10 || !utf8.ValidString(p.Raw) {
		return fmt.Errorf("invalid native Manager output bounds")
	}
	return nil
}
