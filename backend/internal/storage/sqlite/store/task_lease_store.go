package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.TaskLeaseStore = (*Store)(nil)

func taskAttemptFromRow(row gen.AdaptiveTaskAttempt) (domain.TaskAttempt, error) {
	result := domain.TaskAttempt{ID: row.ID, TaskID: row.TaskID, TaskRevision: row.TaskRevision, CriteriaVersion: row.CriteriaVersion, Number: row.Number, LaunchIntentID: row.LaunchIntentID, Reason: row.Reason, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Dependencies), &result.Dependencies); err != nil {
		return result, err
	}
	err := json.Unmarshal([]byte(row.Actor), &result.Actor)
	return result, err
}

func taskLeaseFromRow(row gen.AdaptiveTaskLease) domain.TaskLease {
	l := domain.TaskLease{TaskLeaseToken: domain.TaskLeaseToken{AttemptID: row.AttemptID, Generation: row.Generation, HolderID: row.HolderID}, TaskID: row.TaskID, HeartbeatAt: row.HeartbeatAt, ExpiresAt: row.ExpiresAt, ReleaseReason: row.ReleaseReason}
	if row.LastActivityAt.Valid {
		l.LastActivityAt = &row.LastActivityAt.Time
	}
	if row.ReleasedAt.Valid {
		l.ReleasedAt = &row.ReleasedAt.Time
	}
	return l
}

func taskLeaseFenced(row gen.AdaptiveTaskLease, token domain.TaskLeaseToken) bool {
	return row.ReleasedAt.Valid || row.Generation != token.Generation || row.HolderID != token.HolderID || row.AttemptID != token.AttemptID
}

// ReserveTask persists intent before external launch. Timeout never opens the
// unique reservation; only lifecycle-confirmed release permits a new attempt.
func (s *Store) ReserveTask(ctx context.Context, request domain.TaskReservation) (domain.TaskAttempt, domain.TaskLease, error) {
	if err := validateTaskMutation(request.Mutation); err != nil {
		return domain.TaskAttempt{}, domain.TaskLease{}, err
	}
	if err := request.Validate(); err != nil {
		return domain.TaskAttempt{}, domain.TaskLease{}, fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.TaskAttempt{}, domain.TaskLease{}, err
	}
	defer s.writeMu.Unlock()
	var attempt domain.TaskAttempt
	var lease domain.TaskLease
	err := s.inTx(ctx, "reserve adaptive task", func(q *gen.Queries) error {
		prior, err := q.GetTaskAttemptByIntent(ctx, request.LaunchIntentID)
		if err == nil {
			attempt, err = taskAttemptFromRow(prior)
			if err != nil {
				return err
			}
			if attempt.TaskID != request.TaskID || attempt.TaskRevision != request.Mutation.ExpectedRevision || attempt.Actor != request.Mutation.Actor || attempt.Reason != request.Mutation.Reason {
				return ports.ErrTaskConflict
			}
			row, err := q.GetTaskLease(ctx, attempt.ID)
			if err != nil {
				return err
			}
			if row.HolderID != request.HolderID {
				return ports.ErrTaskLeaseFenced
			}
			lease = taskLeaseFromRow(row)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		task, err := q.GetAdaptiveTask(ctx, request.TaskID)
		if err != nil {
			return taskReadError(err)
		}
		if task.Revision != request.Mutation.ExpectedRevision {
			return ports.ErrTaskConflict
		}
		if err := validateTaskActor(ctx, q, task.ProjectID, request.Mutation.Actor); err != nil {
			return err
		}
		if err := requireTaskRunIntent(ctx, q, task.ID); err != nil {
			return err
		}
		if err := requireTaskNeedsHumanClear(ctx, q, task.ID); err != nil {
			return err
		}
		if err := requireProjectAdmissionsOpen(ctx, q, task.ProjectID); err != nil {
			return err
		}
		if _, err := q.GetActiveTaskLease(ctx, request.TaskID); err == nil {
			return ports.ErrTaskLeaseFenced
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		row, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: task.ID, Number: task.Revision})
		if err != nil {
			return err
		}
		revision, err := taskRevisionFromRow(row)
		if err != nil {
			return err
		}
		if revision.CriteriaVersion < 1 {
			return fmt.Errorf("%w: acceptance criteria must be frozen before reservation", ports.ErrTaskInvalid)
		}
		number, err := q.NextTaskAttemptNumber(ctx, task.ID)
		if err != nil {
			return err
		}
		if number > int64(revision.Definition.MaxAttempts) {
			return fmt.Errorf("%w: task attempt limit reached", ports.ErrTaskInvalid)
		}
		dependencies := make([]domain.TaskRevisionRef, 0, len(revision.Definition.Dependencies))
		for _, id := range revision.Definition.Dependencies {
			dependency, err := q.GetAdaptiveTask(ctx, id)
			if err != nil {
				return err
			}
			pin, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: id, Number: dependency.Revision})
			if err != nil {
				return err
			}
			dependencies = append(dependencies, domain.TaskRevisionRef{TaskID: id, Revision: pin.Number, ContentHash: pin.ContentHash})
		}
		encodedDependencies, err := json.Marshal(dependencies)
		if err != nil {
			return err
		}
		actor, err := json.Marshal(request.Mutation.Actor)
		if err != nil {
			return err
		}
		if err := q.InsertTaskAttempt(ctx, gen.InsertTaskAttemptParams{ID: request.ID, TaskID: task.ID, TaskRevision: task.Revision, CriteriaVersion: revision.CriteriaVersion, Number: number, LaunchIntentID: request.LaunchIntentID, Dependencies: string(encodedDependencies), Actor: string(actor), Reason: request.Mutation.Reason, CreatedAt: request.Now}); err != nil {
			if isSQLiteUnique(err) {
				return ports.ErrTaskConflict
			}
			return err
		}
		if err := q.InsertTaskLease(ctx, gen.InsertTaskLeaseParams{AttemptID: request.ID, TaskID: task.ID, Generation: number, HolderID: request.HolderID, HeartbeatAt: request.Now, ExpiresAt: request.Now.Add(request.TTL)}); err != nil {
			return err
		}
		if err := insertTaskAudit(ctx, q, task.ID, task.Revision, "reserved", request.Mutation, request.Now); err != nil {
			return err
		}
		attempt = domain.TaskAttempt{ID: request.ID, TaskID: task.ID, TaskRevision: task.Revision, CriteriaVersion: revision.CriteriaVersion, Number: number, LaunchIntentID: request.LaunchIntentID, Dependencies: dependencies, Actor: request.Mutation.Actor, Reason: request.Mutation.Reason, CreatedAt: request.Now}
		lease = domain.TaskLease{TaskLeaseToken: domain.TaskLeaseToken{AttemptID: request.ID, Generation: number, HolderID: request.HolderID}, TaskID: task.ID, HeartbeatAt: request.Now, ExpiresAt: request.Now.Add(request.TTL)}
		return nil
	})
	return attempt, lease, err
}

// RenewTaskLease records heartbeat separately from meaningful work. An expired
// lease requires explicit reconciliation even when its former owner returns.
func (s *Store) RenewTaskLease(ctx context.Context, token domain.TaskLeaseToken, now time.Time, ttl time.Duration, activity *time.Time) error {
	if now.IsZero() || ttl < time.Second || ttl > 5*time.Minute || (activity != nil && (activity.IsZero() || activity.After(now))) {
		return ports.ErrTaskInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "renew task lease", func(q *gen.Queries) error {
		row, err := q.GetTaskLease(ctx, token.AttemptID)
		if err != nil {
			return taskReadError(err)
		}
		if taskLeaseFenced(row, token) || !now.Before(row.ExpiresAt) || now.Before(row.HeartbeatAt) {
			return ports.ErrTaskLeaseFenced
		}
		activityTime := sql.NullTime{}
		if activity != nil {
			if row.LastActivityAt.Valid && activity.Before(row.LastActivityAt.Time) {
				return ports.ErrTaskInvalid
			}
			attempt, err := q.GetTaskAttempt(ctx, token.AttemptID)
			if err != nil {
				return err
			}
			if activity.Before(attempt.CreatedAt) {
				return ports.ErrTaskInvalid
			}
			activityTime = sql.NullTime{Time: *activity, Valid: true}
		}
		_, err = q.RenewTaskLease(ctx, gen.RenewTaskLeaseParams{AttemptID: token.AttemptID, Generation: token.Generation, HolderID: token.HolderID, Now: now, ExpiresAt: now.Add(ttl), ActivityAt: activityTime})
		return err
	})
}

// RecoverTaskLease transfers an existing reservation after the service confirms
// attachment to the exact live session owner; no new worker is admitted.
func (s *Store) RecoverTaskLease(ctx context.Context, recovery domain.TaskLeaseRecovery) error {
	return s.changeTaskLease(ctx, recovery, false)
}

// ReleaseTaskLease requires exact confirmed termination for an associated seed.
// Unseeded intent may be released directly because processes always require the
// committed association first. It does not mark work successful.
func (s *Store) ReleaseTaskLease(ctx context.Context, recovery domain.TaskLeaseRecovery) error {
	return s.changeTaskLease(ctx, recovery, true)
}

func (s *Store) changeTaskLease(ctx context.Context, recovery domain.TaskLeaseRecovery, release bool) error {
	if recovery.Now.IsZero() || strings.TrimSpace(recovery.Reason) == "" || len(recovery.Reason) > 2000 {
		return ports.ErrTaskInvalid
	}
	if !release && (strings.TrimSpace(recovery.NewHolderID) == "" || recovery.NewHolderID == recovery.Token.HolderID || len(recovery.NewHolderID) > 200 || strings.ContainsRune(recovery.NewHolderID, 0) || recovery.TTL < time.Second || recovery.TTL > 5*time.Minute) {
		return ports.ErrTaskInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "reconcile task lease", func(q *gen.Queries) error {
		row, err := q.GetTaskLease(ctx, recovery.Token.AttemptID)
		if err != nil {
			return taskReadError(err)
		}
		if taskLeaseFenced(row, recovery.Token) || recovery.Now.Before(row.HeartbeatAt) {
			return ports.ErrTaskLeaseFenced
		}
		if _, err := q.PendingTaskAttemptExecution(ctx, row.AttemptID); err == nil {
			return ports.ErrTaskLeaseFenced
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		dispatch, err := q.GetTaskWorkerDispatch(ctx, row.AttemptID)
		if err == nil {
			session, err := q.GetSession(ctx, domain.SessionID(dispatch.SessionID))
			if err != nil {
				return err
			}
			rec := rowToRecord(session)
			if recovery.SessionID != rec.ID || recovery.ObservedOwner == nil || *recovery.ObservedOwner != rec.ControllerOwner() || rec.IsTerminated != release {
				return ports.ErrTaskLeaseFenced
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		} else if !release || recovery.ObservedOwner != nil || recovery.SessionID != "" {
			return ports.ErrTaskLeaseFenced
		}
		attempt, err := q.GetTaskAttempt(ctx, row.AttemptID)
		if err != nil {
			return err
		}
		action, actorID := "lease_released", recovery.Token.HolderID
		if release {
			_, err = q.ReleaseTaskLease(ctx, gen.ReleaseTaskLeaseParams{AttemptID: row.AttemptID, Generation: row.Generation, HolderID: row.HolderID, Now: sql.NullTime{Time: recovery.Now, Valid: true}, Reason: recovery.Reason})
		} else {
			action, actorID = "lease_recovered", recovery.NewHolderID
			_, err = q.RecoverTaskLease(ctx, gen.RecoverTaskLeaseParams{AttemptID: row.AttemptID, Generation: row.Generation, HolderID: row.HolderID, NewHolderID: recovery.NewHolderID, Now: recovery.Now, ExpiresAt: recovery.Now.Add(recovery.TTL)})
		}
		if err != nil {
			return err
		}
		return insertTaskAudit(ctx, q, row.TaskID, attempt.TaskRevision, action, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: actorID}, Reason: recovery.Reason}, recovery.Now)
	})
}
