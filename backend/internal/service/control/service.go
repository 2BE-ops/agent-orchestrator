package control

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Store is the durable control surface. Terminator is optional: without it
// cancel-all still marks every task, and the report says no kill service was
// wired instead of inventing terminations.
type Store interface {
	ports.ProjectControlStore
	ports.TaskNeedsHumanStore
	ports.DryRunStore
	ports.ProjectAttemptSessionReader
}

// Terminator requests one worker's termination through the normal kill
// services. It never proves the worker stopped; reconciliation owns that.
type Terminator interface {
	Kill(ctx context.Context, id domain.SessionID) error
}

// Manager validates control actions and coordinates deterministic project
// controls, Needs Human and dry-runs over the shared store.
type Manager struct {
	store       Store
	terminator  Terminator
	now         func() time.Time
	newIdentity func() string
}

// Option wires optional daemon-owned services into the control manager.
type Option func(*Manager)

// WithTerminator wires the session kill service for cancel-all.
func WithTerminator(terminator Terminator) Option {
	return func(m *Manager) { m.terminator = terminator }
}

// New returns the control service over the shared store.
func New(store Store, options ...Option) *Manager {
	manager := &Manager{store: store, now: time.Now, newIdentity: newIdentityValue}
	for _, option := range options {
		option(manager)
	}
	return manager
}

// StateInput requests one explicit control transition.
type StateInput struct {
	State  string `json:"state" enum:"running,paused,draining,stopped"`
	Reason string `json:"reason"`
}

// Control reads one project's derived control state.
func (m *Manager) Control(ctx context.Context, project domain.ProjectID) (domain.ProjectControlView, error) {
	view, err := m.store.GetProjectControl(ctx, project)
	return view, mapError(err)
}

// SetControl applies pause, resume, drain or stop.
func (m *Manager) SetControl(ctx context.Context, actor domain.AdaptiveActor, project domain.ProjectID, input StateInput) (domain.ProjectControlView, error) {
	target := domain.ProjectControlState(input.State)
	if !domain.ValidControlState(target) {
		return domain.ProjectControlView{}, apierr.Invalid("INVALID_CONTROL_INPUT", "Unknown control state "+input.State, nil)
	}
	view, err := m.store.SetProjectControl(ctx, project, target, actor, input.Reason, m.now().UTC())
	return view, mapError(err)
}

// CancelInput requests deterministic bulk cancellation.
type CancelInput struct {
	Scope  string `json:"scope" enum:"pending,all"`
	Reason string `json:"reason"`
}

// SessionTermination reports one requested termination without claiming it
// succeeded: the error string is empty only when the kill service accepted
// the request.
type SessionTermination struct {
	SessionID domain.SessionID `json:"sessionId"`
	Error     string           `json:"error,omitempty"`
}

// CancelResult reports exactly what a bulk cancel did and, for cancel-all,
// which live workers were asked to stop.
type CancelResult struct {
	Result       domain.ProjectWorkCancellationResult `json:"result"`
	Terminations []SessionTermination                 `json:"terminations,omitempty"`
	KillService  bool                                 `json:"killServiceWired"`
}

// CancelWork cancels pending or all work in one transaction. Cancel-all then
// requests termination of every live worker holding a task attempt through
// the normal kill services, reporting partial failures instead of success.
func (m *Manager) CancelWork(ctx context.Context, actor domain.AdaptiveActor, project domain.ProjectID, input CancelInput) (CancelResult, error) {
	cancellation := domain.ProjectWorkCancellation{ProjectID: project, Scope: input.Scope, Actor: actor, Reason: input.Reason, Now: m.now().UTC()}
	result, err := m.store.CancelProjectWork(ctx, cancellation)
	if err != nil {
		return CancelResult{}, mapError(err)
	}
	cancel := CancelResult{Result: result, KillService: m.terminator != nil}
	if input.Scope != "all" || m.terminator == nil {
		return cancel, nil
	}
	sessions, err := m.store.ListProjectActiveAttemptSessions(ctx, project)
	if err != nil {
		return CancelResult{}, mapError(err)
	}
	for _, id := range sessions {
		termination := SessionTermination{SessionID: id}
		if err := m.terminator.Kill(ctx, id); err != nil {
			termination.Error = err.Error()
		}
		cancel.Terminations = append(cancel.Terminations, termination)
	}
	return cancel, nil
}

// NeedsHumanInput raises one structured request for human input.
type NeedsHumanInput struct {
	ReasonCode string `json:"reasonCode" enum:"credential_missing,approval_required,ambiguous_intent,provider_unavailable,recovery_inconclusive"`
	Detail     string `json:"detail"`
}

// Raise records a request on one task.
func (m *Manager) Raise(ctx context.Context, actor domain.AdaptiveActor, taskID string, input NeedsHumanInput) (domain.TaskNeedsHuman, error) {
	request := domain.TaskNeedsHuman{ID: m.newIdentity(), ReasonCode: input.ReasonCode, Detail: input.Detail, Actor: actor, CreatedAt: m.now().UTC()}
	item, err := m.store.RaiseTaskNeedsHuman(ctx, taskID, request)
	return item, mapError(err)
}

// ResolveInput closes the pending request.
type ResolveInput struct {
	Resolution string `json:"resolution"`
}

// Resolve records the human decision on one task's pending request.
func (m *Manager) Resolve(ctx context.Context, actor domain.AdaptiveActor, taskID string, input ResolveInput) (domain.TaskNeedsHuman, error) {
	resolution := domain.TaskNeedsHumanResolution{Resolution: input.Resolution, Actor: actor, ResolvedAt: m.now().UTC()}
	item, err := m.store.ResolveTaskNeedsHuman(ctx, taskID, resolution)
	return item, mapError(err)
}

// List pages a project's open requests.
func (m *Manager) List(ctx context.Context, project domain.ProjectID, afterID string, limit int) ([]domain.TaskNeedsHuman, error) {
	if limit < 1 || limit > 100 {
		return nil, apierr.Invalid("INVALID_NEEDS_HUMAN_PAGE", "Page limit must be between 1 and 100", nil)
	}
	items, err := m.store.ListProjectNeedsHuman(ctx, project, afterID, limit)
	return items, mapError(err)
}

// DryRun simulates one autonomous plan with writes and launches disabled.
func (m *Manager) DryRun(ctx context.Context, project domain.ProjectID, request domain.DryRunRequest) (domain.DryRunVerdict, error) {
	verdict, err := m.store.DryRunPlan(ctx, project, request)
	return verdict, mapError(err)
}

func newIdentityValue() string {
	return uuid.NewString()
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrProjectControlInvalid):
		return apierr.Invalid("INVALID_CONTROL_INPUT", err.Error(), nil)
	case errors.Is(err, ports.ErrProjectControlConflict):
		return apierr.Conflict("PROJECT_CONTROL_CONFLICT", "The control transition is not allowed from the project's current state", nil)
	case errors.Is(err, ports.ErrProjectControlNotFound):
		return apierr.NotFound("PROJECT_NOT_FOUND", "Project was not found")
	case errors.Is(err, ports.ErrTaskNeedsHumanInvalid):
		return apierr.Invalid("INVALID_NEEDS_HUMAN_INPUT", err.Error(), nil)
	case errors.Is(err, ports.ErrTaskNeedsHumanConflict):
		return apierr.Conflict("NEEDS_HUMAN_CONFLICT", "The task already has a pending request, or none is pending to resolve", nil)
	case errors.Is(err, ports.ErrTaskNeedsHumanNotFound):
		return apierr.NotFound("NEEDS_HUMAN_NOT_FOUND", "The task has no pending needs human request")
	case errors.Is(err, ports.ErrTaskNotFound):
		return apierr.NotFound("TASK_NOT_FOUND", "Task was not found")
	case errors.Is(err, ports.ErrDryRunInvalid):
		return apierr.Invalid("INVALID_DRY_RUN_INPUT", err.Error(), nil)
	default:
		return err
	}
}
