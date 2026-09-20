package evolution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func evolutionAPIError(t *testing.T, err error) string {
	t.Helper()
	var envelope *apierr.Error
	if !errors.As(err, &envelope) {
		t.Fatalf("expected api error, got %T %v", err, err)
	}
	return envelope.Code
}

func TestEvolutionServiceValidatesInputsBeforeStore(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	m := New(store)
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "local-user"}
	if _, err := m.CreateExperiment(ctx, actor, "project", ExperimentInput{Kind: "workspace", EntryID: "type", ControlVersion: 1, CandidateVersion: 2, Hypothesis: "x", MinimumSamples: 2}); evolutionAPIError(t, err) != "INVALID_EVOLUTION_INPUT" {
		t.Fatalf("unknown kind: %v", err)
	}
	if _, err := m.CreateExperiment(ctx, actor, "project", ExperimentInput{Kind: domain.RegistryAgentType, EntryID: "type", ControlVersion: 1, CandidateVersion: 1, Hypothesis: "x", MinimumSamples: 2}); evolutionAPIError(t, err) != "INVALID_EVOLUTION_INPUT" {
		t.Fatalf("identical versions: %v", err)
	}
	if _, err := m.CreateExperiment(ctx, actor, "project", ExperimentInput{Kind: domain.RegistryAgentType, EntryID: "type", ControlVersion: 1, CandidateVersion: 2, Hypothesis: "x", MinimumSamples: 0}); evolutionAPIError(t, err) != "INVALID_EVOLUTION_INPUT" {
		t.Fatalf("zero minimum: %v", err)
	}
	if _, err := m.ListExperiments(ctx, "project", "", 0); evolutionAPIError(t, err) != "INVALID_EVOLUTION_PAGE" {
		t.Fatalf("zero page: %v", err)
	}
	if _, err := m.ListRecommendations(ctx, "project", "", 101); evolutionAPIError(t, err) != "INVALID_EVOLUTION_PAGE" {
		t.Fatalf("oversized page: %v", err)
	}
	if _, err := m.GetExperiment(ctx, "project", "missing"); evolutionAPIError(t, err) != "EVOLUTION_EXPERIMENT_NOT_FOUND" {
		t.Fatalf("missing experiment: %v", err)
	}
	if _, err := m.GetRecommendation(ctx, "project", "missing"); evolutionAPIError(t, err) != "EVOLUTION_RECOMMENDATION_NOT_FOUND" {
		t.Fatalf("missing recommendation: %v", err)
	}
	if _, err := m.CreateRecommendation(ctx, "project", RecommendationInput{Kind: domain.RegistrySkill, EntryID: "skill", FromVersion: 1, Observation: "x", SampleSize: 0, Proposed: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "x"}}}); evolutionAPIError(t, err) != "INVALID_EVOLUTION_INPUT" {
		t.Fatalf("zero sample: %v", err)
	}
}

func TestEvolutionServiceConcludesThroughPolicyGate(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	m := New(store)
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "local-user"}
	metadata := domain.RegistryMetadata{Name: "permitted", Enabled: true, Policy: domain.RegistryPolicy{ManagerCanVersion: true}}
	definition := domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeTUI, Instructions: "Check work", MaxParallelWorkers: 5, Capabilities: []string{}}}
	if _, err := store.CreateRegistryEntry(ctx, "permitted", domain.RegistryAgentType, metadata, definition, domain.RegistryMutation{Actor: actor, Reason: "Seed"}); err != nil {
		t.Fatal(err)
	}
	second := definition
	second.AgentType.Instructions = "Check work and tests"
	if _, err := store.AppendRegistryVersion(ctx, "permitted", second, domain.RegistryMutation{Actor: actor, ExpectedRevision: 1, Reason: "Candidate"}); err != nil {
		t.Fatal(err)
	}

	permitted, err := m.CreateExperiment(ctx, actor, "project", ExperimentInput{Kind: domain.RegistryAgentType, EntryID: "permitted", ControlVersion: 1, CandidateVersion: 2, Hypothesis: "Candidate reduces revisions", MinimumSamples: 1})
	if err != nil || permitted.Status != "running" || permitted.ID == "" {
		t.Fatalf("create: %+v %v", permitted, err)
	}
	from, to := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	evidence, err := m.ExperimentEvidence(ctx, "project", permitted.ID, from, to)
	if err != nil || evidence.Control.ComparableAttempts != 0 || evidence.Candidate.ComparableAttempts != 0 {
		t.Fatalf("empty evidence: %+v %v", evidence, err)
	}
	early := ConclusionInput{Outcome: "promote_candidate", Reason: "Too early", From: from, To: to}
	if _, err := m.ConcludeExperiment(ctx, domain.RegistryActor{Origin: domain.RegistryManager, ID: "manager"}, "project", permitted.ID, early); evolutionAPIError(t, err) != "EVOLUTION_NOT_ELIGIBLE" {
		t.Fatalf("promotion without evidence: %v", err)
	}
	keep := ConclusionInput{Outcome: "insufficient_evidence", Reason: "No comparable work in the window", From: from, To: to}
	concluded, err := m.ConcludeExperiment(ctx, actor, "project", permitted.ID, keep)
	if err != nil || concluded.Conclusion == nil || concluded.Conclusion.Promotion != "none" {
		t.Fatalf("keep conclusion: %+v %v", concluded, err)
	}
	if _, err := m.ConcludeExperiment(ctx, actor, "project", permitted.ID, keep); evolutionAPIError(t, err) != "EVOLUTION_STATE_CONFLICT" {
		t.Fatalf("second conclusion: %v", err)
	}
	if diff, err := m.DiffExperiment(ctx, "project", permitted.ID); err != nil || len(diff.Fields) == 0 {
		t.Fatalf("experiment diff: %+v %v", diff, err)
	}
	if _, err := m.ExperimentEvidence(ctx, "project", permitted.ID, to, from); evolutionAPIError(t, err) != "INVALID_EVOLUTION_INPUT" {
		t.Fatalf("inverted window: %v", err)
	}
}
