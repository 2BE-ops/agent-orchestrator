package domain

import "fmt"

// AgentManagerCandidateIssue records a deterministic exclusion. Codes and states
// are application facts; native free-form diagnostics are not routing context.
type AgentManagerCandidateIssue struct {
	Code  string `json:"code"`
	State string `json:"state" enum:"invalid,unavailable"`
}

// AgentManagerCandidateSkill pins both content and mutable selection policy.
type AgentManagerCandidateSkill struct {
	Reference        WorkerDefinitionRef `json:"reference"`
	MetadataRevision int64               `json:"metadataRevision"`
}

// AgentManagerCandidate is a point-in-time compatibility observation, never a
// launch authorization or semantic ranking. Applying a decision rechecks its pins.
type AgentManagerCandidate struct {
	AgentType           WorkerDefinitionRef          `json:"agentType"`
	MetadataRevision    int64                        `json:"metadataRevision"`
	MaxContextClass     ContextClass                 `json:"maxContextClass" enum:"technical,engagement,mission"`
	Harness             AgentHarness                 `json:"harness"`
	SessionMode         SessionMode                  `json:"sessionMode"`
	Config              AgentConfig                  `json:"config"`
	ProviderBindingID   string                       `json:"providerBindingId,omitempty"`
	BindingRevision     int64                        `json:"bindingRevision,omitempty"`
	Capabilities        []string                     `json:"capabilities"`
	Skills              []AgentManagerCandidateSkill `json:"skills"`
	MissingCapabilities []string                     `json:"missingCapabilities"`
	Eligible            bool                         `json:"eligible"`
	Issues              []AgentManagerCandidateIssue `json:"issues"`
	CatalogFingerprint  string                       `json:"catalogFingerprint,omitempty"`
}

// Validate bounds persisted compatibility observations. Missing/disabled entries
// may lack native facts; eligible entries must retain a complete exact choice.
func (c AgentManagerCandidate) Validate() error {
	if !messageIdentity(c.AgentType.ID) || c.AgentType.Version < 1 || c.MetadataRevision < 0 || c.BindingRevision < 0 || !registryText(c.AgentType.Name, 120, false) || (c.AgentType.ContentHash != "" && !validArtifactHash(c.AgentType.ContentHash)) || c.MaxContextClass == "" || c.MaxContextClass.Validate() != nil || len(c.Capabilities) > 2112 || len(c.MissingCapabilities) > 32 || len(c.Skills) > 32 || len(c.Issues) > 128 || c.Issues == nil || c.Skills == nil || c.Capabilities == nil || c.MissingCapabilities == nil {
		return fmt.Errorf("invalid Manager candidate observation")
	}
	if err := c.Config.Validate(); err != nil {
		return err
	}
	if !registryText(c.ProviderBindingID, 200, false) || !registryText(c.CatalogFingerprint, 2000, false) {
		return fmt.Errorf("invalid candidate native reference")
	}
	seen := map[string]bool{}
	for _, capability := range c.Capabilities {
		if !registryText(capability, 120, true) || seen[capability] {
			return fmt.Errorf("invalid candidate capability")
		}
		seen[capability] = true
	}
	if err := registryTags(c.MissingCapabilities); err != nil {
		return err
	}
	seen = map[string]bool{}
	for _, skill := range c.Skills {
		ref := skill.Reference
		if !messageIdentity(ref.ID) || ref.Version < 1 || !validArtifactHash(ref.ContentHash) || skill.MetadataRevision < 1 || !registryText(ref.Name, 120, true) || seen[ref.ID] {
			return fmt.Errorf("invalid candidate Skill pin")
		}
		seen[ref.ID] = true
	}
	for _, issue := range c.Issues {
		if !messageIdentity(issue.Code) || (issue.State != "invalid" && issue.State != "unavailable") {
			return fmt.Errorf("invalid candidate exclusion")
		}
	}
	if c.Eligible {
		if len(c.Issues) != 0 || len(c.MissingCapabilities) != 0 || c.MetadataRevision < 1 || !validArtifactHash(c.AgentType.ContentHash) || !c.Harness.IsKnown() || !c.SessionMode.Valid() || !registryText(c.AgentType.Name, 120, true) {
			return fmt.Errorf("eligible candidate lacks verified configuration")
		}
	} else if len(c.Issues) == 0 {
		return fmt.Errorf("rejected candidate requires an exclusion")
	}
	return nil
}
