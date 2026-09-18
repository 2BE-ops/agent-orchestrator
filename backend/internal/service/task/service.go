// Package task owns project work intent and immutable acceptance history. Native
// execution is admitted separately through the shared deterministic scheduler.
package task

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Store is the daemon persistence boundary used by human and controller tools.
type Store interface {
	ports.AdaptiveTaskStore
	ports.TaskLeaseStore
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	GetRegistryEntry(context.Context, string) (domain.RegistryEntry, error)
	GetRegistryVersion(context.Context, string, int64) (domain.RegistryVersion, error)
}

// Manager applies shared validation without starting worker processes.
type Manager struct{ store Store }

// New builds the task service over the existing daemon store.
func New(store Store) *Manager { return &Manager{store: store} }

// CreateInput authors work with optional criteria; criteria are required to lease.
type CreateInput struct {
	Definition domain.TaskDefinition      `json:"definition"`
	Criteria   *domain.AcceptanceCriteria `json:"criteria,omitempty"`
	Reason     string                     `json:"reason"`
}

// RevisionInput appends work intent without changing historical attempts.
type RevisionInput struct {
	Definition       domain.TaskDefinition `json:"definition"`
	ExpectedRevision int64                 `json:"expectedRevision"`
	Reason           string                `json:"reason"`
}

// CriteriaInput versions success criteria with an explicit reason and fence.
type CriteriaInput struct {
	Criteria         domain.AcceptanceCriteria `json:"criteria"`
	ExpectedRevision int64                     `json:"expectedRevision"`
	Reason           string                    `json:"reason"`
}

// View combines exact current planning with independently observed lease facts.
type View struct {
	Task     domain.AdaptiveTask               `json:"task"`
	Revision domain.TaskRevision               `json:"revision"`
	Criteria *domain.AcceptanceCriteriaVersion `json:"criteria,omitempty"`
	Lease    *domain.TaskLease                 `json:"lease,omitempty"`
}

// AttemptView includes retained worker association and current ownership facts.
type AttemptView struct {
	Attempt  domain.TaskAttempt         `json:"attempt"`
	Lease    domain.TaskLease           `json:"lease"`
	Dispatch *domain.TaskWorkerDispatch `json:"dispatch,omitempty"`
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrTaskNotFound), errors.Is(err, ports.ErrRegistryNotFound):
		return apierr.NotFound("TASK_NOT_FOUND", "Task, project or selected definition was not found")
	case errors.Is(err, ports.ErrTaskForbidden):
		return apierr.Forbidden("TASK_PLANNING_FORBIDDEN", "Only the user or owning orchestrator may change task planning")
	case errors.Is(err, ports.ErrTaskConflict):
		return apierr.Conflict("TASK_REVISION_CONFLICT", "Task changed; reload its current revision", nil)
	case errors.Is(err, ports.ErrTaskLeaseFenced):
		return apierr.Conflict("TASK_LEASE_FENCED", "Task ownership requires reconciliation", nil)
	case errors.Is(err, ports.ErrTaskInvalid):
		return apierr.Invalid("INVALID_TASK", err.Error(), nil)
	default:
		return err
	}
}

func validatePage(after int64, limit int) error {
	if after < 0 || limit < 1 || limit > 100 {
		return apierr.Invalid("INVALID_TASK_PAGE", "Cursor must be non-negative and limit between 1 and 100", nil)
	}
	return nil
}

func (m *Manager) project(ctx context.Context, id domain.ProjectID) error {
	_, ok, err := m.store.GetProject(ctx, string(id))
	if err != nil {
		return err
	}
	if !ok {
		return mapError(ports.ErrTaskNotFound)
	}
	return nil
}

func (m *Manager) selectedWorker(ctx context.Context, definition domain.TaskDefinition) error {
	if err := definition.Validate(); err != nil {
		return apierr.Invalid("INVALID_TASK", err.Error(), nil)
	}
	if definition.RequestedWorker == nil {
		return nil
	}
	selection := definition.RequestedWorker
	entry, err := m.store.GetRegistryEntry(ctx, selection.AgentTypeID)
	if err != nil {
		return mapError(err)
	}
	if entry.Kind != domain.RegistryAgentType {
		return mapError(ports.ErrTaskNotFound)
	}
	version := selection.Version
	if version == 0 {
		version = entry.ActiveVersion
	}
	_, err = m.store.GetRegistryVersion(ctx, entry.ID, version)
	return mapError(err)
}

// Create records actor provenance from trusted action context, never input JSON.
func (m *Manager) Create(ctx context.Context, actor domain.AdaptiveActor, projectID domain.ProjectID, input CreateInput) (View, error) {
	if err := m.selectedWorker(ctx, input.Definition); err != nil {
		return View{}, err
	}
	task, err := m.store.CreateAdaptiveTask(ctx, uuid.NewString(), projectID, input.Definition, input.Criteria, domain.TaskMutation{Actor: actor, Reason: input.Reason})
	if err != nil {
		return View{}, mapError(err)
	}
	return m.view(ctx, task)
}

func (m *Manager) view(ctx context.Context, task domain.AdaptiveTask) (View, error) {
	revision, err := m.store.GetTaskRevision(ctx, task.ID, task.Revision)
	if err != nil {
		return View{}, mapError(err)
	}
	view := View{Task: task, Revision: revision}
	if revision.CriteriaVersion > 0 {
		criteria, err := m.store.GetAcceptanceCriteria(ctx, task.ID, revision.CriteriaVersion)
		if err != nil {
			return View{}, mapError(err)
		}
		view.Criteria = &criteria
	}
	lease, ok, err := m.store.GetActiveTaskLease(ctx, task.ID)
	if err != nil {
		return View{}, mapError(err)
	}
	if ok {
		view.Lease = &lease
	}
	return view, nil
}

// Get resolves current revision once, then reads only exact immutable content.
func (m *Manager) Get(ctx context.Context, id string) (View, error) {
	task, err := m.store.GetAdaptiveTask(ctx, id)
	if err != nil {
		return View{}, mapError(err)
	}
	return m.view(ctx, task)
}

// List returns bounded project-scoped task views in stable ID order.
func (m *Manager) List(ctx context.Context, projectID domain.ProjectID, after string, limit int) ([]View, error) {
	if err := validatePage(0, limit); err != nil {
		return nil, err
	}
	if err := m.project(ctx, projectID); err != nil {
		return nil, err
	}
	tasks, err := m.store.ListAdaptiveTasks(ctx, projectID, after, limit)
	if err != nil {
		return nil, mapError(err)
	}
	result := make([]View, 0, len(tasks))
	for _, task := range tasks {
		view, err := m.view(ctx, task)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}

// Revise changes future work intent; leased attempts keep their exact pins.
func (m *Manager) Revise(ctx context.Context, actor domain.AdaptiveActor, id string, input RevisionInput) (domain.TaskRevision, error) {
	if err := m.selectedWorker(ctx, input.Definition); err != nil {
		return domain.TaskRevision{}, err
	}
	revision, err := m.store.ReviseAdaptiveTask(ctx, id, input.Definition, domain.TaskMutation{Actor: actor, ExpectedRevision: input.ExpectedRevision, Reason: input.Reason})
	return revision, mapError(err)
}

// ReviseCriteria changes future success criteria without altering old attempts.
func (m *Manager) ReviseCriteria(ctx context.Context, actor domain.AdaptiveActor, id string, input CriteriaInput) (domain.TaskRevision, error) {
	revision, err := m.store.ReviseAcceptanceCriteria(ctx, id, input.Criteria, domain.TaskMutation{Actor: actor, ExpectedRevision: input.ExpectedRevision, Reason: input.Reason})
	return revision, mapError(err)
}

// Revision reads exact historical intent for comparison and worker context.
func (m *Manager) Revision(ctx context.Context, id string, number int64) (domain.TaskRevision, error) {
	if number < 1 {
		return domain.TaskRevision{}, apierr.Invalid("INVALID_TASK_VERSION", "Version must be positive", nil)
	}
	r, err := m.store.GetTaskRevision(ctx, id, number)
	return r, mapError(err)
}

// Criteria reads an exact retained definition of success.
func (m *Manager) Criteria(ctx context.Context, id string, number int64) (domain.AcceptanceCriteriaVersion, error) {
	if number < 1 {
		return domain.AcceptanceCriteriaVersion{}, apierr.Invalid("INVALID_TASK_VERSION", "Version must be positive", nil)
	}
	c, err := m.store.GetAcceptanceCriteria(ctx, id, number)
	return c, mapError(err)
}

// Revisions returns immutable intent history with bounded pagination.
func (m *Manager) Revisions(ctx context.Context, id string, after int64, limit int) ([]domain.TaskRevision, error) {
	if err := m.historyPage(ctx, id, after, limit); err != nil {
		return nil, err
	}
	rows, err := m.store.ListTaskRevisions(ctx, id, after, limit)
	return rows, mapError(err)
}

// Audit includes planning, reservation and recovery actions across all revisions.
func (m *Manager) Audit(ctx context.Context, id string, after int64, limit int) ([]domain.TaskAudit, error) {
	if err := m.historyPage(ctx, id, after, limit); err != nil {
		return nil, err
	}
	rows, err := m.store.ListTaskAudit(ctx, id, after, limit)
	return rows, mapError(err)
}

// Attempts exposes retained dispatch history without granting lease mutations.
func (m *Manager) Attempts(ctx context.Context, id string, after int64, limit int) ([]AttemptView, error) {
	if err := m.historyPage(ctx, id, after, limit); err != nil {
		return nil, err
	}
	attempts, err := m.store.ListTaskAttempts(ctx, id, after, limit)
	if err != nil {
		return nil, mapError(err)
	}
	result := make([]AttemptView, 0, len(attempts))
	for _, attempt := range attempts {
		lease, err := m.store.GetTaskLease(ctx, attempt.ID)
		if err != nil {
			return nil, mapError(err)
		}
		view := AttemptView{Attempt: attempt, Lease: lease}
		dispatch, ok, err := m.store.GetTaskWorkerDispatch(ctx, attempt.ID)
		if err != nil {
			return nil, mapError(err)
		}
		if ok {
			view.Dispatch = &dispatch
		}
		result = append(result, view)
	}
	return result, nil
}

func (m *Manager) historyPage(ctx context.Context, id string, after int64, limit int) error {
	if err := validatePage(after, limit); err != nil {
		return err
	}
	_, err := m.store.GetAdaptiveTask(ctx, id)
	return mapError(err)
}
