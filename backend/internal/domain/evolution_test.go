package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func evolutionExperiment() domain.EvolutionExperiment {
	return domain.EvolutionExperiment{
		ID: "experiment-1", ProjectID: "project", Kind: domain.RegistryAgentType, EntryID: "rust-coder",
		ControlVersion: 3, CandidateVersion: 4, Hypothesis: "Rust-v4 reduces review revisions",
		MinimumSamples: 12, Status: "running", CreatedAt: time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC),
	}
}

func evolutionEvidence(control, candidate int64) domain.EvolutionEvidence {
	return domain.EvolutionEvidence{
		From:       time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC),
		To:         time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC),
		ObservedAt: time.Date(2026, 9, 19, 9, 30, 0, 0, time.UTC),
		Control:    domain.EvolutionCohort{Version: 3, ComparableAttempts: control, Metrics: domain.EvolutionCohortMetrics{Attempts: control, AssessedPassed: control / 2}},
		Candidate:  domain.EvolutionCohort{Version: 4, ComparableAttempts: candidate, Metrics: domain.EvolutionCohortMetrics{Attempts: candidate, AssessedPassed: candidate / 2}},
	}
}

func TestEvolutionExperimentValidatePinsComparableVersions(t *testing.T) {
	if err := evolutionExperiment().Validate(); err != nil {
		t.Fatalf("valid experiment: %v", err)
	}
	for name, mutate := range map[string]func(*domain.EvolutionExperiment){
		"same version twice":     func(e *domain.EvolutionExperiment) { e.CandidateVersion = e.ControlVersion },
		"zero control":           func(e *domain.EvolutionExperiment) { e.ControlVersion = 0 },
		"missing hypothesis":     func(e *domain.EvolutionExperiment) { e.Hypothesis = " " },
		"zero minimum samples":   func(e *domain.EvolutionExperiment) { e.MinimumSamples = 0 },
		"oversized minimum":      func(e *domain.EvolutionExperiment) { e.MinimumSamples = 1001 },
		"unknown kind":           func(e *domain.EvolutionExperiment) { e.Kind = "workspace" },
		"concluded without seal": func(e *domain.EvolutionExperiment) { e.Status = "concluded" },
		"missing timestamp":      func(e *domain.EvolutionExperiment) { e.CreatedAt = time.Time{} },
	} {
		experiment := evolutionExperiment()
		mutate(&experiment)
		if err := experiment.Validate(); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestEvaluateEvolutionEvidenceGatesPromotion(t *testing.T) {
	experiment := evolutionExperiment()
	eligible := domain.EvaluateEvolutionEvidence(experiment, evolutionEvidence(15, 14))
	if !eligible.Eligible || len(eligible.Findings) != 0 {
		t.Fatalf("sufficient cohorts must be eligible: %+v", eligible)
	}
	short := domain.EvaluateEvolutionEvidence(experiment, evolutionEvidence(15, 4))
	if short.Eligible {
		t.Fatal("candidate below minimum must block promotion")
	}
	joined := strings.Join(short.Findings, "; ")
	if !strings.Contains(joined, "candidate cohort has 4 comparable attempts, minimum 12") {
		t.Fatalf("findings must name the failed gate: %q", joined)
	}
	confounded := evolutionEvidence(15, 14)
	confounded.ConfoundedTasks = 2
	verdict := domain.EvaluateEvolutionEvidence(experiment, confounded)
	if verdict.Eligible || !strings.Contains(strings.Join(verdict.Findings, "; "), "2 tasks contributed attempts to both cohorts") {
		t.Fatalf("task overlap must block promotion: %+v", verdict)
	}
	mismatched := evolutionEvidence(15, 14)
	mismatched.Candidate.Version = 5
	if domain.EvaluateEvolutionEvidence(experiment, mismatched).Eligible {
		t.Fatal("evidence pinned to other versions must not promote")
	}
}

func evolutionConclusion(outcome, promotion string) domain.EvolutionConclusion {
	return domain.EvolutionConclusion{
		Outcome: outcome, Promotion: promotion, Reason: "Evidence reviewed",
		Actor:     domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"},
		DecidedAt: time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC),
		Evidence:  evolutionEvidence(15, 14),
		Verdict:   domain.EvaluateEvolutionEvidence(evolutionExperiment(), evolutionEvidence(15, 14)),
	}
}

func TestEvolutionConclusionValidateEnforcesPolicyPath(t *testing.T) {
	if err := evolutionConclusion("promote_candidate", "version_permitted").Validate(); err != nil {
		t.Fatalf("permitted promotion: %v", err)
	}
	if err := evolutionConclusion("promote_candidate", "recommendation_required").Validate(); err != nil {
		t.Fatalf("policy-blocked promotion: %v", err)
	}
	for name, conclusion := range map[string]domain.EvolutionConclusion{
		"promotion without path": evolutionConclusion("promote_candidate", "none"),
		"keep with promotion":    evolutionConclusion("keep_control", "version_permitted"),
		"unknown outcome":        evolutionConclusion("rollback", "none"),
		"missing reason": func() domain.EvolutionConclusion {
			c := evolutionConclusion("keep_control", "none")
			c.Reason = ""
			return c
		}(),
		"unknown actor": func() domain.EvolutionConclusion {
			c := evolutionConclusion("keep_control", "none")
			c.Actor.Origin = "AGENT"
			return c
		}(),
		"ineligible promotion": func() domain.EvolutionConclusion {
			c := evolutionConclusion("promote_candidate", "version_permitted")
			c.Verdict.Eligible = false
			return c
		}(),
		"unsealed evidence window": func() domain.EvolutionConclusion {
			c := evolutionConclusion("keep_control", "none")
			c.Evidence.To = c.Evidence.From
			return c
		}(),
	} {
		if err := conclusion.Validate(); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func evolutionRecommendation() domain.EvolutionRecommendation {
	return domain.EvolutionRecommendation{
		ID: "recommendation-1", ProjectID: "project", Kind: domain.RegistrySkill, EntryID: "reviewer",
		FromVersion: 2, Observation: "6 of 11 recent tasks required revision",
		SampleSize: 11, Status: "pending", CreatedAt: time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC),
		Proposed: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Require component tests", Capabilities: []string{"review"}}},
	}
}

func TestEvolutionRecommendationValidateCarriesProposal(t *testing.T) {
	if err := evolutionRecommendation().Validate(); err != nil {
		t.Fatalf("valid recommendation: %v", err)
	}
	for name, mutate := range map[string]func(*domain.EvolutionRecommendation){
		"missing observation":  func(r *domain.EvolutionRecommendation) { r.Observation = "" },
		"zero sample":          func(r *domain.EvolutionRecommendation) { r.SampleSize = 0 },
		"invalid proposal":     func(r *domain.EvolutionRecommendation) { r.Proposed.Skill = nil },
		"decided without seal": func(r *domain.EvolutionRecommendation) { r.Status = "adopted" },
		"pending with seal": func(r *domain.EvolutionRecommendation) {
			r.Decision = &domain.EvolutionDecision{Disposition: "adopted", Reason: "Ship", Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, DecidedAt: time.Now().UTC()}
		},
	} {
		recommendation := evolutionRecommendation()
		mutate(&recommendation)
		if err := recommendation.Validate(); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestDiffRegistryDefinitionsReportsBoundedFields(t *testing.T) {
	from := domain.RegistryDefinition{Skill: &domain.SkillDefinition{
		Instructions: "Review changes", Capabilities: []string{"review", "lint"}, RequiredTools: []string{"git"},
		Resources: []domain.SkillResource{{Path: "checklist.md", Content: "- accessibility"}},
	}}
	to := domain.RegistryDefinition{Skill: &domain.SkillDefinition{
		Instructions: "Review changes and component tests", Capabilities: []string{"review", "testing"}, RequiredTools: []string{"git"},
		Resources: []domain.SkillResource{{Path: "checklist.md", Content: "- accessibility\n- component tests"}, {Path: "fixtures.md", Content: "seed"}},
	}}
	diff, err := domain.DiffRegistryDefinitions(domain.RegistrySkill, "reviewer", 2, 3, from, to)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]domain.RegistryDiffField{}
	for _, field := range diff.Fields {
		byPath[field.Path] = field
	}
	if len(diff.Fields) != 5 {
		t.Fatalf("expected five changed fields, got %d: %+v", len(diff.Fields), diff.Fields)
	}
	if field := byPath["instructions"]; field.Change != "changed" || !strings.Contains(field.To, "component tests") {
		t.Fatalf("instructions diff: %+v", field)
	}
	if field := byPath["capabilities.lint"]; field.Change != "removed" || field.To != "" {
		t.Fatalf("removed capability: %+v", field)
	}
	if field := byPath["capabilities.testing"]; field.Change != "added" || field.From != "" {
		t.Fatalf("added capability: %+v", field)
	}
	if field := byPath["resources.checklist.md"]; field.Change != "changed" || !strings.Contains(field.From, "accessibility") {
		t.Fatalf("changed resource: %+v", field)
	}
	if field := byPath["resources.fixtures.md"]; field.Change != "added" {
		t.Fatalf("added resource: %+v", field)
	}
	identical, err := domain.DiffRegistryDefinitions(domain.RegistrySkill, "reviewer", 2, 2, from, from)
	if err != nil || len(identical.Fields) != 0 {
		t.Fatalf("identical definitions must produce an empty diff: %v %+v", err, identical.Fields)
	}
}

func TestDiffRegistryDefinitionsRejectsBrokenInput(t *testing.T) {
	agentType := domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, Instructions: "Work", MaxParallelWorkers: 1, Capabilities: []string{}}}
	if _, err := domain.DiffRegistryDefinitions(domain.RegistryAgentType, "coder", 1, 2, domain.RegistryDefinition{}, agentType); err == nil {
		t.Fatal("invalid from-definition must be refused")
	}
	if _, err := domain.DiffRegistryDefinitions(domain.RegistrySkill, "coder", 1, 2, agentType, agentType); err == nil {
		t.Fatal("kind/definition mismatch must be refused")
	}
}
