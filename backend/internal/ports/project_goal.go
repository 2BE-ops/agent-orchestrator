package ports

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Project goal errors preserve authority and verification failures across
// service boundaries.
var (
	ErrGoalNotFound   = errors.New("project goal not found")
	ErrGoalConflict   = errors.New("project goal version conflict")
	ErrGoalIncomplete = errors.New("project goal is not satisfied by durable task facts")
	ErrGoalInvalid    = errors.New("invalid project goal or completion")

	ErrOrchestratorPlanNotFound = errors.New("orchestrator plan receipt not found")
	ErrOrchestratorPlanConflict = errors.New("orchestrator plan receipt conflict")
)

// ProjectGoalStore owns versioned project goals and verified completions.
type ProjectGoalStore interface {
	SetProjectGoal(ctx context.Context, projectID domain.ProjectID, goal, reason string, actor domain.AdaptiveActor) (domain.ProjectGoalVersion, error)
	GetProjectGoal(ctx context.Context, projectID domain.ProjectID) (domain.ProjectGoalVersion, error)
	ListProjectGoalVersions(ctx context.Context, projectID domain.ProjectID, after int64, limit int) ([]domain.ProjectGoalVersion, error)
	CompleteProjectGoal(ctx context.Context, request domain.ProjectGoalCompletionRequest) (domain.ProjectGoalCompletion, bool, error)
	GetProjectGoalCompletion(ctx context.Context, projectID domain.ProjectID, goalVersion int64) (domain.ProjectGoalCompletion, error)
	ListProjectGoalCompletions(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.ProjectGoalCompletion, error)
}

// OrchestratorPlanStore seals native planning actions with their effects.
type OrchestratorPlanStore interface {
	ApplyOrchestratorPlan(ctx context.Context, submission domain.OrchestratorPlanSubmission) (domain.OrchestratorPlanReceipt, bool, error)
	GetOrchestratorPlanReceipt(ctx context.Context, projectID domain.ProjectID, id string) (domain.OrchestratorPlanReceipt, error)
	ListOrchestratorPlanReceipts(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.OrchestratorPlanReceipt, error)
}

// ProjectFeedbackStore derives per-task loop facts at read time. Nothing is
// stored: terminal outcomes, exhaustion and cancellation all come from the
// durable rows the rest of the system already owns.
type ProjectFeedbackStore interface {
	ListProjectFeedback(ctx context.Context, projectID domain.ProjectID, afterTaskID string, limit int) ([]domain.ProjectFeedbackItem, error)
}
