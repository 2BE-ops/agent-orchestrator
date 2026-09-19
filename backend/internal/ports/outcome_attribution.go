package ports

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ErrOutcomeCohortTooLarge requires a narrower complete attribution summary.
var ErrOutcomeCohortTooLarge = errors.New("attribution window exceeds the cohort limit")

// ErrOutcomePageInvalid bounds attribution page reads.
var ErrOutcomePageInvalid = errors.New("invalid attribution page")

// ManagerRoutingOutcomeStore derives the Manager's routing outcomes — sealed
// decisions coupled with the read-time fate of the routed tasks — and their
// complete bounded summaries. Nothing is written; worker metrics stay separate.
type ManagerRoutingOutcomeStore interface {
	ListManagerRoutingOutcomes(ctx context.Context, projectID domain.ProjectID, afterSequence int64, limit int) ([]domain.ManagerRoutingOutcome, error)
	ManagerRoutingSummary(ctx context.Context, query domain.OutcomeAttributionQuery) (domain.ManagerRoutingSummary, error)
}

// OrchestratorPlanningOutcomeStore derives the orchestrator's planning
// outcomes — sealed plan receipts coupled with the read-time fate of the tasks
// they created or revised — and their complete bounded summaries.
type OrchestratorPlanningOutcomeStore interface {
	ListOrchestratorPlanningOutcomes(ctx context.Context, projectID domain.ProjectID, afterReceiptID string, limit int) ([]domain.OrchestratorPlanningOutcome, error)
	OrchestratorPlanningSummary(ctx context.Context, query domain.OutcomeAttributionQuery) (domain.OrchestratorPlanningSummary, error)
}
