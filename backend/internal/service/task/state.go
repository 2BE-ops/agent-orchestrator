package task

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// State is a read projection. It is never stored on a session or used instead of
// transactional admission, and expiry never means that a worker has died.
type State struct {
	Phase                  string `json:"phase" enum:"planned,blocked,ready,leased,working,failed,completed,cancelling,cancelled"`
	Reason                 string `json:"reason"`
	CancelledBy            string `json:"cancelledBy,omitempty"`
	RequiresReconciliation bool   `json:"requiresReconciliation"`
}

// IntentInput requests admission changes without claiming process termination.
type IntentInput struct {
	Intent           string `json:"intent" enum:"run,cancel"`
	ExpectedVersion  int64  `json:"expectedVersion"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

// ChangeIntent retains active reservations for lifecycle cleanup/recovery.
func (m *Manager) ChangeIntent(ctx context.Context, actor domain.AdaptiveActor, id string, input IntentInput) (domain.TaskIntent, error) {
	result, err := m.store.ChangeTaskIntent(ctx, id, domain.TaskIntentChange{Intent: input.Intent, ExpectedVersion: input.ExpectedVersion, Mutation: domain.TaskMutation{Actor: actor, Reason: input.Reason, ExpectedRevision: input.ExpectedRevision}})
	return result, mapError(err)
}

// Intents pages audited control history independently of planning revisions.
func (m *Manager) Intents(ctx context.Context, id string, after int64, limit int) ([]domain.TaskIntent, error) {
	if err := validatePage(after, limit); err != nil {
		return nil, err
	}
	if _, err := m.store.GetAdaptiveTask(ctx, id); err != nil {
		return nil, mapError(err)
	}
	items, err := m.store.ListTaskIntents(ctx, id, after, limit)
	return items, mapError(err)
}

func (m *Manager) state(ctx context.Context, view View) (State, error) {
	cancelledBy, err := m.store.CancelledTaskAncestor(ctx, view.Task.ID)
	if err != nil {
		return State{}, err
	}
	if cancelledBy != "" {
		phase, reason := "cancelled", "Cancellation intent blocks new work"
		if view.Lease != nil {
			phase, reason = "cancelling", "Cancellation requested; worker ownership is still reserved"
		}
		return State{Phase: phase, Reason: reason, CancelledBy: cancelledBy, RequiresReconciliation: view.Lease != nil}, nil
	}
	if view.Lease != nil {
		state := State{Phase: "leased", Reason: "Exclusive task attempt is reserved", RequiresReconciliation: view.Lease.NeedsReconciliation(time.Now().UTC())}
		dispatch, ok, err := m.store.GetTaskWorkerDispatch(ctx, view.Lease.AttemptID)
		if err != nil || !ok {
			return state, err
		}
		rec, ok, err := m.store.GetSession(ctx, dispatch.SessionID)
		if err != nil {
			return State{}, err
		}
		if !ok {
			return State{}, ports.ErrTaskNotFound
		}
		_, pending, err := m.store.PendingTaskExecution(ctx, rec.ID)
		if err != nil {
			return State{}, err
		}
		if pending || rec.IsTerminated {
			state.RequiresReconciliation = true
			state.Reason = "Native execution or termination requires ownership reconciliation"
		} else if rec.Metadata.RuntimeLaunchID != "" || rec.Metadata.ControllerGeneration != "" {
			state.Phase, state.Reason = "working", "Task attempt has a connected worker generation"
		}
		if view.Completion.Verified {
			state.Phase, state.Reason = "completed", "Independent evidence passes current criteria; native ownership remains reserved"
		}
		return state, nil
	}
	if view.Completion.Verified {
		return State{Phase: "completed", Reason: view.Completion.Reason}, nil
	}
	if view.Criteria == nil {
		return State{Phase: "planned", Reason: "Acceptance criteria must be frozen before dispatch"}, nil
	}
	if len(view.Revision.Definition.Dependencies) > 0 {
		return State{Phase: "blocked", Reason: "Dependencies require independently verified completion"}, nil
	}
	attempts, err := m.store.ListTaskAttempts(ctx, view.Task.ID, 0, 100)
	if err != nil {
		return State{}, err
	}
	if len(attempts) >= view.Revision.Definition.MaxAttempts {
		return State{Phase: "failed", Reason: "Task attempt limit is exhausted"}, nil
	}
	return State{Phase: "ready", Reason: "Planning is ready; dispatch still requires scheduler admission"}, nil
}
