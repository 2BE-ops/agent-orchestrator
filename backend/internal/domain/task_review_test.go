package domain

import (
	"strings"
	"testing"
)

func TestTaskReviewPolicyExactIdentityAndIndependence(t *testing.T) {
	policy := TaskReviewPolicy{AgentTypeID: "reviewer", Version: 3, DifferentAgentType: true, DifferentHarness: true}
	for _, tc := range []struct {
		name                    string
		implementer, reviewer   WorkerDefinitionRef
		implementing, reviewing AgentHarness
		allowed                 bool
	}{
		{"independent", WorkerDefinitionRef{ID: "implementer", Version: 1}, WorkerDefinitionRef{ID: "reviewer", Version: 3}, HarnessCodex, HarnessClaudeCode, true},
		{"wrong version", WorkerDefinitionRef{ID: "implementer", Version: 1}, WorkerDefinitionRef{ID: "reviewer", Version: 4}, HarnessCodex, HarnessClaudeCode, false},
		{"wrong type", WorkerDefinitionRef{ID: "implementer", Version: 1}, WorkerDefinitionRef{ID: "other", Version: 3}, HarnessCodex, HarnessClaudeCode, false},
		{"same identity different version", WorkerDefinitionRef{ID: "reviewer", Version: 2}, WorkerDefinitionRef{ID: "reviewer", Version: 3}, HarnessCodex, HarnessClaudeCode, false},
		{"same harness", WorkerDefinitionRef{ID: "implementer", Version: 1}, WorkerDefinitionRef{ID: "reviewer", Version: 3}, HarnessCodex, HarnessCodex, false},
		{"unknown implementer", WorkerDefinitionRef{}, WorkerDefinitionRef{ID: "reviewer", Version: 3}, HarnessCodex, HarnessClaudeCode, false},
		{"unknown harness", WorkerDefinitionRef{ID: "implementer", Version: 1}, WorkerDefinitionRef{ID: "reviewer", Version: 3}, "unknown", HarnessClaudeCode, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.Allows(tc.implementer, tc.reviewer, tc.implementing, tc.reviewing); got != tc.allowed {
				t.Fatalf("allowed = %v, want %v", got, tc.allowed)
			}
		})
	}
	policy.DifferentAgentType, policy.DifferentHarness = false, false
	ref := WorkerDefinitionRef{ID: "reviewer", Version: 3}
	if !policy.Allows(ref, ref, HarnessCodex, HarnessCodex) {
		t.Fatal("optional independence dimensions were made mandatory")
	}
}

func TestTaskReviewPolicyValidationAndLegacyHash(t *testing.T) {
	criteria := AcceptanceCriteria{Criteria: []AcceptanceCriterion{{ID: "review", Requirement: "No blocking findings", EvidenceKind: "review"}}}
	content, hash, err := TaskContent(criteria)
	if err != nil || string(content) != `{"criteria":[{"id":"review","requirement":"No blocking findings","evidenceKind":"review"}]}` {
		t.Fatalf("legacy representation changed: %s %v", content, err)
	}
	for _, policy := range []TaskReviewPolicy{{}, {AgentTypeID: "reviewer"}, {AgentTypeID: "reviewer", Version: -1}, {AgentTypeID: "bad\x00id", Version: 1}, {AgentTypeID: strings.Repeat("x", 201), Version: 1}} {
		criteria.ReviewPolicy = &policy
		if criteria.Validate() == nil {
			t.Fatalf("accepted invalid policy: %+v", policy)
		}
	}
	criteria.ReviewPolicy = &TaskReviewPolicy{AgentTypeID: "reviewer", Version: 1, DifferentAgentType: true}
	if err := criteria.Validate(); err != nil {
		t.Fatal(err)
	}
	_, changedHash, _ := TaskContent(criteria)
	if hash == changedHash {
		t.Fatal("review policy not covered by criteria hash")
	}
	criteria.Criteria[0].EvidenceKind = "manual"
	if criteria.Validate() == nil {
		t.Fatal("accepted orphan review policy")
	}
}
