package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.TaskIntentStore = (*Store)(nil)

func taskIntentFromRow(row gen.AdaptiveTaskIntent) (domain.TaskIntent, error) {
	i := domain.TaskIntent{TaskID: row.TaskID, Version: row.Version, TaskRevision: row.TaskRevision, Intent: row.Intent, Reason: row.Reason, CreatedAt: row.CreatedAt}
	err := json.Unmarshal([]byte(row.Actor), &i.Actor)
	return i, err
}

func taskIntent(ctx context.Context, q *gen.Queries, task gen.AdaptiveTask) (domain.TaskIntent, error) {
	row, err := q.GetTaskIntent(ctx, task.ID)
	if errors.Is(err, sql.ErrNoRows) {
		initial := domain.TaskIntent{TaskID: task.ID, TaskRevision: 1, Intent: "run", CreatedAt: task.CreatedAt, Reason: "Initial task intent"}
		err := json.Unmarshal([]byte(task.CreatedBy), &initial.Actor)
		return initial, err
	}
	if err != nil {
		return domain.TaskIntent{}, err
	}
	return taskIntentFromRow(row)
}

// ChangeTaskIntent serializes with reservations, worker seeds and native guards.
// It retains every active lease; cancellation cleanup must reconcile ownership.
func (s *Store) ChangeTaskIntent(ctx context.Context, id string, change domain.TaskIntentChange) (domain.TaskIntent, error) {
	if err := validateTaskMutation(change.Mutation); err != nil {
		return domain.TaskIntent{}, err
	}
	if change.ExpectedVersion < 0 || change.Mutation.ExpectedRevision < 1 || (change.Intent != "run" && change.Intent != "cancel") {
		return domain.TaskIntent{}, ports.ErrTaskInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.TaskIntent{}, err
	}
	defer s.writeMu.Unlock()
	var result domain.TaskIntent
	err := s.inTx(ctx, "change task intent", func(q *gen.Queries) error {
		task, err := q.GetAdaptiveTask(ctx, id)
		if err != nil {
			return taskReadError(err)
		}
		if err := validateTaskActor(ctx, q, task.ProjectID, change.Mutation.Actor); err != nil {
			return err
		}
		current, err := taskIntent(ctx, q, task)
		if err != nil {
			return err
		}
		if task.Revision != change.Mutation.ExpectedRevision || current.Version != change.ExpectedVersion {
			return ports.ErrTaskConflict
		}
		// An exact no-op does not manufacture another control event.
		if current.Intent == change.Intent {
			result = current
			return nil
		}
		actor, err := json.Marshal(change.Mutation.Actor)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		result = domain.TaskIntent{TaskID: id, Version: current.Version + 1, TaskRevision: task.Revision, Intent: change.Intent, Actor: change.Mutation.Actor, Reason: change.Mutation.Reason, CreatedAt: now}
		if err := q.InsertTaskIntent(ctx, gen.InsertTaskIntentParams{TaskID: id, Version: result.Version, TaskRevision: task.Revision, Intent: change.Intent, Actor: string(actor), Reason: result.Reason, CreatedAt: now}); err != nil {
			return err
		}
		return insertTaskAudit(ctx, q, id, task.Revision, "intent_"+change.Intent, change.Mutation, now)
	})
	return result, err
}

// GetTaskIntent includes the implicit initial intent without inserting history.
func (s *Store) GetTaskIntent(ctx context.Context, id string) (domain.TaskIntent, error) {
	task, err := s.qr.GetAdaptiveTask(ctx, id)
	if err != nil {
		return domain.TaskIntent{}, taskReadError(err)
	}
	return taskIntent(ctx, s.qr, task)
}

// ListTaskIntents returns explicit historical instructions in version order.
func (s *Store) ListTaskIntents(ctx context.Context, id string, after int64, limit int) ([]domain.TaskIntent, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListTaskIntents(ctx, gen.ListTaskIntentsParams{TaskID: id, Version: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.TaskIntent, 0, len(rows))
	for _, row := range rows {
		item, err := taskIntentFromRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

// CancelledTaskAncestor includes the task itself and its current parent chain.
func (s *Store) CancelledTaskAncestor(ctx context.Context, id string) (string, error) {
	ancestor, err := s.qr.CancelledTaskAncestor(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return ancestor, err
}

func requireTaskRunIntent(ctx context.Context, q *gen.Queries, id string) error {
	_, err := q.CancelledTaskAncestor(ctx, id)
	if err == nil {
		return ports.ErrTaskLeaseFenced
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

// requireTaskNeedsHumanClear fences new attempts behind a pending request on
// the task or one of its ancestors. Manager assessment may still observe a
// needs-human task; only admission is fenced.
func requireTaskNeedsHumanClear(ctx context.Context, q *gen.Queries, id string) error {
	_, err := q.NeedsHumanTaskAncestor(ctx, id)
	if err == nil {
		return ports.ErrTaskNeedsHumanFenced
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

// requireProjectAdmissionsOpen fences new attempts while a project control
// state other than running holds. Idempotent dispatch replays bypass this:
// only genuinely new admissions respect the fence.
func requireProjectAdmissionsOpen(ctx context.Context, q *gen.Queries, projectID string) error {
	control, err := q.GetProjectControl(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if domain.ProjectControlState(control.State) != domain.ProjectRunning {
		return fmt.Errorf("%w: project is %s", ports.ErrProjectAdmissionsFenced, control.State)
	}
	return nil
}
