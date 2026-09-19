package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// AgentManagerAssessment contains trusted deterministic observations. Public
// proposal JSON cannot supply these observations or their compatibility verdict.
type AgentManagerAssessment struct {
	ProjectID                ProjectID
	RequestID                string
	ProposalID               string
	ProjectConfigurationHash string
	Candidates               []AgentManagerCandidate
	ObservedAt               time.Time
}

// Validate bounds a complete selected-first assessment of at most 33 versions.
func (a AgentManagerAssessment) Validate() error {
	if !messageIdentity(string(a.ProjectID)) || !messageIdentity(a.RequestID) || !messageIdentity(a.ProposalID) || !validArtifactHash(a.ProjectConfigurationHash) || a.ObservedAt.IsZero() || len(a.Candidates) < 1 || len(a.Candidates) > 33 {
		return fmt.Errorf("invalid Manager assessment identity, bounds or observation")
	}
	seen := map[SkillVersionRef]bool{}
	for _, candidate := range a.Candidates {
		key := SkillVersionRef{ID: candidate.AgentType.ID, Version: candidate.AgentType.Version}
		if seen[key] {
			return fmt.Errorf("duplicate assessed candidate")
		}
		seen[key] = true
		if err := candidate.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// AgentManagerDecision seals a semantic proposal together with deterministic
// candidate checks. Accepted means routing selected a configuration, not launched
// or completed. Its narrative inherits the native proposal's conversation class.
type AgentManagerDecision struct {
	SchemaVersion            int                     `json:"schemaVersion"`
	ProjectID                ProjectID               `json:"projectId"`
	RequestID                string                  `json:"requestId"`
	ProposalID               string                  `json:"proposalId"`
	RequestHash              string                  `json:"requestHash"`
	ProposalHash             string                  `json:"proposalHash"`
	ConfigurationHash        string                  `json:"configurationHash"`
	ProjectConfigurationHash string                  `json:"projectConfigurationHash"`
	Optimization             string                  `json:"optimization" enum:"quality,balanced,speed,usage"`
	Classification           ContextClass            `json:"classification" enum:"technical,engagement,mission"`
	EngagementID             string                  `json:"engagementId,omitempty"`
	Candidates               []AgentManagerCandidate `json:"candidates"`
	Outcome                  string                  `json:"outcome" enum:"accepted,rejected"`
	ObservedAt               time.Time               `json:"observedAt"`
	CreatedAt                time.Time               `json:"createdAt"`
	ContentHash              string                  `json:"contentHash"`
}

// Hash includes every retained decision fact except the hash itself.
func (d AgentManagerDecision) Hash() string {
	d.ContentHash = ""
	_, hash, _ := TaskContent(d)
	return hash
}

// Validate checks history without consulting current registry or native state.
func (d AgentManagerDecision) Validate() error {
	if err := (AgentManagerAssessment{ProjectID: d.ProjectID, RequestID: d.RequestID, ProposalID: d.ProposalID, ProjectConfigurationHash: d.ProjectConfigurationHash, Candidates: d.Candidates, ObservedAt: d.ObservedAt}).Validate(); err != nil {
		return err
	}
	if d.SchemaVersion != 1 || !validArtifactHash(d.RequestHash) || !validArtifactHash(d.ProposalHash) || !validArtifactHash(d.ConfigurationHash) || !validArtifactHash(d.ContentHash) || d.ContentHash != d.Hash() || d.CreatedAt.Before(d.ObservedAt) || d.Classification == "" || ValidateContextScope(d.Classification, d.EngagementID) != nil {
		return fmt.Errorf("invalid sealed Manager decision")
	}
	if (d.Outcome != "accepted" && d.Outcome != "rejected") || (d.Outcome == "accepted") != d.Candidates[0].Eligible {
		return fmt.Errorf("decision must reflect its selected candidate")
	}
	if d.Optimization != "quality" && d.Optimization != "balanced" && d.Optimization != "speed" && d.Optimization != "usage" {
		return fmt.Errorf("invalid decision preference")
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(encoded) > 16<<20 {
		return fmt.Errorf("decision exceeds its 16 MiB history bound")
	}
	return nil
}
