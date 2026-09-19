package orchestrator

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Store combines goal/planning persistence with the session and project reads
// the service needs to attribute native actions. The service never opens
// storage or starts processes itself.
type Store interface {
	ports.ProjectGoalStore
	ports.OrchestratorPlanStore
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error)
}

// Manager coordinates the durable orchestrator protocol: the project goal,
// sealed native planning actions and deterministically verified completion.
type Manager struct {
	store Store
}

// New builds the orchestrator service over the existing daemon store.
func New(store Store) *Manager {
	return &Manager{store: store}
}

// GoalInput sets a new user-authored goal wording.
type GoalInput struct {
	Goal   string `json:"goal"`
	Reason string `json:"reason"`
}

// NativeGoal is the read an orchestrator session starts each cycle from: the
// current goal wording plus the live generation its submissions must carry.
type NativeGoal struct {
	Goal             domain.ProjectGoalVersion `json:"goal"`
	SourceGeneration string                    `json:"sourceGeneration"`
	SessionID        domain.SessionID          `json:"sessionId"`
}

// PlanInput is native planning output and retry identity, never
// caller-supplied authority. Task identity for create_task is minted here.
type PlanInput struct {
	SourceGeneration string                        `json:"sourceGeneration"`
	IdempotencyKey   string                        `json:"idempotencyKey"`
	Action           domain.OrchestratorPlanAction `json:"action"`
}

// PlanReceipt retains the sealed planning outcome and whether it was created.
type PlanReceipt struct {
	Receipt domain.OrchestratorPlanReceipt `json:"receipt"`
	Created bool                           `json:"created"`
}

// CompleteInput is a native goal-completion submission.
type CompleteInput struct {
	SourceGeneration string `json:"sourceGeneration"`
	GoalVersion      int64  `json:"goal_version"`
	Summary          string `json:"summary"`
	Reason           string `json:"reason"`
}

// SetGoal appends a user-authored goal version.
func (m *Manager) SetGoal(ctx context.Context, actor domain.AdaptiveActor, project domain.ProjectID, input GoalInput) (domain.ProjectGoalVersion, error) {
	if err := m.project(ctx, project); err != nil {
		return domain.ProjectGoalVersion{}, err
	}
	version, err := m.store.SetProjectGoal(ctx, project, input.Goal, input.Reason, actor)
	return version, mapError(err)
}

// Goal reads the current goal wording.
func (m *Manager) Goal(ctx context.Context, project domain.ProjectID) (domain.ProjectGoalVersion, error) {
	if err := m.project(ctx, project); err != nil {
		return domain.ProjectGoalVersion{}, err
	}
	version, err := m.store.GetProjectGoal(ctx, project)
	return version, mapError(err)
}

// GoalVersions pages immutable goal history (after >= 0, limit 1-100).
func (m *Manager) GoalVersions(ctx context.Context, project domain.ProjectID, after int64, limit int) ([]domain.ProjectGoalVersion, error) {
	if err := m.project(ctx, project); err != nil {
		return nil, err
	}
	if after < 0 || limit < 1 || limit > 100 {
		return nil, apierr.Invalid("INVALID_GOAL_PAGE", "Cursor must be non-negative and limit between 1 and 100", nil)
	}
	items, err := m.store.ListProjectGoalVersions(ctx, project, after, limit)
	return items, mapError(err)
}

// Completions pages retained goal completions (limit 1-100).
func (m *Manager) Completions(ctx context.Context, project domain.ProjectID, afterID string, limit int) ([]domain.ProjectGoalCompletion, error) {
	if err := m.project(ctx, project); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, apierr.Invalid("INVALID_GOAL_PAGE", "Completion limit must be between 1 and 100", nil)
	}
	items, err := m.store.ListProjectGoalCompletions(ctx, project, afterID, limit)
	return items, mapError(err)
}

// NativeGoal returns the current goal plus the live orchestrator generation.
// A project without a goal or without an active orchestrator answers with a
// typed error rather than an empty protocol surface.
func (m *Manager) NativeGoal(ctx context.Context, project domain.ProjectID) (NativeGoal, error) {
	if err := m.project(ctx, project); err != nil {
		return NativeGoal{}, err
	}
	goal, err := m.store.GetProjectGoal(ctx, project)
	if err != nil {
		return NativeGoal{}, mapError(err)
	}
	rec, err := m.activeOrchestrator(ctx, project)
	if err != nil {
		return NativeGoal{}, err
	}
	return NativeGoal{Goal: goal, SourceGeneration: orchestratorGeneration(rec), SessionID: rec.ID}, nil
}

// Plan executes one native planning action as the project's live orchestrator.
// The store transaction rechecks session authority while sealing the receipt.
func (m *Manager) Plan(ctx context.Context, project domain.ProjectID, input PlanInput) (PlanReceipt, error) {
	if err := m.project(ctx, project); err != nil {
		return PlanReceipt{}, err
	}
	rec, err := m.activeOrchestrator(ctx, project)
	if err != nil {
		return PlanReceipt{}, err
	}
	if orchestratorGeneration(rec) != input.SourceGeneration {
		return PlanReceipt{}, apierr.Conflict("ORCHESTRATOR_OWNER_CHANGED", "The planning source is not the current live orchestrator generation; re-read the goal", nil)
	}
	newTaskID := ""
	if input.Action.Action == "create_task" {
		newTaskID = uuid.NewString()
	}
	submission := domain.OrchestratorPlanSubmission{ReceiptID: uuid.NewString(), ProjectID: project, SessionID: rec.ID, IdempotencyKey: input.IdempotencyKey, Action: input.Action, NewTaskID: newTaskID, Now: time.Now().UTC()}
	receipt, created, err := m.store.ApplyOrchestratorPlan(ctx, submission)
	if err != nil {
		return PlanReceipt{}, mapError(err)
	}
	return PlanReceipt{Receipt: receipt, Created: created}, nil
}

// Complete submits a native goal-completion assessment. AO verifies every
// project task against durable facts before retaining the decision.
func (m *Manager) Complete(ctx context.Context, project domain.ProjectID, input CompleteInput) (domain.ProjectGoalCompletion, bool, error) {
	if err := m.project(ctx, project); err != nil {
		return domain.ProjectGoalCompletion{}, false, err
	}
	rec, err := m.activeOrchestrator(ctx, project)
	if err != nil {
		return domain.ProjectGoalCompletion{}, false, err
	}
	if orchestratorGeneration(rec) != input.SourceGeneration {
		return domain.ProjectGoalCompletion{}, false, apierr.Conflict("ORCHESTRATOR_OWNER_CHANGED", "The completion source is not the current live orchestrator generation; re-read the goal", nil)
	}
	request := domain.ProjectGoalCompletionRequest{ID: uuid.NewString(), ProjectID: project, GoalVersion: input.GoalVersion, Summary: input.Summary, Actor: domain.AdaptiveActor{Kind: "ORCHESTRATOR", ID: string(rec.ID), SessionID: rec.ID}, Reason: input.Reason, Now: time.Now().UTC()}
	completion, created, err := m.store.CompleteProjectGoal(ctx, request)
	return completion, created, mapError(err)
}

// Receipts pages retained planning receipts (limit 1-100).
func (m *Manager) Receipts(ctx context.Context, project domain.ProjectID, afterID string, limit int) ([]domain.OrchestratorPlanReceipt, error) {
	if err := m.project(ctx, project); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, apierr.Invalid("INVALID_ORCHESTRATOR_PAGE", "Receipt limit must be between 1 and 100", nil)
	}
	items, err := m.store.ListOrchestratorPlanReceipts(ctx, project, afterID, limit)
	return items, mapError(err)
}

// Receipt reads one retained planning receipt.
func (m *Manager) Receipt(ctx context.Context, project domain.ProjectID, id string) (domain.OrchestratorPlanReceipt, error) {
	if err := m.project(ctx, project); err != nil {
		return domain.OrchestratorPlanReceipt{}, err
	}
	receipt, err := m.store.GetOrchestratorPlanReceipt(ctx, project, id)
	return receipt, mapError(err)
}

func (m *Manager) project(ctx context.Context, id domain.ProjectID) error {
	if id == "" {
		return apierr.Invalid("INVALID_PROJECT", "A project identity is required", nil)
	}
	_, ok, err := m.store.GetProject(ctx, string(id))
	if err != nil {
		return err
	}
	if !ok {
		return apierr.NotFound("PROJECT_NOT_FOUND", "Project was not found")
	}
	return nil
}

// activeOrchestrator resolves the project's live orchestrator session. Spawn
// semantics keep at most one active orchestrator per project; when a
// replacement overlaps, the newest live session owns planning.
func (m *Manager) activeOrchestrator(ctx context.Context, project domain.ProjectID) (domain.SessionRecord, error) {
	sessions, err := m.store.ListSessions(ctx, project)
	if err != nil {
		return domain.SessionRecord{}, err
	}
	live := make([]domain.SessionRecord, 0, len(sessions))
	for _, session := range sessions {
		if session.Kind == domain.KindOrchestrator && !session.IsTerminated && session.Activity.State != domain.ActivityExited {
			live = append(live, session)
		}
	}
	if len(live) == 0 {
		return domain.SessionRecord{}, apierr.NotFound("ORCHESTRATOR_NOT_FOUND", "This project has no live orchestrator session to attribute native planning")
	}
	sort.Slice(live, func(i, j int) bool {
		if live[i].CreatedAt.Equal(live[j].CreatedAt) {
			return live[i].ID > live[j].ID
		}
		return live[i].CreatedAt.After(live[j].CreatedAt)
	})
	return live[0], nil
}

func orchestratorGeneration(rec domain.SessionRecord) string {
	owner := rec.ControllerOwner()
	if owner.Mode == domain.SessionModeChat {
		return owner.ControllerGeneration
	}
	return owner.RuntimeLaunchID
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrGoalNotFound):
		return apierr.NotFound("GOAL_NOT_FOUND", "This project has no current goal")
	case errors.Is(err, ports.ErrGoalConflict):
		return apierr.Conflict("GOAL_VERSION_CONFLICT", "The goal changed; re-read the current version before completing", nil)
	case errors.Is(err, ports.ErrGoalIncomplete):
		var blockers domain.ProjectGoalBlockers
		details := map[string]any{}
		if errors.As(err, &blockers) {
			items := make([]map[string]any, 0, len(blockers.Items))
			for _, item := range blockers.Items {
				items = append(items, map[string]any{"taskId": item.TaskID, "revision": item.Revision, "state": item.State, "reason": item.Reason})
			}
			details["blockers"] = items
		}
		return apierr.Conflict("GOAL_INCOMPLETE", "Durable task facts do not satisfy the goal yet", details)
	case errors.Is(err, ports.ErrGoalInvalid):
		return apierr.Invalid("INVALID_GOAL", err.Error(), nil)
	case errors.Is(err, ports.ErrOrchestratorPlanNotFound):
		return apierr.NotFound("ORCHESTRATOR_RECEIPT_NOT_FOUND", "Planning receipt was not found in this project")
	case errors.Is(err, ports.ErrOrchestratorPlanConflict):
		return apierr.Conflict("ORCHESTRATOR_PLAN_CONFLICT", "Planning action changed under the same idempotency key; inspect retained receipts", nil)
	case errors.Is(err, ports.ErrTaskNotFound):
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
