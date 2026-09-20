package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// GetTaskAttempt reads immutable reservation intent and pinned requirements.
func (s *Store) GetTaskAttempt(ctx context.Context, id string) (domain.TaskAttempt, error) {
	row, err := s.qr.GetTaskAttempt(ctx, id)
	if err != nil {
		return domain.TaskAttempt{}, taskReadError(err)
	}
	return taskAttemptFromRow(row)
}

// ListTaskAttempts pages attempts chronologically, including failed dispatches.
func (s *Store) ListTaskAttempts(ctx context.Context, taskID string, after int64, limit int) ([]domain.TaskAttempt, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListTaskAttempts(ctx, gen.ListTaskAttemptsParams{TaskID: taskID, Number: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.TaskAttempt, 0, len(rows))
	for _, row := range rows {
		attempt, err := taskAttemptFromRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, attempt)
	}
	return result, nil
}

// GetTaskLease returns durable heartbeat and exclusive ownership facts.
func (s *Store) GetTaskLease(ctx context.Context, attemptID string) (domain.TaskLease, error) {
	row, err := s.qr.GetTaskLease(ctx, attemptID)
	if err != nil {
		return domain.TaskLease{}, taskReadError(err)
	}
	return taskLeaseFromRow(row), nil
}

// GetActiveTaskLease includes expired reservations until confirmed release.
func (s *Store) GetActiveTaskLease(ctx context.Context, taskID string) (domain.TaskLease, bool, error) {
	row, err := s.qr.GetActiveTaskLease(ctx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskLease{}, false, nil
	}
	if err != nil {
		return domain.TaskLease{}, false, err
	}
	return taskLeaseFromRow(row), true, nil
}

// ListExpiredTaskLeases is bounded recovery work, not an admission candidate list.
func (s *Store) ListExpiredTaskLeases(ctx context.Context, now time.Time, after string, limit int) ([]domain.TaskLease, error) {
	if now.IsZero() || limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListExpiredTaskLeases(ctx, gen.ListExpiredTaskLeasesParams{ExpiresAt: now, AttemptID: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.TaskLease, 0, len(rows))
	for _, row := range rows {
		result = append(result, taskLeaseFromRow(row))
	}
	return result, nil
}

func taskDispatchFromRow(row gen.AdaptiveTaskDispatch) domain.TaskWorkerDispatch {
	return domain.TaskWorkerDispatch{AttemptID: row.AttemptID, SessionID: domain.SessionID(row.SessionID), ConfigurationHash: row.ConfigurationHash, CreatedAt: row.CreatedAt}
}

// GetTaskWorkerDispatch returns the one immutable worker association, if seeded.
func (s *Store) GetTaskWorkerDispatch(ctx context.Context, attemptID string) (domain.TaskWorkerDispatch, bool, error) {
	row, err := s.qr.GetTaskWorkerDispatch(ctx, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskWorkerDispatch{}, false, nil
	}
	if err != nil {
		return domain.TaskWorkerDispatch{}, false, err
	}
	return taskDispatchFromRow(row), true, nil
}

// GetTaskWorkerDispatchBySession resolves a worker's trusted task ownership.
func (s *Store) GetTaskWorkerDispatchBySession(ctx context.Context, id domain.SessionID) (domain.TaskWorkerDispatch, bool, error) {
	row, err := s.qr.GetTaskWorkerDispatchBySession(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskWorkerDispatch{}, false, nil
	}
	if err != nil {
		return domain.TaskWorkerDispatch{}, false, err
	}
	return taskDispatchFromRow(row), true, nil
}

// CreateTaskWorkerSession commits the native worker snapshot, existing AO session
// seed and dispatch association together. created=false means return/reconcile
// the existing seed; the caller must never perform a second fresh launch.
func (s *Store) CreateTaskWorkerSession(ctx context.Context, token domain.TaskLeaseToken, rec domain.SessionRecord, snapshot domain.WorkerConfiguration, now time.Time) (domain.SessionRecord, bool, error) {
	if now.IsZero() {
		return domain.SessionRecord{}, false, ports.ErrTaskInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.SessionRecord{}, false, err
	}
	defer s.writeMu.Unlock()
	var result domain.SessionRecord
	created := false
	err := s.inTx(ctx, "associate task worker", func(q *gen.Queries) error {
		lease, err := q.GetTaskLease(ctx, token.AttemptID)
		if err != nil {
			return taskReadError(err)
		}
		if taskLeaseFenced(lease, token) {
			return ports.ErrTaskLeaseFenced
		}
		task, err := q.GetAdaptiveTask(ctx, lease.TaskID)
		if err != nil {
			return err
		}
		if string(rec.ProjectID) != task.ProjectID {
			return ports.ErrTaskInvalid
		}
		dispatch, err := q.GetTaskWorkerDispatch(ctx, token.AttemptID)
		if err == nil {
			if dispatch.ConfigurationHash != snapshot.ContentHash {
				return ports.ErrTaskConflict
			}
			row, err := q.GetSession(ctx, domain.SessionID(dispatch.SessionID))
			if err != nil {
				return err
			}
			result = rowToRecord(row)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !now.Before(lease.ExpiresAt) || now.Before(lease.HeartbeatAt) {
			return ports.ErrTaskLeaseFenced
		}
		if err := requireTaskRunIntent(ctx, q, task.ID); err != nil {
			return err
		}
		if err := validateTaskActor(ctx, q, task.ProjectID, domain.AdaptiveActor{Kind: "SYSTEM", ID: token.HolderID}); err != nil {
			return err
		}
		attempt, err := q.GetTaskAttempt(ctx, token.AttemptID)
		if err != nil {
			return err
		}
		revisionRow, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: task.ID, Number: attempt.TaskRevision})
		if err != nil {
			return err
		}
		revision, err := taskRevisionFromRow(revisionRow)
		if err != nil {
			return err
		}
		if requested := revision.Definition.RequestedWorker; requested != nil {
			_, requestedHash, err := domain.TaskContent(requested)
			if err != nil {
				return err
			}
			_, selectedHash, err := domain.TaskContent(snapshot.Selection)
			if err != nil {
				return err
			}
			if requestedHash != selectedHash {
				return ports.ErrTaskConflict
			}
		}
		result, err = createConfiguredSession(ctx, q, rec, snapshot)
		if err != nil {
			return err
		}
		if err := q.InsertTaskWorkerDispatch(ctx, gen.InsertTaskWorkerDispatchParams{AttemptID: token.AttemptID, SessionID: string(result.ID), ConfigurationHash: snapshot.ContentHash, CreatedAt: now}); err != nil {
			return err
		}
		if err := insertTaskAudit(ctx, q, task.ID, attempt.TaskRevision, "worker_seeded", domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: token.HolderID}, Reason: "Associated immutable configuration and session seed"}, now); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	return result, created, nil
}
