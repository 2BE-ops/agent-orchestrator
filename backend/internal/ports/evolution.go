package ports

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

var (
	// ErrEvolutionInvalid wraps every rejected evolution input before any read.
	ErrEvolutionInvalid = errors.New("evolution request invalid")
	// ErrEvolutionExperimentNotFound names a missing or foreign experiment.
	ErrEvolutionExperimentNotFound = errors.New("evolution experiment not found")
	// ErrEvolutionRecommendationNotFound names a missing or foreign recommendation.
	ErrEvolutionRecommendationNotFound = errors.New("evolution recommendation not found")
	// ErrEvolutionPageInvalid wraps rejected page cursors and limits.
	ErrEvolutionPageInvalid = errors.New("evolution page invalid")
	// ErrEvolutionCohortTooLarge refuses, never partially compares, wide windows.
	ErrEvolutionCohortTooLarge = errors.New("evolution cohort too large")
	// ErrEvolutionNotEligible refuses promotion over insufficient or confounded evidence.
	ErrEvolutionNotEligible = errors.New("evolution evidence not eligible for promotion")
	// ErrEvolutionStateConflict names duplicate or already-terminal records.
	ErrEvolutionStateConflict = errors.New("evolution record state conflict")
)

// EvolutionExperimentStore seals controlled experiments, derives comparable
// cohorts from durable attempt facts, and enforces the promotion gates.
type EvolutionExperimentStore interface {
	CreateEvolutionExperiment(ctx context.Context, experiment domain.EvolutionExperiment) (domain.EvolutionExperiment, error)
	GetEvolutionExperiment(ctx context.Context, projectID domain.ProjectID, experimentID string) (domain.EvolutionExperiment, error)
	ListEvolutionExperiments(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.EvolutionExperiment, error)
	ConcludeEvolutionExperiment(ctx context.Context, projectID domain.ProjectID, experimentID string, request domain.EvolutionConclusionRequest) (domain.EvolutionExperiment, error)
	EvolutionExperimentEvidence(ctx context.Context, query domain.EvolutionEvidenceQuery) (domain.EvolutionEvidence, error)
	DiffEvolutionExperiment(ctx context.Context, projectID domain.ProjectID, experimentID string) (domain.RegistryDefinitionDiff, error)
}

// EvolutionRecommendationStore persists improvement recommendations with an
// inspectable proposed definition and one-time human/manager dispositions.
type EvolutionRecommendationStore interface {
	CreateEvolutionRecommendation(ctx context.Context, recommendation domain.EvolutionRecommendation) (domain.EvolutionRecommendation, error)
	GetEvolutionRecommendation(ctx context.Context, projectID domain.ProjectID, recommendationID string) (domain.EvolutionRecommendation, error)
	ListEvolutionRecommendations(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.EvolutionRecommendation, error)
	DecideEvolutionRecommendation(ctx context.Context, projectID domain.ProjectID, recommendationID string, decision domain.EvolutionDecision) (domain.EvolutionRecommendation, error)
	DiffEvolutionRecommendation(ctx context.Context, projectID domain.ProjectID, recommendationID string) (domain.RegistryDefinitionDiff, error)
}
