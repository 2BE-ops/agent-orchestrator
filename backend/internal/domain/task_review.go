package domain

import "fmt"

// TaskReviewPolicy pins reviewer identity before an attempt starts. It is part
// of the immutable acceptance criteria, never a worker-supplied assertion.
type TaskReviewPolicy struct {
	AgentTypeID        string `json:"agentTypeId"`
	Version            int64  `json:"version"`
	DifferentAgentType bool   `json:"differentAgentType"`
	DifferentHarness   bool   `json:"differentHarness"`
}

// Validate requires an exact historical version, not a floating active alias.
func (p TaskReviewPolicy) Validate() error {
	if !resultText(p.AgentTypeID, 200, true) || p.Version < 1 {
		return fmt.Errorf("review policy requires an Agent Type and exact positive version")
	}
	return nil
}

// Allows compares observed configurations, not reported roles or display names.
// A different version of the same Type is not a different Type identity.
func (p TaskReviewPolicy) Allows(implementer, reviewer WorkerDefinitionRef, implementingHarness, reviewingHarness AgentHarness) bool {
	if p.Validate() != nil || reviewer.ID != p.AgentTypeID || reviewer.Version != p.Version || implementer.ID == "" || implementer.Version < 1 || !implementingHarness.IsKnown() || !reviewingHarness.IsKnown() {
		return false
	}
	if p.DifferentAgentType && implementer.ID == reviewer.ID {
		return false
	}
	return !p.DifferentHarness || implementingHarness != reviewingHarness
}
