package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestTaskReviewEvidenceRequiresExactNativeAttribution(t *testing.T) {
	now := time.Now().UTC()
	commit, hash := strings.Repeat("a", 40), strings.Repeat("b", 64)
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "review", Requirement: "Independent review", EvidenceKind: "review"}}, ReviewPolicy: &domain.TaskReviewPolicy{AgentTypeID: "reviewer", Version: 2, DifferentAgentType: true, DifferentHarness: true}}
	_, criteriaHash, _ := domain.TaskContent(criteria)
	target := domain.TaskReviewTarget{ResultID: "result", ResultHash: hash, CriteriaHash: criteriaHash, ImplementingType: domain.WorkerDefinitionRef{ID: "implementer", Version: 1, Name: "Implementer", ContentHash: hash}, ImplementingHarness: domain.HarnessCodex, ImplementingConfigurationHash: hash}
	for _, tc := range []struct {
		name   string
		change func(*domain.TaskObservationEvidence)
		want   string
	}{
		{"approved", func(*domain.TaskObservationEvidence) {}, "passed"},
		{"generic approval", func(e *domain.TaskObservationEvidence) { e.Reviews[0].Attribution = nil }, "inconclusive"},
		{"no witness", func(e *domain.TaskObservationEvidence) { e.Reviews[0].Attribution.StartedAt = nil }, "inconclusive"},
		{"future witness", func(e *domain.TaskObservationEvidence) {
			future := now.Add(time.Minute)
			e.Reviews[0].Attribution.StartedAt = &future
		}, "inconclusive"},
		{"wrong result", func(e *domain.TaskObservationEvidence) { e.Reviews[0].Attribution.Target.ResultID = "other" }, "inconclusive"},
		{"wrong result hash", func(e *domain.TaskObservationEvidence) {
			e.Reviews[0].Attribution.Target.ResultHash = strings.Repeat("c", 64)
		}, "inconclusive"},
		{"wrong criteria", func(e *domain.TaskObservationEvidence) {
			e.Reviews[0].Attribution.Target.CriteriaHash = strings.Repeat("c", 64)
		}, "inconclusive"},
		{"wrong implementer", func(e *domain.TaskObservationEvidence) {
			e.Reviews[0].Attribution.Target.ImplementingConfigurationHash = strings.Repeat("c", 64)
		}, "inconclusive"},
		{"wrong reviewer version", func(e *domain.TaskObservationEvidence) { e.Reviews[0].Attribution.ReviewerType.Version = 3 }, "inconclusive"},
		{"same harness", func(e *domain.TaskObservationEvidence) { e.Reviews[0].Harness = domain.ReviewerCodex }, "inconclusive"},
		{"same type", func(e *domain.TaskObservationEvidence) { e.Reviews[0].Attribution.ReviewerType.ID = "implementer" }, "inconclusive"},
		{"stale PR", func(e *domain.TaskObservationEvidence) { e.PRs[0].HeadCommit = strings.Repeat("c", 40) }, "inconclusive"},
		{"no PR observation", func(e *domain.TaskObservationEvidence) { e.PRs[0].ObservedAt = time.Time{} }, "inconclusive"},
		{"truncated", func(e *domain.TaskObservationEvidence) { e.ReviewsTruncated = true }, "inconclusive"},
		{"failed native", func(e *domain.TaskObservationEvidence) { e.Reviews[0].Status = domain.ReviewRunFailed }, "inconclusive"},
		{"changes requested", func(e *domain.TaskObservationEvidence) { e.Reviews[0].Verdict = domain.VerdictChangesRequested }, "failed"},
		{"newer running", func(e *domain.TaskObservationEvidence) {
			later := e.Reviews[0]
			later.RunID = "newer"
			later.CreatedAt = now.Add(time.Second)
			later.Status = domain.ReviewRunRunning
			later.Verdict = domain.VerdictNone
			e.Reviews = append(e.Reviews, later)
		}, "inconclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := domain.TaskObservationEvidence{ReviewTarget: &target, PRs: []domain.TaskPREvidence{{URL: "pr", HeadCommit: commit, ObservedAt: now}}, Reviews: []domain.TaskReviewEvidence{{RunID: "review", PRURL: "pr", TargetCommit: commit, Harness: domain.ReviewerClaudeCode, Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved, CreatedAt: now, Attribution: &domain.TaskReviewAttribution{Target: target, ContextHash: hash, ConfigurationHash: hash, ReviewerType: domain.WorkerDefinitionRef{ID: "reviewer", Version: 2, Name: "Reviewer", ContentHash: hash}, StartedAt: &now}}}}
			tc.change(&evidence)
			decisions, outcome, _ := domain.EvaluateTaskEvidence(criteria, commit, nil, false, &evidence, now)
			if outcome != tc.want || decisions[0].Outcome != tc.want {
				t.Fatalf("decision=%+v outcome=%s want=%s", decisions, outcome, tc.want)
			}
			if outcome == "passed" && !strings.Contains(decisions[0].Reason, "qualitative") {
				t.Fatal("review did not identify qualitative evidence")
			}
			withCI := criteria
			withCI.Criteria = append(append([]domain.AcceptanceCriterion{}, criteria.Criteria...), domain.AcceptanceCriterion{ID: "ci", Requirement: "Required test passes", EvidenceKind: "ci", CheckNames: []string{"test"}})
			_, combinedHash, _ := domain.TaskContent(withCI)
			combinedTarget := *evidence.ReviewTarget
			combinedTarget.CriteriaHash = combinedHash
			evidence.ReviewTarget = &combinedTarget
			for i := range evidence.Reviews {
				if a := evidence.Reviews[i].Attribution; a != nil {
					a.Target.CriteriaHash = combinedHash
				}
			}
			combined, outcome, _ := domain.EvaluateTaskEvidence(withCI, commit, nil, false, &evidence, now)
			if outcome == "passed" || (tc.name == "approved" && combined[0].Outcome != "passed") || combined[1].Outcome != "inconclusive" {
				t.Fatal("review replaced absent objective check")
			}
		})
	}
}
