package domain

import (
	"strings"
	"testing"
)

func validTaskResult() TaskResultDefinition {
	return TaskResultDefinition{SchemaVersion: 1, ClaimedOutcome: "completed", ClaimedCommit: strings.Repeat("a", 40), Summary: "Implemented the requested interface", Implementation: "Added bounded behavior", Tests: []TaskTestClaim{{Command: []string{"go", "test", "./..."}, Outcome: "passed", Details: "Worker report, not objective verification"}}, Interfaces: []TaskInterfaceClaim{{Name: "API", Contract: "Returns structured results", Files: []string{"src/api.go"}}}}
}

func TestTaskResultSchemaBoundsAndClaims(t *testing.T) {
	if err := validTaskResult().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*TaskResultDefinition)
	}{
		{"schema", func(d *TaskResultDefinition) { d.SchemaVersion = 2 }},
		{"outcome", func(d *TaskResultDefinition) { d.ClaimedOutcome = "verified" }},
		{"summary", func(d *TaskResultDefinition) { d.Summary = " " }},
		{"nul", func(d *TaskResultDefinition) { d.Implementation = "nul\x00" }},
		{"short_commit", func(d *TaskResultDefinition) { d.ClaimedCommit = "main" }},
		{"bad_commit", func(d *TaskResultDefinition) { d.ClaimedCommit = strings.Repeat("z", 40) }},
		{"claim_count", func(d *TaskResultDefinition) { d.Findings = make([]string, 33) }},
		{"claim_length", func(d *TaskResultDefinition) { d.Findings = []string{strings.Repeat("x", 2001)} }},
		{"escape", func(d *TaskResultDefinition) { d.Interfaces[0].Files = []string{"../secret"} }},
		{"duplicate_file", func(d *TaskResultDefinition) { d.Interfaces[0].Files = []string{"src/api.go", "src/api.go"} }},
		{"tests", func(d *TaskResultDefinition) { d.Tests[0].Outcome = "verified" }},
		{"command", func(d *TaskResultDefinition) { d.Tests[0].Command = []string{""} }},
		{"knowledge", func(d *TaskResultDefinition) {
			d.KnowledgeCandidates = []TaskKnowledgeCandidate{{Title: "Claim", Kind: "fake", Content: "Proposed fact", Confidence: "high"}}
		}},
		{"total_bytes", func(d *TaskResultDefinition) {
			claims := make([]string, 32)
			for i := range claims {
				claims[i] = strings.Repeat("x", 2000)
			}
			d.Decisions, d.Assumptions, d.Findings, d.UnresolvedIssues, d.RecommendedFollowUp = claims, claims, claims, claims, claims
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := validTaskResult()
			tc.change(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
}

func TestTaskResultSubmissionRequiresBoundedIdempotencyAndVersion(t *testing.T) {
	input := TaskResultSubmission{ID: "result", AttemptID: "attempt", SessionID: "session", IdempotencyKey: "submission-1", Definition: validTaskResult()}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*TaskResultSubmission){
		func(s *TaskResultSubmission) { s.IdempotencyKey = "" },
		func(s *TaskResultSubmission) { s.IdempotencyKey = "line\nbreak" },
		func(s *TaskResultSubmission) { s.ExpectedVersion = 16 },
		func(s *TaskResultSubmission) { s.ExpectedActivation = -1 },
	} {
		changed := input
		mutate(&changed)
		if err := changed.Validate(); err == nil {
			t.Fatal("unbounded submission accepted")
		}
	}
}
