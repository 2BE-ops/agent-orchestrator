package controllers

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	evolutionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/evolution"
)

// EvolutionService is the controlled-experiment boundary shared with CLI and,
// later, native manager tools. Manager-origin actors are established by the
// application, never by a JSON field.
type EvolutionService interface {
	CreateExperiment(context.Context, domain.RegistryActor, domain.ProjectID, evolutionsvc.ExperimentInput) (domain.EvolutionExperiment, error)
	GetExperiment(context.Context, domain.ProjectID, string) (domain.EvolutionExperiment, error)
	ListExperiments(context.Context, domain.ProjectID, string, int) ([]domain.EvolutionExperiment, error)
	ConcludeExperiment(context.Context, domain.RegistryActor, domain.ProjectID, string, evolutionsvc.ConclusionInput) (domain.EvolutionExperiment, error)
	ExperimentEvidence(context.Context, domain.ProjectID, string, time.Time, time.Time) (domain.EvolutionEvidence, error)
	DiffExperiment(context.Context, domain.ProjectID, string) (domain.RegistryDefinitionDiff, error)
	CreateRecommendation(context.Context, domain.ProjectID, evolutionsvc.RecommendationInput) (domain.EvolutionRecommendation, error)
	GetRecommendation(context.Context, domain.ProjectID, string) (domain.EvolutionRecommendation, error)
	ListRecommendations(context.Context, domain.ProjectID, string, int) ([]domain.EvolutionRecommendation, error)
	DecideRecommendation(context.Context, domain.RegistryActor, domain.ProjectID, string, evolutionsvc.DecisionInput) (domain.EvolutionRecommendation, error)
	DiffRecommendation(context.Context, domain.ProjectID, string) (domain.RegistryDefinitionDiff, error)
}

// EvolutionController mounts the project-scoped experiment and recommendation
// API. Every write records its actor; evidence is always derived, never posted.
type EvolutionController struct{ Svc EvolutionService }

// Register exposes experiments and recommendations under their project.
func (c *EvolutionController) Register(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(c.available)
		r.Post("/projects/{id}/experiments", c.createExperiment)
		r.Get("/projects/{id}/experiments", c.listExperiments)
		r.Get("/projects/{id}/experiments/{experimentId}", c.getExperiment)
		r.Get("/projects/{id}/experiments/{experimentId}/evidence", c.experimentEvidence)
		r.Post("/projects/{id}/experiments/{experimentId}/conclude", c.concludeExperiment)
		r.Get("/projects/{id}/experiments/{experimentId}/diff", c.diffExperiment)
		r.Post("/projects/{id}/recommendations", c.createRecommendation)
		r.Get("/projects/{id}/recommendations", c.listRecommendations)
		r.Get("/projects/{id}/recommendations/{recommendationId}", c.getRecommendation)
		r.Post("/projects/{id}/recommendations/{recommendationId}/decide", c.decideRecommendation)
		r.Get("/projects/{id}/recommendations/{recommendationId}/diff", c.diffRecommendation)
	})
}

func (c *EvolutionController) available(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.Svc == nil {
			envelope.WriteError(w, r, apierr.NotImplemented("EVOLUTION_UNAVAILABLE", "Evolution service is unavailable"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (c *EvolutionController) createExperiment(w http.ResponseWriter, r *http.Request) {
	var input evolutionsvc.ExperimentInput
	if !decodeTaskOutput(w, r, &input, 32<<10, "INVALID_EVOLUTION_JSON", "evolution-experiment") {
		return
	}
	experiment, err := c.Svc.CreateExperiment(r.Context(), evolutionHumanActor(), domain.ProjectID(chi.URLParam(r, "id")), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, EvolutionExperimentResponse{Experiment: experiment})
}

func (c *EvolutionController) listExperiments(w http.ResponseWriter, r *http.Request) {
	afterID, limit, ok := parseEvolutionPage(w, r)
	if !ok {
		return
	}
	items, err := c.Svc.ListExperiments(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), afterID, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := EvolutionExperimentsResponse{Items: items}
	if len(items) == limit {
		response.NextAfterID = items[len(items)-1].ID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *EvolutionController) getExperiment(w http.ResponseWriter, r *http.Request) {
	experiment, err := c.Svc.GetExperiment(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "experimentId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, EvolutionExperimentResponse{Experiment: experiment})
}

func (c *EvolutionController) experimentEvidence(w http.ResponseWriter, r *http.Request) {
	from, to, ok := parseEvolutionWindow(w, r)
	if !ok {
		return
	}
	evidence, err := c.Svc.ExperimentEvidence(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "experimentId"), from, to)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, EvolutionEvidenceResponse{Evidence: evidence})
}

func (c *EvolutionController) concludeExperiment(w http.ResponseWriter, r *http.Request) {
	var input evolutionsvc.ConclusionInput
	if !decodeTaskOutput(w, r, &input, 32<<10, "INVALID_EVOLUTION_JSON", "evolution-conclude") {
		return
	}
	if input.From.IsZero() || input.To.IsZero() {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_EVOLUTION_WINDOW", "Seal the admission window the conclusion was observed over", nil))
		return
	}
	experiment, err := c.Svc.ConcludeExperiment(r.Context(), evolutionHumanActor(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "experimentId"), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, EvolutionExperimentResponse{Experiment: experiment})
}

func (c *EvolutionController) diffExperiment(w http.ResponseWriter, r *http.Request) {
	diff, err := c.Svc.DiffExperiment(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "experimentId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, EvolutionDefinitionDiffResponse{Diff: diff})
}

func (c *EvolutionController) createRecommendation(w http.ResponseWriter, r *http.Request) {
	var input evolutionsvc.RecommendationInput
	if !decodeTaskOutput(w, r, &input, 256<<10, "INVALID_EVOLUTION_JSON", "evolution-recommendation") {
		return
	}
	recommendation, err := c.Svc.CreateRecommendation(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, EvolutionRecommendationResponse{Recommendation: recommendation})
}

func (c *EvolutionController) listRecommendations(w http.ResponseWriter, r *http.Request) {
	afterID, limit, ok := parseEvolutionPage(w, r)
	if !ok {
		return
	}
	items, err := c.Svc.ListRecommendations(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), afterID, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := EvolutionRecommendationsResponse{Items: items}
	if len(items) == limit {
		response.NextAfterID = items[len(items)-1].ID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *EvolutionController) getRecommendation(w http.ResponseWriter, r *http.Request) {
	recommendation, err := c.Svc.GetRecommendation(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "recommendationId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, EvolutionRecommendationResponse{Recommendation: recommendation})
}

func (c *EvolutionController) decideRecommendation(w http.ResponseWriter, r *http.Request) {
	var input evolutionsvc.DecisionInput
	if !decodeTaskOutput(w, r, &input, 32<<10, "INVALID_EVOLUTION_JSON", "evolution-decide") {
		return
	}
	recommendation, err := c.Svc.DecideRecommendation(r.Context(), evolutionHumanActor(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "recommendationId"), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, EvolutionRecommendationResponse{Recommendation: recommendation})
}

func (c *EvolutionController) diffRecommendation(w http.ResponseWriter, r *http.Request) {
	diff, err := c.Svc.DiffRecommendation(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "recommendationId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, EvolutionDefinitionDiffResponse{Diff: diff})
}

func evolutionHumanActor() domain.RegistryActor {
	return domain.RegistryActor{Origin: domain.RegistryUser, ID: "local-user"}
}

// parseEvolutionPage reads the strict id-keyset page shared by both histories.
func parseEvolutionPage(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	return parseStrictIDPage(w, r, apierr.Invalid("INVALID_EVOLUTION_PAGE", "Use one bounded afterId and a limit from 1 to 100", nil))
}

// parseEvolutionWindow reads the strict admission window used by evidence and
// conclusions. Both bounds are required; neither is defaulted.
func parseEvolutionWindow(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	return parseStrictWindow(w, r, apierr.Invalid("INVALID_EVOLUTION_WINDOW", "Use one RFC3339 from and to with from before to", nil))
}
