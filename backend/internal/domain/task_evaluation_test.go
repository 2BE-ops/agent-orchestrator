package domain_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestTaskEvaluationRequiresExactCompleteCheckSnapshot(t *testing.T) {
	now := time.Now().UTC()
	commit := strings.Repeat("a", 40)
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "ci", Requirement: "CI passes", EvidenceKind: "ci", CheckNames: []string{"test"}}}}
	for _, tc := range []struct {
		name   string
		mutate func(*domain.TaskCICheckEvidence)
		want   string
	}{
		{"passed", func(*domain.TaskCICheckEvidence) {}, "passed"},
		{"failed", func(c *domain.TaskCICheckEvidence) { c.Status = domain.PRCheckFailed; c.Conclusion = "failure" }, "failed"},
		{"pending", func(c *domain.TaskCICheckEvidence) { c.Status = domain.PRCheckInProgress }, "inconclusive"},
		{"cancelled", func(c *domain.TaskCICheckEvidence) { c.Status = domain.PRCheckCancelled }, "inconclusive"},
		{"skipped", func(c *domain.TaskCICheckEvidence) { c.Status = domain.PRCheckSkipped }, "inconclusive"},
		{"unknown", func(c *domain.TaskCICheckEvidence) { c.Status = domain.PRCheckUnknown }, "inconclusive"},
		{"contradictory_status", func(c *domain.TaskCICheckEvidence) { c.Conclusion = "failure" }, "inconclusive"},
		{"wrong_name", func(c *domain.TaskCICheckEvidence) { c.Name = "different" }, "inconclusive"},
		{"old_commit", func(c *domain.TaskCICheckEvidence) { c.TargetCommit = strings.Repeat("b", 40) }, "inconclusive"},
		{"old_head", func(c *domain.TaskCICheckEvidence) { c.HeadCommit = strings.Repeat("b", 40) }, "inconclusive"},
		{"absent_from_snapshot", func(c *domain.TaskCICheckEvidence) { c.ObservedAt = now.Add(-time.Minute) }, "inconclusive"},
		{"future", func(c *domain.TaskCICheckEvidence) { c.ObservedAt = now.Add(time.Minute) }, "inconclusive"},
		{"unobserved", func(c *domain.TaskCICheckEvidence) { c.ObservedAt = time.Time{} }, "inconclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := domain.TaskCICheckEvidence{PRURL: "https://example.test/pr/1", HeadCommit: commit, TargetCommit: commit, Name: "test", Status: domain.PRCheckPassed, Conclusion: "success", ObservedAt: now, SnapshotAt: now}
			tc.mutate(&check)
			decisions, outcome, _ := domain.EvaluateTaskCriteria(criteria, commit, []domain.TaskCICheckEvidence{check}, false, now)
			if outcome != tc.want || len(decisions) != 1 || decisions[0].Outcome != tc.want {
				t.Fatalf("unsupported outcome: %+v %s", decisions, outcome)
			}
			if _, outcome, _ := domain.EvaluateTaskCriteria(criteria, commit, []domain.TaskCICheckEvidence{check}, true, now); outcome == "passed" {
				t.Fatal("truncated evidence passed")
			}
		})
	}
	criteria.Criteria = append(criteria.Criteria, domain.AcceptanceCriterion{ID: "review", Requirement: "Independent review", EvidenceKind: "review"})
	if _, outcome, _ := domain.EvaluateTaskCriteria(criteria, commit, nil, false, now); outcome != "inconclusive" {
		t.Fatal("absent review accepted")
	}
}

func TestTaskCICriteriaPreserveLegacyHashesAndRejectAmbiguousSelectors(t *testing.T) {
	legacy := `{"criteria":[{"id":"test","requirement":"Run tests","evidenceKind":"test","command":["go","test","./..."]}]}`
	var criteria domain.AcceptanceCriteria
	if err := json.Unmarshal([]byte(legacy), &criteria); err != nil {
		t.Fatal(err)
	}
	encoded, hash, err := domain.TaskContent(criteria)
	if err != nil || string(encoded) != legacy || hash != domain.ContextTextHash(legacy) {
		t.Fatalf("legacy criteria hash changed: %s %v", encoded, err)
	}
	for _, criterion := range []domain.AcceptanceCriterion{
		{ID: "ci", Requirement: "Checks", EvidenceKind: "ci"},
		{ID: "ci", Requirement: "Checks", EvidenceKind: "ci", CheckNames: []string{"test", "test"}},
		{ID: "ci", Requirement: "Checks", EvidenceKind: "ci", CheckNames: []string{"test"}, Command: []string{"run"}},
		{ID: "ci", Requirement: "Checks", EvidenceKind: "review", CheckNames: []string{"test"}},
	} {
		if err := (domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{criterion}}).Validate(); err == nil {
			t.Fatalf("ambiguous criterion accepted: %+v", criterion)
		}
	}
}

func TestBuildTestAndLintUseIndependentNamedChecks(t *testing.T) {
	now := time.Now().UTC()
	commit := strings.Repeat("a", 40)
	for _, kind := range []string{"test", "build", "lint"} {
		t.Run(kind, func(t *testing.T) {
			criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: kind, Requirement: "Named verification succeeds", EvidenceKind: kind, CheckNames: []string{"required-" + kind}}}}
			if err := criteria.Validate(); err != nil {
				t.Fatal(err)
			}
			check := domain.TaskCICheckEvidence{PRURL: "https://example.test/pr/1", HeadCommit: commit, TargetCommit: commit, Name: "required-" + kind, Status: domain.PRCheckPassed, Conclusion: "success", ObservedAt: now, SnapshotAt: now}
			for _, tc := range []struct {
				status domain.PRCheckStatus
				want   string
			}{{domain.PRCheckPassed, "passed"}, {domain.PRCheckFailed, "failed"}, {domain.PRCheckInProgress, "inconclusive"}, {domain.PRCheckSkipped, "inconclusive"}} {
				check.Status = tc.status
				_, outcome, _ := domain.EvaluateTaskCriteria(criteria, commit, []domain.TaskCICheckEvidence{check}, false, now)
				if outcome != tc.want {
					t.Fatalf("%s: %s want %s", tc.status, outcome, tc.want)
				}
			}
			criteria.Criteria[0].Command = []string{"echo", "passed"}
			if err := criteria.Validate(); err == nil {
				t.Fatal("ambiguous command/check selector accepted")
			}
			criteria.Criteria[0].CheckNames = nil
			if err := criteria.Validate(); err != nil {
				t.Fatalf("legacy command criteria invalidated: %v", err)
			}
			check.Status = domain.PRCheckPassed
			if _, outcome, _ := domain.EvaluateTaskCriteria(criteria, commit, []domain.TaskCICheckEvidence{check}, false, now); outcome == "passed" {
				t.Fatal("unnamed check satisfied legacy command")
			}
		})
	}
}
