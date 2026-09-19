package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	evolutionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/evolution"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func evolutionAPIFixture(t *testing.T) (*sqlite.Store, http.Handler) {
	t.Helper()
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "local-user"}
	definition := domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeTUI, Instructions: "Check work", MaxParallelWorkers: 5, Capabilities: []string{}}}
	if _, err := s.CreateRegistryEntry(ctx, "evolution-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Candidate", Enabled: true, Policy: domain.RegistryPolicy{ManagerCanSelect: true, ManagerCanVersion: true}}, definition, domain.RegistryMutation{Actor: actor, Reason: "Seed"}); err != nil {
		t.Fatal(err)
	}
	second := definition
	second.AgentType.Instructions = "Check work and component tests"
	if _, err := s.AppendRegistryVersion(ctx, "evolution-type", second, domain.RegistryMutation{Actor: actor, ExpectedRevision: 1, Reason: "Candidate"}); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1", (&controllers.EvolutionController{Svc: evolutionsvc.New(s)}).Register)
	return s, r
}

func TestEvolutionExperimentAPILifecycle(t *testing.T) {
	_, router := evolutionAPIFixture(t)
	input := evolutionsvc.ExperimentInput{Kind: domain.RegistryAgentType, EntryID: "evolution-type", ControlVersion: 1, CandidateVersion: 2, Hypothesis: "Candidate reduces review revisions", MinimumSamples: 12}
	w := registryRequest(t, router, http.MethodPost, "/projects/project/experiments", input, http.StatusCreated)
	var created controllers.EvolutionExperimentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Experiment.Status != "running" || created.Experiment.ID == "" {
		t.Fatalf("create: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodPost, "/projects/project/experiments", evolutionsvc.ExperimentInput{Kind: "workspace"}, http.StatusBadRequest)

	w = registryRequest(t, router, http.MethodGet, "/projects/project/experiments?limit=20", nil, http.StatusOK)
	var listed controllers.EvolutionExperimentsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed.Items) != 1 || listed.NextAfterID != "" {
		t.Fatalf("list: %s (%v)", w.Body.String(), err)
	}
	experimentID := listed.Items[0].ID
	w = registryRequest(t, router, http.MethodGet, "/projects/project/experiments/"+experimentID, nil, http.StatusOK)
	var read controllers.EvolutionExperimentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &read); err != nil || read.Experiment.ID != experimentID {
		t.Fatalf("read: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodGet, "/projects/project/experiments/missing", nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, "/projects/project/experiments?limit=0", nil, http.StatusBadRequest)

	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	to := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	w = registryRequest(t, router, http.MethodGet, "/projects/project/experiments/"+experimentID+"/evidence?from="+from+"&to="+to, nil, http.StatusOK)
	var evidence controllers.EvolutionEvidenceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &evidence); err != nil || evidence.Evidence.Control.ComparableAttempts != 0 || evidence.Evidence.Candidate.Version != 2 {
		t.Fatalf("evidence: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodGet, "/projects/project/experiments/"+experimentID+"/evidence?from="+to+"&to="+from, nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, "/projects/project/experiments/"+experimentID+"/evidence?from="+from, nil, http.StatusBadRequest)

	w = registryRequest(t, router, http.MethodGet, "/projects/project/experiments/"+experimentID+"/diff", nil, http.StatusOK)
	var diff controllers.EvolutionDefinitionDiffResponse
	if err := json.Unmarshal(w.Body.Bytes(), &diff); err != nil || diff.Diff.FromNumber != 1 || diff.Diff.ToNumber != 2 || len(diff.Diff.Fields) == 0 {
		t.Fatalf("diff: %s (%v)", w.Body.String(), err)
	}

	promote := evolutionsvc.ConclusionInput{Outcome: "promote_candidate", Reason: "Too early", From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	w = registryRequest(t, router, http.MethodPost, "/projects/project/experiments/"+experimentID+"/conclude", promote, http.StatusBadRequest)
	var rejected struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rejected); err != nil || rejected.Code != "EVOLUTION_NOT_ELIGIBLE" {
		t.Fatalf("promotion refusal envelope: %s (%v)", w.Body.String(), err)
	}
	keep := evolutionsvc.ConclusionInput{Outcome: "insufficient_evidence", Reason: "No comparable work in the window", From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	w = registryRequest(t, router, http.MethodPost, "/projects/project/experiments/"+experimentID+"/conclude", keep, http.StatusOK)
	var concluded controllers.EvolutionExperimentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &concluded); err != nil || concluded.Experiment.Status != "concluded" || concluded.Experiment.Conclusion == nil || concluded.Experiment.Conclusion.Promotion != "none" {
		t.Fatalf("conclude: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodPost, "/projects/project/experiments/"+experimentID+"/conclude", keep, http.StatusConflict)
	registryRequest(t, router, http.MethodPost, "/projects/project/experiments/"+experimentID+"/conclude", evolutionsvc.ConclusionInput{Outcome: "keep_control", Reason: "No window"}, http.StatusBadRequest)
}

func TestEvolutionRecommendationAPILifecycle(t *testing.T) {
	_, router := evolutionAPIFixture(t)
	input := evolutionsvc.RecommendationInput{Kind: domain.RegistryAgentType, EntryID: "evolution-type", FromVersion: 1, Observation: "Three of four recent tasks required a revision", SampleSize: 4, Proposed: domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeTUI, Instructions: "Require component tests", MaxParallelWorkers: 5, Capabilities: []string{}}}}
	w := registryRequest(t, router, http.MethodPost, "/projects/project/recommendations", input, http.StatusCreated)
	var created controllers.EvolutionRecommendationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Recommendation.Status != "pending" || created.Recommendation.ID == "" {
		t.Fatalf("create: %s (%v)", w.Body.String(), err)
	}
	recommendationID := created.Recommendation.ID
	w = registryRequest(t, router, http.MethodGet, "/projects/project/recommendations?limit=20", nil, http.StatusOK)
	var listed controllers.EvolutionRecommendationsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed.Items) != 1 || listed.Items[0].ID != recommendationID {
		t.Fatalf("list: %s (%v)", w.Body.String(), err)
	}
	w = registryRequest(t, router, http.MethodGet, "/projects/project/recommendations/"+recommendationID+"/diff", nil, http.StatusOK)
	var diff controllers.EvolutionDefinitionDiffResponse
	if err := json.Unmarshal(w.Body.Bytes(), &diff); err != nil || diff.Diff.ToNumber != 0 || len(diff.Diff.Fields) == 0 {
		t.Fatalf("diff: %s (%v)", w.Body.String(), err)
	}
	decide := evolutionsvc.DecisionInput{Disposition: "adopted", Reason: "Create the version through registry authoring"}
	w = registryRequest(t, router, http.MethodPost, "/projects/project/recommendations/"+recommendationID+"/decide", decide, http.StatusOK)
	var decided controllers.EvolutionRecommendationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &decided); err != nil || decided.Recommendation.Status != "adopted" || decided.Recommendation.Decision == nil {
		t.Fatalf("decide: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodPost, "/projects/project/recommendations/"+recommendationID+"/decide", decide, http.StatusConflict)
	registryRequest(t, router, http.MethodPost, "/projects/project/recommendations/"+recommendationID+"/decide", evolutionsvc.DecisionInput{Disposition: "deferred", Reason: "x"}, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, "/projects/project/recommendations/missing/diff", nil, http.StatusNotFound)
}
