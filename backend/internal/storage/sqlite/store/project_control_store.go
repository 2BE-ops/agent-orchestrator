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

var _ ports.ProjectControlStore = (*Store)(nil)
var _ ports.TaskNeedsHumanStore = (*Store)(nil)

// projectControlBound enumerates every project task exactly once per bulk
// cancel; wider projects refuse instead of partially cancelling.
const projectControlBound = 10000

func projectControlReadError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrProjectControlNotFound
	}
	return err
}

func projectControlFromRow(row gen.AdaptiveProjectControl) (domain.ProjectControl, error) {
	control := domain.ProjectControl{ProjectID: domain.ProjectID(row.ProjectID), State: domain.ProjectControlState(row.State), Reason: row.Reason, UpdatedAt: row.UpdatedAt}
	err := json.Unmarshal([]byte(row.Actor), &control.Actor)
	return control, err
}

// controlSnapshot seals the raised request shape the insert trigger echoes.
type controlNeedsHumanSnapshot struct {
	ID         string               `json:"id"`
	TaskID     string               `json:"taskId"`
	ProjectID  string               `json:"projectId"`
	ReasonCode string               `json:"reasonCode"`
	Detail     string               `json:"detail"`
	Actor      domain.AdaptiveActor `json:"actor"`
	CreatedAt  time.Time            `json:"createdAt"`
	Resolution string               `json:"resolution"`
}

func needsHumanFromRow(row gen.AdaptiveTaskNeedsHuman) (domain.TaskNeedsHuman, error) {
	item := domain.TaskNeedsHuman{ID: row.ID, TaskID: row.TaskID, ProjectID: domain.ProjectID(row.ProjectID), ReasonCode: row.ReasonCode, Detail: row.Detail, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Actor), &item.Actor); err != nil {
		return domain.TaskNeedsHuman{}, err
	}
	if row.ResolvedAt.Valid {
		resolution := domain.TaskNeedsHumanResolution{ResolvedAt: row.ResolvedAt.Time}
		if err := json.Unmarshal([]byte(row.Resolution.String), &resolution); err != nil {
			return domain.TaskNeedsHuman{}, err
		}
		if err := json.Unmarshal([]byte(row.ResolvedBy.String), &resolution.Actor); err != nil {
			return domain.TaskNeedsHuman{}, err
		}
		item.Resolution = &resolution
	}
	return item, nil
}

func readProjectControl(ctx context.Context, q *gen.Queries, projectID domain.ProjectID) (domain.ProjectControl, error) {
	row, err := q.GetProjectControl(ctx, string(projectID))
	if errors.Is(err, sql.ErrNoRows) {
		// Absence is the durable default: the project has never been steered.
		return domain.ProjectControl{ProjectID: projectID, State: domain.ProjectRunning}, nil
	}
	if err != nil {
		return domain.ProjectControl{}, err
	}
	return projectControlFromRow(row)
}

// GetProjectControl derives the effective control state from durable facts.
// Draining reports paused once no active attempt remains; nothing is
// materialized at read time and a restart changes nothing.
func (s *Store) GetProjectControl(ctx context.Context, projectID domain.ProjectID) (domain.ProjectControlView, error) {
	control, err := readProjectControl(ctx, s.qr, projectID)
	if err != nil {
		return domain.ProjectControlView{}, err
	}
	active, err := s.qr.CountProjectActiveAttempts(ctx, string(projectID))
	if err != nil {
		return domain.ProjectControlView{}, err
	}
	effective := control.State
	if control.State == domain.ProjectDraining && active == 0 {
		effective = domain.ProjectPaused
	}
	return domain.ProjectControlView{Control: control, EffectiveState: effective, ActiveAttempts: active}, nil
}

// SetProjectControl applies one explicit control transition under the
// single-writer lock. The state machine is rechecked in the transaction, so a
// concurrent launch can never slip past a pause that has already returned.
func (s *Store) SetProjectControl(ctx context.Context, projectID domain.ProjectID, target domain.ProjectControlState, actor domain.AdaptiveActor, reason string, now time.Time) (domain.ProjectControlView, error) {
	control := domain.ProjectControl{ProjectID: projectID, State: target, Actor: actor, Reason: reason, UpdatedAt: now}
	if err := control.Validate(); err != nil {
		return domain.ProjectControlView{}, fmt.Errorf("%w: %w", ports.ErrProjectControlInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.ProjectControlView{}, err
	}
	defer s.writeMu.Unlock()
	var view domain.ProjectControlView
	err := s.inTx(ctx, "set project control", func(q *gen.Queries) error {
		project, err := q.GetProject(ctx, projectID)
		if err != nil {
			return projectControlReadError(err)
		}
		if project.ArchivedAt.Valid {
			return fmt.Errorf("%w: project is archived", ports.ErrProjectControlInvalid)
		}
		current, err := readProjectControl(ctx, q, projectID)
		if err != nil {
			return err
		}
		if !domain.ValidProjectControlTransition(current.State, target) {
			return fmt.Errorf("%w: %s -> %s", ports.ErrProjectControlConflict, current.State, target)
		}
		if current.State == target {
			view.Control = current
			return nil
		}
		encoded, err := json.Marshal(actor)
		if err != nil {
			return err
		}
		applied := domain.ProjectControl{ProjectID: projectID, State: target, Actor: actor, Reason: reason, UpdatedAt: now}
		if current.State == domain.ProjectRunning && current.UpdatedAt.IsZero() {
			err = q.InsertProjectControl(ctx, gen.InsertProjectControlParams{ProjectID: string(projectID), State: string(target), Actor: string(encoded), Reason: reason, UpdatedAt: now})
		} else {
			var rows int64
			rows, err = q.UpdateProjectControlState(ctx, gen.UpdateProjectControlStateParams{State: string(target), Actor: string(encoded), Reason: reason, UpdatedAt: now, ProjectID: string(projectID)})
			if err == nil && rows == 0 {
				err = ports.ErrProjectControlConflict
			}
		}
		if err != nil {
			return err
		}
		view.Control = applied
		return nil
	})
	if err != nil {
		return domain.ProjectControlView{}, err
	}
	active, err := s.qr.CountProjectActiveAttempts(ctx, string(projectID))
	if err != nil {
		return domain.ProjectControlView{}, err
	}
	view.ActiveAttempts = active
	view.EffectiveState = view.Control.State
	if view.Control.State == domain.ProjectDraining && active == 0 {
		view.EffectiveState = domain.ProjectPaused
	}
	return view, nil
}

// CancelProjectWork marks work cancelled in one transaction. Pending cancels
// unleased tasks; all additionally marks leased work cancelling so lifecycle
// cleanup and explicit termination can proceed. Live leases are retained and
// reported, never assumed stopped.
func (s *Store) CancelProjectWork(ctx context.Context, cancellation domain.ProjectWorkCancellation) (domain.ProjectWorkCancellationResult, error) {
	if err := cancellation.Validate(); err != nil {
		return domain.ProjectWorkCancellationResult{}, fmt.Errorf("%w: %w", ports.ErrProjectControlInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.ProjectWorkCancellationResult{}, err
	}
	defer s.writeMu.Unlock()
	result := domain.ProjectWorkCancellationResult{Scope: cancellation.Scope}
	err := s.inTx(ctx, "cancel project work", func(q *gen.Queries) error {
		if _, err := q.GetProject(ctx, cancellation.ProjectID); err != nil {
			return projectControlReadError(err)
		}
		var after string
		for {
			ids, err := q.ListAdaptiveTaskIDs(ctx, gen.ListAdaptiveTaskIDsParams{ProjectID: string(cancellation.ProjectID), ID: after, Limit: 101})
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
			if len(ids) > 100 {
				total := len(result.Cancelled) + len(result.Retained) + len(ids)
				if total > projectControlBound {
					return fmt.Errorf("%w: project exceeds the %d task cancellation bound", ports.ErrProjectControlInvalid, projectControlBound)
				}
			}
			for _, id := range ids {
				after = id
				task, err := q.GetAdaptiveTask(ctx, id)
				if err != nil {
					return taskReadError(err)
				}
				current, err := taskIntent(ctx, q, task)
				if err != nil {
					return err
				}
				if current.Intent == "cancel" {
					// Already cancelled work is not a new control event.
					continue
				}
				leased := true
				if _, err := q.GetActiveTaskLease(ctx, id); err != nil {
					if !errors.Is(err, sql.ErrNoRows) {
						return err
					}
					leased = false
				}
				if leased && cancellation.Scope == "pending" {
					result.Retained = append(result.Retained, id)
					continue
				}
				mutation := domain.TaskMutation{Actor: cancellation.Actor, Reason: cancellation.Reason, ExpectedRevision: task.Revision}
				actor, err := json.Marshal(cancellation.Actor)
				if err != nil {
					return err
				}
				if err := q.InsertTaskIntent(ctx, gen.InsertTaskIntentParams{TaskID: id, Version: current.Version + 1, TaskRevision: task.Revision, Intent: "cancel", Actor: string(actor), Reason: cancellation.Reason, CreatedAt: cancellation.Now}); err != nil {
					return err
				}
				if err := insertTaskAudit(ctx, q, id, task.Revision, "intent_cancel", mutation, cancellation.Now); err != nil {
					return err
				}
				result.Cancelled = append(result.Cancelled, id)
			}
			if len(ids) <= 100 {
				return nil
			}
		}
	})
	if err != nil {
		return domain.ProjectWorkCancellationResult{}, err
	}
	return result, nil
}

// ListProjectActiveAttemptSessions names the live worker sessions holding
// task attempts in one project. It is the termination candidate set for
// cancel-all; cancellation never assumes any of them stopped.
func (s *Store) ListProjectActiveAttemptSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionID, error) {
	return s.qr.ListProjectActiveAttemptSessions(ctx, string(projectID))
}

// RaiseTaskNeedsHuman records one structured request for human input. A task
// holds at most one pending request; the raise blocks new attempts on the task
// and its descendants while every unrelated branch stays schedulable.
func (s *Store) RaiseTaskNeedsHuman(ctx context.Context, id string, request domain.TaskNeedsHuman) (domain.TaskNeedsHuman, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.TaskNeedsHuman{}, err
	}
	defer s.writeMu.Unlock()
	err := s.inTx(ctx, "raise task needs human", func(q *gen.Queries) error {
		task, err := q.GetAdaptiveTask(ctx, id)
		if err != nil {
			return taskReadError(err)
		}
		// Scope identity comes from durable rows, never from the caller.
		request.TaskID = id
		request.ProjectID = domain.ProjectID(task.ProjectID)
		if err := request.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ports.ErrTaskNeedsHumanInvalid, err)
		}
		snapshot, hash, err := domain.TaskContent(controlNeedsHumanSnapshot{ID: request.ID, TaskID: request.TaskID, ProjectID: string(request.ProjectID), ReasonCode: request.ReasonCode, Detail: request.Detail, Actor: request.Actor, CreatedAt: request.CreatedAt})
		if err != nil {
			return err
		}
		if _, err := q.GetPendingTaskNeedsHuman(ctx, id); err == nil {
			return ports.ErrTaskNeedsHumanConflict
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		actor, err := json.Marshal(request.Actor)
		if err != nil {
			return err
		}
		if err := q.InsertTaskNeedsHuman(ctx, gen.InsertTaskNeedsHumanParams{ID: request.ID, TaskID: request.TaskID, ProjectID: string(request.ProjectID), ReasonCode: request.ReasonCode, Detail: request.Detail, Actor: string(actor), Snapshot: string(snapshot), ContentHash: hash, CreatedAt: request.CreatedAt}); err != nil {
			if isSQLiteUnique(err) {
				return ports.ErrTaskNeedsHumanConflict
			}
			return err
		}
		request.Resolution = nil
		return nil
	})
	if err != nil {
		return domain.TaskNeedsHuman{}, err
	}
	return request, nil
}

// ResolveTaskNeedsHuman closes the pending request exactly once. Resolving
// records the decision; it never rewrites why the request was raised.
func (s *Store) ResolveTaskNeedsHuman(ctx context.Context, taskID string, resolution domain.TaskNeedsHumanResolution) (domain.TaskNeedsHuman, error) {
	if err := resolution.Validate(); err != nil {
		return domain.TaskNeedsHuman{}, fmt.Errorf("%w: %w", ports.ErrTaskNeedsHumanInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.TaskNeedsHuman{}, err
	}
	defer s.writeMu.Unlock()
	var resolved domain.TaskNeedsHuman
	err := s.inTx(ctx, "resolve task needs human", func(q *gen.Queries) error {
		pending, err := q.GetPendingTaskNeedsHuman(ctx, taskID)
		if errors.Is(err, sql.ErrNoRows) {
			return ports.ErrTaskNeedsHumanNotFound
		}
		if err != nil {
			return err
		}
		item, err := needsHumanFromRow(pending)
		if err != nil {
			return err
		}
		sealed, err := json.Marshal(domain.TaskNeedsHumanResolution{Resolution: resolution.Resolution, Actor: resolution.Actor, ResolvedAt: resolution.ResolvedAt})
		if err != nil {
			return err
		}
		actor, err := json.Marshal(resolution.Actor)
		if err != nil {
			return err
		}
		rows, err := q.ResolveTaskNeedsHuman(ctx, gen.ResolveTaskNeedsHumanParams{ResolvedAt: sql.NullTime{Time: resolution.ResolvedAt, Valid: true}, Resolution: sql.NullString{String: string(sealed), Valid: true}, ResolvedBy: sql.NullString{String: string(actor), Valid: true}, TaskID: taskID})
		if err != nil {
			return err
		}
		if rows == 0 {
			return ports.ErrTaskNeedsHumanConflict
		}
		item.Resolution = &resolution
		resolved = item
		return nil
	})
	if err != nil {
		return domain.TaskNeedsHuman{}, err
	}
	return resolved, nil
}

// GetPendingTaskNeedsHuman reads a task's open request, if any.
func (s *Store) GetPendingTaskNeedsHuman(ctx context.Context, taskID string) (domain.TaskNeedsHuman, bool, error) {
	row, err := s.qr.GetPendingTaskNeedsHuman(ctx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskNeedsHuman{}, false, nil
	}
	if err != nil {
		return domain.TaskNeedsHuman{}, false, err
	}
	item, err := needsHumanFromRow(row)
	return item, true, err
}

// ListProjectNeedsHuman pages the project's open requests by id keyset.
func (s *Store) ListProjectNeedsHuman(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.TaskNeedsHuman, error) {
	if limit < 1 || limit > 100 || len(afterID) > 200 {
		return nil, ports.ErrProjectControlInvalid
	}
	ids, err := s.qr.ListProjectNeedsHumanIDs(ctx, gen.ListProjectNeedsHumanIDsParams{ProjectID: string(projectID), ID: afterID, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.TaskNeedsHuman, 0, len(ids))
	for _, id := range ids {
		row, err := s.qr.GetTaskNeedsHuman(ctx, id)
		if err != nil {
			return nil, err
		}
		item, err := needsHumanFromRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

// NeedsHumanTaskAncestor includes the task itself and its current parent
// chain, mirroring the cancelled-ancestor fence.
func (s *Store) NeedsHumanTaskAncestor(ctx context.Context, id string) (string, error) {
	ancestor, err := s.qr.NeedsHumanTaskAncestor(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return ancestor, err
}
