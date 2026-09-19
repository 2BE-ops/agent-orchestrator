// Package evolution is the controlled-experiment boundary for Agent Type and
// Skill version comparisons. It seals pinned experiments, derives comparable
// cohorts from durable attempt facts, enforces promotion gates and policy
// paths, and persists recommendations with inspectable proposed definitions.
package evolution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Store is every evolution capability the daemon's SQLite store provides.
type Store interface {
	ports.EvolutionExperimentStore
	ports.EvolutionRecommendationStore
}

// Manager applies input validation before the store's transactional gates.
type Manager struct {
	store Store
}

// New constructs the evolution service over the daemon's existing store.
func New(store Store) *Manager { return &Manager{store: store} }

// ExperimentInput contains caller-authorable fields only. The pinned versions
// and minimum sample count are the experiment's contract; they are immutable
// after creation.
type ExperimentInput struct {
	Kind             domain.RegistryKind `json:"kind" enum:"agent_type,skill"`
	EntryID          string              `json:"entryId"`
	ControlVersion   int64               `json:"controlVersion"`
	CandidateVersion int64               `json:"candidateVersion"`
	Hypothesis       string              `json:"hypothesis"`
	MinimumSamples   int64               `json:"minimumSamples"`
}

// CreateExperiment seals one pinned comparison with a daemon-minted identity.
func (m *Manager) CreateExperiment(ctx context.Context, actor domain.RegistryActor, projectID domain.ProjectID, input ExperimentInput) (domain.EvolutionExperiment, error) {
	if input.Kind != domain.RegistryAgentType && input.Kind != domain.RegistrySkill {
		return domain.EvolutionExperiment{}, apierr.Invalid("INVALID_EVOLUTION_INPUT", "Kind must be agent_type or skill", nil)
	}
	if input.ControlVersion < 1 || input.CandidateVersion < 1 || input.ControlVersion == input.CandidateVersion {
		return domain.EvolutionExperiment{}, apierr.Invalid("INVALID_EVOLUTION_INPUT", "Pin two distinct existing versions of one entry", nil)
	}
	if input.MinimumSamples < 1 || input.MinimumSamples > domain.EvolutionCohortLimit {
		return domain.EvolutionExperiment{}, apierr.Invalid("INVALID_EVOLUTION_INPUT", fmt.Sprintf("Minimum samples must be between 1 and %d", domain.EvolutionCohortLimit), nil)
	}
	experiment := domain.EvolutionExperiment{
		ID: uuid.NewString(), ProjectID: projectID, Kind: input.Kind, EntryID: input.EntryID,
		ControlVersion: input.ControlVersion, CandidateVersion: input.CandidateVersion,
		Hypothesis: input.Hypothesis, MinimumSamples: input.MinimumSamples,
		Status: "running", CreatedAt: time.Now().UTC(),
	}
	created, err := m.store.CreateEvolutionExperiment(ctx, experiment)
	return created, mapError(err)
}

// GetExperiment reads one sealed experiment.
func (m *Manager) GetExperiment(ctx context.Context, projectID domain.ProjectID, experimentID string) (domain.EvolutionExperiment, error) {
	experiment, err := m.store.GetEvolutionExperiment(ctx, projectID, experimentID)
	return experiment, mapError(err)
}

// ListExperiments pages experiments by stable ID cursor.
func (m *Manager) ListExperiments(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.EvolutionExperiment, error) {
	if limit < 1 || limit > 100 || len(afterID) > 200 {
		return nil, apierr.Invalid("INVALID_EVOLUTION_PAGE", "Page limit must be between 1 and 100", nil)
	}
	experiments, err := m.store.ListEvolutionExperiments(ctx, projectID, afterID, limit)
	return experiments, mapError(err)
}

// ConclusionInput carries the terminal decision request. Evidence is never
// accepted from the caller: the store recomputes and seals it.
type ConclusionInput struct {
	Outcome string    `json:"outcome" enum:"promote_candidate,keep_control,insufficient_evidence"`
	Reason  string    `json:"reason"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
}

// ConcludeExperiment seals the one terminal decision, refusing promotion when
// the comparable-cohort gates fail and applying the entry's manager-versioning
// policy to the promotion path.
func (m *Manager) ConcludeExperiment(ctx context.Context, actor domain.RegistryActor, projectID domain.ProjectID, experimentID string, input ConclusionInput) (domain.EvolutionExperiment, error) {
	request := domain.EvolutionConclusionRequest{Outcome: input.Outcome, Reason: input.Reason, Actor: actor, From: input.From, To: input.To}
	concluded, err := m.store.ConcludeEvolutionExperiment(ctx, projectID, experimentID, request)
	return concluded, mapError(err)
}

// ExperimentEvidence derives both comparable cohorts for an admission window.
func (m *Manager) ExperimentEvidence(ctx context.Context, projectID domain.ProjectID, experimentID string, from, to time.Time) (domain.EvolutionEvidence, error) {
	query := domain.EvolutionEvidenceQuery{ProjectID: projectID, ExperimentID: experimentID, From: from, To: to}
	evidence, err := m.store.EvolutionExperimentEvidence(ctx, query)
	return evidence, mapError(err)
}

// DiffExperiment returns the immutable field diff between the pinned versions.
func (m *Manager) DiffExperiment(ctx context.Context, projectID domain.ProjectID, experimentID string) (domain.RegistryDefinitionDiff, error) {
	diff, err := m.store.DiffEvolutionExperiment(ctx, projectID, experimentID)
	return diff, mapError(err)
}

// RecommendationInput contains caller-authorable fields only. The proposed
// definition is sealed at creation and never applied by this service.
type RecommendationInput struct {
	Kind        domain.RegistryKind       `json:"kind" enum:"agent_type,skill"`
	EntryID     string                    `json:"entryId"`
	FromVersion int64                     `json:"fromVersion"`
	Observation string                    `json:"observation"`
	SampleSize  int64                     `json:"sampleSize"`
	Proposed    domain.RegistryDefinition `json:"proposed"`
}

// CreateRecommendation persists one improvement proposal with a minted identity.
func (m *Manager) CreateRecommendation(ctx context.Context, projectID domain.ProjectID, input RecommendationInput) (domain.EvolutionRecommendation, error) {
	if input.Kind != domain.RegistryAgentType && input.Kind != domain.RegistrySkill {
		return domain.EvolutionRecommendation{}, apierr.Invalid("INVALID_EVOLUTION_INPUT", "Kind must be agent_type or skill", nil)
	}
	if input.SampleSize < 1 || input.SampleSize > domain.EvolutionCohortLimit {
		return domain.EvolutionRecommendation{}, apierr.Invalid("INVALID_EVOLUTION_INPUT", fmt.Sprintf("Sample size must be between 1 and %d", domain.EvolutionCohortLimit), nil)
	}
	recommendation := domain.EvolutionRecommendation{
		ID: uuid.NewString(), ProjectID: projectID, Kind: input.Kind, EntryID: input.EntryID,
		FromVersion: input.FromVersion, Observation: input.Observation, SampleSize: input.SampleSize,
		Proposed: input.Proposed, Status: "pending", CreatedAt: time.Now().UTC(),
	}
	created, err := m.store.CreateEvolutionRecommendation(ctx, recommendation)
	return created, mapError(err)
}

// GetRecommendation reads one sealed recommendation.
func (m *Manager) GetRecommendation(ctx context.Context, projectID domain.ProjectID, recommendationID string) (domain.EvolutionRecommendation, error) {
	recommendation, err := m.store.GetEvolutionRecommendation(ctx, projectID, recommendationID)
	return recommendation, mapError(err)
}

// ListRecommendations pages recommendations by stable ID cursor.
func (m *Manager) ListRecommendations(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.EvolutionRecommendation, error) {
	if limit < 1 || limit > 100 || len(afterID) > 200 {
		return nil, apierr.Invalid("INVALID_EVOLUTION_PAGE", "Page limit must be between 1 and 100", nil)
	}
	recommendations, err := m.store.ListEvolutionRecommendations(ctx, projectID, afterID, limit)
	return recommendations, mapError(err)
}

// DecisionInput carries the one-time disposition change.
type DecisionInput struct {
	Disposition string `json:"disposition" enum:"dismissed,adopted"`
	Reason      string `json:"reason"`
}

// DecideRecommendation seals a dismissal or adoption. Adoption is a recorded
// decision; creating the version remains normal registry authoring.
func (m *Manager) DecideRecommendation(ctx context.Context, actor domain.RegistryActor, projectID domain.ProjectID, recommendationID string, input DecisionInput) (domain.EvolutionRecommendation, error) {
	decision := domain.EvolutionDecision{Disposition: input.Disposition, Reason: input.Reason, Actor: actor, DecidedAt: time.Now().UTC()}
	decided, err := m.store.DecideEvolutionRecommendation(ctx, projectID, recommendationID, decision)
	return decided, mapError(err)
}

// DiffRecommendation returns the inspectable diff from the sealed from-version
// to the proposed definition.
func (m *Manager) DiffRecommendation(ctx context.Context, projectID domain.ProjectID, recommendationID string) (domain.RegistryDefinitionDiff, error) {
	diff, err := m.store.DiffEvolutionRecommendation(ctx, projectID, recommendationID)
	return diff, mapError(err)
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrEvolutionInvalid):
		return apierr.Invalid("INVALID_EVOLUTION_INPUT", err.Error(), nil)
	case errors.Is(err, ports.ErrEvolutionExperimentNotFound):
		return apierr.NotFound("EVOLUTION_EXPERIMENT_NOT_FOUND", "Experiment was not found in this project")
	case errors.Is(err, ports.ErrEvolutionRecommendationNotFound):
		return apierr.NotFound("EVOLUTION_RECOMMENDATION_NOT_FOUND", "Recommendation was not found in this project")
	case errors.Is(err, ports.ErrEvolutionPageInvalid):
		return apierr.Invalid("INVALID_EVOLUTION_PAGE", "Page limit must be between 1 and 100", nil)
	case errors.Is(err, ports.ErrEvolutionCohortTooLarge):
		return apierr.Invalid("EVOLUTION_COHORT_TOO_LARGE", "Narrow the admission window to at most 1000 attempts; no partial comparison was returned", nil)
	case errors.Is(err, ports.ErrEvolutionNotEligible):
		return apierr.Invalid("EVOLUTION_NOT_ELIGIBLE", "Promotion refused: "+err.Error(), nil)
	case errors.Is(err, ports.ErrEvolutionStateConflict):
		return apierr.Conflict("EVOLUTION_STATE_CONFLICT", "Evolution record already exists or is already terminal", nil)
	default:
		return err
	}
}
