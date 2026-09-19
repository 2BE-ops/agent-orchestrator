package ports

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Project controls are deterministic durable state: fencing happens inside
// the session-creation and reservation transactions, never in an LLM, and no
// caller can admit work a control has already fenced.
var (
	// ErrProjectControlInvalid reports a malformed control instruction.
	ErrProjectControlInvalid = errors.New("invalid project control")
	// ErrProjectControlConflict reports an illegal control transition.
	ErrProjectControlConflict = errors.New("project control transition is not allowed")
	// ErrProjectControlNotFound reports an unknown project.
	ErrProjectControlNotFound = errors.New("project not found")
	// ErrProjectAdmissionsFenced reports a launch refused because the
	// project's control state fences new admissions.
	ErrProjectAdmissionsFenced = errors.New("project admissions are fenced by its control state")
	// ErrTaskNeedsHumanInvalid reports a malformed needs human request.
	ErrTaskNeedsHumanInvalid = errors.New("invalid needs human request")
	// ErrTaskNeedsHumanConflict reports a request that cannot be applied,
	// because one is already pending or none is.
	ErrTaskNeedsHumanConflict = errors.New("needs human request conflicts with its pending state")
	// ErrTaskNeedsHumanNotFound reports a missing pending request.
	ErrTaskNeedsHumanNotFound = errors.New("no pending needs human request")
	// ErrTaskNeedsHumanFenced reports a reservation refused because the task
	// or one of its ancestors has a pending needs human request.
	ErrTaskNeedsHumanFenced = errors.New("task is fenced by a pending needs human request")
	// ErrDryRunInvalid reports a malformed dry-run request.
	ErrDryRunInvalid = errors.New("invalid dry run request")
)

// ProjectControlStore reads and steers one project's adaptive controls.
type ProjectControlStore interface {
	GetProjectControl(ctx context.Context, projectID domain.ProjectID) (domain.ProjectControlView, error)
	SetProjectControl(ctx context.Context, projectID domain.ProjectID, target domain.ProjectControlState, actor domain.AdaptiveActor, reason string, now time.Time) (domain.ProjectControlView, error)
	CancelProjectWork(ctx context.Context, cancellation domain.ProjectWorkCancellation) (domain.ProjectWorkCancellationResult, error)
}

// DryRunStore simulates plans read-only: no worktree, no harness install, no
// repository write and no registry/task mutation ever happens inside it.
type DryRunStore interface {
	DryRunPlan(ctx context.Context, projectID domain.ProjectID, request domain.DryRunRequest) (domain.DryRunVerdict, error)
}

// ProjectAttemptSessionReader names the worker sessions holding live task
// attempts in one project, so cancel-all can request termination through the
// normal kill services instead of assuming anything stopped.
type ProjectAttemptSessionReader interface {
	ListProjectActiveAttemptSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionID, error)
}

// TaskNeedsHumanStore records and pages structured requests for human input.
type TaskNeedsHumanStore interface {
	RaiseTaskNeedsHuman(ctx context.Context, taskID string, request domain.TaskNeedsHuman) (domain.TaskNeedsHuman, error)
	ResolveTaskNeedsHuman(ctx context.Context, taskID string, resolution domain.TaskNeedsHumanResolution) (domain.TaskNeedsHuman, error)
	GetPendingTaskNeedsHuman(ctx context.Context, taskID string) (domain.TaskNeedsHuman, bool, error)
	ListProjectNeedsHuman(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.TaskNeedsHuman, error)
	NeedsHumanTaskAncestor(ctx context.Context, id string) (string, error)
}
