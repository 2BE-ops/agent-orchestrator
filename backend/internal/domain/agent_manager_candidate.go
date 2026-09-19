package domain

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
