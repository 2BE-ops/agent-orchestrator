package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.TaskExecutionStore = (*Store)(nil)

func taskExecutionFromRow(row gen.AdaptiveTaskExecutionOperation) (domain.TaskExecutionOperation, error) {
	op := domain.TaskExecutionOperation{ID: row.ID, SessionID: domain.SessionID(row.SessionID), Lease: domain.TaskLeaseToken{AttemptID: row.AttemptID, Generation: row.Generation, HolderID: row.HolderID}, Kind: row.Kind, CreatedAt: row.CreatedAt}
	err := json.Unmarshal([]byte(row.SourceOwner), &op.SourceOwner)
	return op, err
}

// BeginTaskExecution reserves native side effects before process I/O. A replay
// returns false and is inspection only, never authorization to launch twice.
func (s *Store) BeginTaskExecution(ctx context.Context, op domain.TaskExecutionOperation) (bool, error) {
	if strings.TrimSpace(op.ID) == "" || len(op.ID) > 200 || op.SessionID == "" || op.CreatedAt.IsZero() || (op.Kind != "dispatch" && op.Kind != "restore") {
		return false, ports.ErrTaskInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return false, err
	}
	defer s.writeMu.Unlock()
	created := false
	err := s.inTx(ctx, "reserve task native execution", func(q *gen.Queries) error {
		row, err := q.GetTaskExecutionOperation(ctx, op.ID)
		if err == nil {
			previous, err := taskExecutionFromRow(row)
			if err != nil {
				return err
			}
			if previous.SessionID != op.SessionID || previous.Lease != op.Lease || previous.SourceOwner != op.SourceOwner || previous.Kind != op.Kind {
				return ports.ErrTaskConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		lease, err := q.GetTaskLease(ctx, op.Lease.AttemptID)
		if err != nil {
			return taskReadError(err)
		}
		if taskLeaseFenced(lease, op.Lease) || op.CreatedAt.Before(lease.HeartbeatAt) || (op.Kind == "dispatch" && !op.CreatedAt.Before(lease.ExpiresAt)) {
			return ports.ErrTaskLeaseFenced
		}
		dispatch, err := q.GetTaskWorkerDispatch(ctx, op.Lease.AttemptID)
		if err != nil {
			return taskReadError(err)
		}
		if dispatch.SessionID != string(op.SessionID) {
			return ports.ErrTaskLeaseFenced
		}
		if _, err := q.PendingTaskAttemptExecution(ctx, op.Lease.AttemptID); err == nil {
			return ports.ErrTaskLeaseFenced
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		session, err := q.GetSession(ctx, op.SessionID)
		if err != nil {
			return err
		}
		rec := rowToRecord(session)
		if rec.ControllerOwner() != op.SourceOwner || (op.Kind == "dispatch" && rec.IsTerminated) {
			return ports.ErrTaskLeaseFenced
		}
		owner, err := json.Marshal(op.SourceOwner)
		if err != nil {
			return err
		}
		if err := q.InsertTaskExecutionOperation(ctx, gen.InsertTaskExecutionOperationParams{ID: op.ID, AttemptID: op.Lease.AttemptID, SessionID: string(op.SessionID), Generation: op.Lease.Generation, HolderID: op.Lease.HolderID, SourceOwner: string(owner), Kind: op.Kind, CreatedAt: op.CreatedAt}); err != nil {
			return err
		}
		attempt, err := q.GetTaskAttempt(ctx, op.Lease.AttemptID)
		if err != nil {
			return err
		}
		if err := insertTaskAudit(ctx, q, attempt.TaskID, attempt.TaskRevision, "execution_reserved", domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: op.Lease.HolderID}, Reason: "Reserved native " + op.Kind + " operation " + op.ID}, op.CreatedAt); err != nil {
			return err
		}
		created = true
		return nil
	})
	return created && err == nil, err
}

// PendingTaskExecution reports unresolved native side effects without inferring
// termination from timeout, missing responses, or an interrupted daemon.
func (s *Store) PendingTaskExecution(ctx context.Context, id domain.SessionID) (domain.TaskExecutionOperation, bool, error) {
	row, err := s.qr.PendingTaskExecution(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskExecutionOperation{}, false, nil
	}
	if err != nil {
		return domain.TaskExecutionOperation{}, false, err
	}
	op, err := taskExecutionFromRow(row)
	return op, err == nil, err
}

// ResolveTaskExecution records a verified lifecycle outcome atomically with
// audit/CDC. Unknown outcomes stay pending and keep the exclusive reservation.
func (s *Store) ResolveTaskExecution(ctx context.Context, resolution domain.TaskExecutionResolution) error {
	if resolution.CreatedAt.IsZero() || strings.TrimSpace(resolution.Reason) == "" || len(resolution.Reason) > 2000 || (resolution.Outcome != "connected" && resolution.Outcome != "terminated") {
		return ports.ErrTaskInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "resolve task native execution", func(q *gen.Queries) error {
		owner, err := json.Marshal(resolution.ObservedOwner)
		if err != nil {
			return err
		}
		previous, err := q.GetTaskExecutionResolution(ctx, resolution.OperationID)
		if err == nil {
			if previous.ObservedOwner != string(owner) || previous.Outcome != resolution.Outcome || previous.Reason != resolution.Reason {
				return ports.ErrTaskConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		row, err := q.GetTaskExecutionOperation(ctx, resolution.OperationID)
		if err != nil {
			return taskReadError(err)
		}
		op, err := taskExecutionFromRow(row)
		if err != nil {
			return err
		}
		if resolution.CreatedAt.Before(op.CreatedAt) {
			return ports.ErrTaskInvalid
		}
		lease, err := q.GetTaskLease(ctx, op.Lease.AttemptID)
		if err != nil {
			return err
		}
		if taskLeaseFenced(lease, op.Lease) {
			return ports.ErrTaskLeaseFenced
		}
		session, err := q.GetSession(ctx, op.SessionID)
		if err != nil {
			return err
		}
		rec := rowToRecord(session)
		if rec.ControllerOwner() != resolution.ObservedOwner || rec.IsTerminated != (resolution.Outcome == "terminated") {
			return ports.ErrTaskLeaseFenced
		}
		if err := q.InsertTaskExecutionResolution(ctx, gen.InsertTaskExecutionResolutionParams{OperationID: op.ID, ObservedOwner: string(owner), Outcome: resolution.Outcome, Reason: resolution.Reason, CreatedAt: resolution.CreatedAt}); err != nil {
			return err
		}
		attempt, err := q.GetTaskAttempt(ctx, op.Lease.AttemptID)
		if err != nil {
			return err
		}
		return insertTaskAudit(ctx, q, attempt.TaskID, attempt.TaskRevision, "execution_"+resolution.Outcome, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: op.Lease.HolderID}, Reason: resolution.Reason}, resolution.CreatedAt)
	})
}
