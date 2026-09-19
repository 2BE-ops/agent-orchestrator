package domain_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestTaskMergeabilityRequiresObservedExactHead(t *testing.T) {
	now := time.Now().UTC()
	commit := strings.Repeat("a", 40)
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "merge", Requirement: "PR is mergeable", EvidenceKind: "mergeability"}}}
	if err := criteria.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*domain.TaskPREvidence)
		want   string
	}{
		{"mergeable", func(*domain.TaskPREvidence) {}, "passed"},
		{"conflicting", func(p *domain.TaskPREvidence) { p.Mergeability = domain.MergeConflicting }, "failed"},
		{"closed", func(p *domain.TaskPREvidence) { p.Closed = true }, "failed"},
		{"unknown", func(p *domain.TaskPREvidence) { p.Mergeability = domain.MergeUnknown }, "inconclusive"},
		{"blocked", func(p *domain.TaskPREvidence) { p.Mergeability = domain.MergeBlocked }, "inconclusive"},
		{"draft", func(p *domain.TaskPREvidence) { p.Draft = true }, "inconclusive"},
		{"old_head", func(p *domain.TaskPREvidence) { p.HeadCommit = strings.Repeat("b", 40) }, "inconclusive"},
		{"unobserved", func(p *domain.TaskPREvidence) { p.ObservedAt = time.Time{} }, "inconclusive"},
		{"future", func(p *domain.TaskPREvidence) { p.ObservedAt = now.Add(time.Minute) }, "inconclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := domain.TaskPREvidence{URL: "https://example.test/pr/1", HeadCommit: commit, Mergeability: domain.MergeMergeable, ObservedAt: now}
			tc.mutate(&pr)
			evidence := &domain.TaskObservationEvidence{PRs: []domain.TaskPREvidence{pr}}
			decisions, outcome, _ := domain.EvaluateTaskEvidence(criteria, commit, nil, false, evidence, now)
			if outcome != tc.want || decisions[0].Outcome != tc.want {
				t.Fatalf("incorrect merge evidence: %+v %s", decisions, outcome)
			}
			evidence.PRsTruncated = true
			if _, outcome, _ := domain.EvaluateTaskEvidence(criteria, commit, nil, false, evidence, now); outcome == "passed" {
				t.Fatal("truncated PR set passed")
			}
		})
	}
	criteria.Criteria[0].Command = []string{"echo", "pass"}
	if err := criteria.Validate(); err == nil {
		t.Fatal("ambiguous merge criterion accepted")
	}
}

func TestTaskEvaluationObservationExtensionPreservesHistoricalHash(t *testing.T) {
	old := `{"schemaVersion":1,"targetCommit":"","criteriaHash":"","resultHash":"","checks":null,"checksTruncated":false,"criteria":null,"outcome":"inconclusive","reason":"No evidence"}`
	var definition domain.TaskEvaluationDefinition
	if err := json.Unmarshal([]byte(old), &definition); err != nil {
		t.Fatal(err)
	}
	encoded, hash, err := domain.TaskContent(definition)
	if err != nil || string(encoded) != old || hash != domain.ContextTextHash(old) {
		t.Fatalf("historical hash changed: %s %v", encoded, err)
	}
}
