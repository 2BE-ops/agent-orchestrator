package ports

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Task errors preserve graph and ownership failures across service boundaries.
var (
	ErrTaskNotFound  = errors.New("task or revision not found")
	ErrTaskConflict  = errors.New("task revision conflict")
	ErrTaskForbidden = errors.New("task planning authority required")
	ErrTaskInvalid   = errors.New("invalid task graph or definition")
)

// AdaptiveTaskStore owns transactional planning, immutable criteria and history.
type AdaptiveTaskStore interface {
	CreateAdaptiveTask(context.Context, string, domain.ProjectID, domain.TaskDefinition, *domain.AcceptanceCriteria, domain.TaskMutation) (domain.AdaptiveTask, error)
	GetAdaptiveTask(context.Context, string) (domain.AdaptiveTask, error)
	ListAdaptiveTasks(context.Context, domain.ProjectID, string, int) ([]domain.AdaptiveTask, error)
	GetTaskRevision(context.Context, string, int64) (domain.TaskRevision, error)
	ListTaskRevisions(context.Context, string, int64, int) ([]domain.TaskRevision, error)
	ReviseAdaptiveTask(context.Context, string, domain.TaskDefinition, domain.TaskMutation) (domain.TaskRevision, error)
	ReviseAcceptanceCriteria(context.Context, string, domain.AcceptanceCriteria, domain.TaskMutation) (domain.TaskRevision, error)
	GetAcceptanceCriteria(context.Context, string, int64) (domain.AcceptanceCriteriaVersion, error)
	ListTaskAudit(context.Context, string, int64, int) ([]domain.TaskAudit, error)
}
