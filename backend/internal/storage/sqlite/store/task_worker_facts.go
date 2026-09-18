package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

type taskWorkerFacts struct {
	attempt       gen.AdaptiveTaskAttempt
	task          gen.AdaptiveTask
	context       domain.TaskContextSnapshot
	configuration domain.WorkerConfiguration
	activation    int64
}

// readTaskWorkerFacts is called inside the writing transaction by both result
// and message admission. Lease expiry/cancellation alone does not discard output
// from a still-owned controller. Unresolved controller changes reject authorship.
func readTaskWorkerFacts(ctx context.Context, q *gen.Queries, attemptID string, sessionID domain.SessionID, owner domain.SessionControllerOwner, expectedActivation int64) (taskWorkerFacts, error) {
	var facts taskWorkerFacts
	var err error
	facts.attempt, err = q.GetTaskAttempt(ctx, attemptID)
	if err != nil {
		return facts, taskReadError(err)
	}
	lease, err := q.GetTaskLease(ctx, attemptID)
	if err != nil {
		return facts, taskReadError(err)
	}
	if lease.ReleasedAt.Valid {
		return facts, ports.ErrTaskLeaseFenced
	}
	dispatch, err := q.GetTaskWorkerDispatch(ctx, attemptID)
	if err != nil {
		return facts, taskReadError(err)
	}
	if dispatch.SessionID != string(sessionID) {
		return facts, ports.ErrTaskLeaseFenced
	}
	session, err := q.GetSession(ctx, sessionID)
	if err != nil {
		return facts, taskReadError(err)
	}
	rec := rowToRecord(session)
	if rec.Kind != domain.KindWorker || rec.IsTerminated || rec.ControllerOwner() != owner || resultGeneration(owner) == "" {
		return facts, ports.ErrTaskLeaseFenced
	}
	facts.task, err = q.GetAdaptiveTask(ctx, facts.attempt.TaskID)
	if err != nil {
		return facts, taskReadError(err)
	}
	if string(rec.ProjectID) != facts.task.ProjectID {
		return facts, ports.ErrTaskLeaseFenced
	}
	if _, err := q.GetActiveAgentSwitch(ctx, sessionID); err == nil {
		return facts, ports.ErrTaskLeaseFenced
	} else if !errors.Is(err, sql.ErrNoRows) {
		return facts, err
	}
	transition, err := q.GetLatestSessionInterfaceTransition(ctx, sessionID)
	if err == nil {
		// DAEMON_RESTARTED is the existing reconciler's confirmed closed record.
		if !transition.Phase.Terminal() || (transition.Phase == domain.SessionInterfaceTransitionRecovery && transition.ErrorCode != "DAEMON_RESTARTED") {
			return facts, ports.ErrTaskLeaseFenced
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return facts, err
	}
	if _, err := q.PendingTaskAttemptExecution(ctx, attemptID); err == nil {
		return facts, ports.ErrTaskLeaseFenced
	} else if !errors.Is(err, sql.ErrNoRows) {
		return facts, err
	}
	if _, err := q.PendingWorkerNativeChange(ctx, string(sessionID)); err == nil {
		return facts, ports.ErrTaskLeaseFenced
	} else if !errors.Is(err, sql.ErrNoRows) {
		return facts, err
	}
	contextRow, err := q.GetTaskContext(ctx, attemptID)
	if err != nil {
		return facts, taskReadError(err)
	}
	facts.context, err = taskContextFromRow(contextRow)
	if err != nil {
		return facts, err
	}
	if facts.context.SessionID != sessionID || facts.context.Task.TaskID != facts.attempt.TaskID || facts.context.Task.Revision != facts.attempt.TaskRevision || facts.context.CriteriaVersion != facts.attempt.CriteriaVersion {
		return facts, ports.ErrTaskConflict
	}
	var found bool
	facts.configuration, facts.activation, found, err = effectiveWorkerConfiguration(ctx, q, sessionID)
	if err != nil {
		return facts, err
	}
	if !found || facts.activation != expectedActivation || facts.configuration.Effective.Harness != rec.Harness || facts.configuration.Effective.SessionMode != domain.NormalizeSessionMode(rec.Mode) {
		return facts, ports.ErrTaskConflict
	}
	return facts, nil
}
