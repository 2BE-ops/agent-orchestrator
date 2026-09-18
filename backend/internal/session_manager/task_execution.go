package sessionmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type taskExecutionContextKey struct{}

func taskExecutionUnavailable() error {
	return apierr.NotImplemented("TASK_EXECUTION_UNAVAILABLE", "Task execution persistence is unavailable")
}

func taskExecutionFenced(err error) error {
	return apierr.Conflict("TASK_EXECUTION_RECOVERY_REQUIRED", "Task execution requires ownership reconciliation", map[string]any{"reason": err.Error()})
}

// Replayed scheduler requests inspect their existing seed before native
// readiness/configuration changes can replace the already-recorded launch.
func (m *Manager) taskDispatchReplay(ctx context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, bool, error) {
	if cfg.TaskLease == nil {
		return domain.SessionRecord{}, false, nil
	}
	if cfg.Kind != domain.KindWorker || cfg.ProjectID == "" || cfg.WorkerSelection == nil {
		return domain.SessionRecord{}, false, apierr.Invalid("INVALID_TASK_DISPATCH", "Task dispatch requires a project worker and Agent Type selection", nil)
	}
	store, ok := m.store.(ports.TaskLeaseStore)
	if !ok {
		return domain.SessionRecord{}, false, taskExecutionUnavailable()
	}
	if _, ok := m.store.(ports.TaskExecutionStore); !ok {
		return domain.SessionRecord{}, false, taskExecutionUnavailable()
	}
	lease, err := store.GetTaskLease(ctx, cfg.TaskLease.AttemptID)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	if lease.TaskLeaseToken != *cfg.TaskLease || lease.ReleasedAt != nil {
		return domain.SessionRecord{}, false, taskExecutionFenced(ports.ErrTaskLeaseFenced)
	}
	dispatch, found, err := store.GetTaskWorkerDispatch(ctx, cfg.TaskLease.AttemptID)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	if !found {
		if m.taskContexts == nil {
			return domain.SessionRecord{}, false, taskExecutionUnavailable()
		}
		if lease.NeedsReconciliation(m.clock()) {
			return domain.SessionRecord{}, false, taskExecutionFenced(ports.ErrTaskLeaseFenced)
		}
		return domain.SessionRecord{}, false, nil
	}
	rec, err := m.getRecord(ctx, dispatch.SessionID)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	if rec.ProjectID != cfg.ProjectID {
		return domain.SessionRecord{}, false, taskExecutionFenced(ports.ErrTaskLeaseFenced)
	}
	return rec, true, nil
}

// Legacy sessions have no task reservation. Task workers reserve their native
// generation durably before process creation; unknown outcomes keep the guard.
func (m *Manager) beginTaskExecution(ctx context.Context, id domain.SessionID, kind, generation string) (*domain.TaskExecutionOperation, error) {
	if op, ok := ctx.Value(taskExecutionContextKey{}).(*domain.TaskExecutionOperation); ok && op.SessionID == id {
		return op, nil
	}
	leases, ok := m.store.(ports.TaskLeaseStore)
	if !ok {
		return nil, nil
	}
	dispatch, found, err := leases.GetTaskWorkerDispatchBySession(ctx, id)
	if err != nil || !found {
		return nil, err
	}
	store, ok := m.store.(ports.TaskExecutionStore)
	if !ok {
		return nil, taskExecutionUnavailable()
	}
	lease, err := leases.GetTaskLease(ctx, dispatch.AttemptID)
	if err != nil {
		return nil, err
	}
	if lease.ReleasedAt != nil {
		return nil, taskExecutionFenced(ports.ErrTaskLeaseFenced)
	}
	rec, err := m.getRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	if generation == "" {
		generation = m.newLaunchID()
	}
	op := domain.TaskExecutionOperation{ID: generation, SessionID: id, Lease: lease.TaskLeaseToken, SourceOwner: rec.ControllerOwner(), Kind: kind, CreatedAt: m.clock()}
	created, err := store.BeginTaskExecution(ctx, op)
	if err != nil {
		return nil, taskExecutionFenced(err)
	}
	if !created {
		return nil, taskExecutionFenced(ports.ErrTaskLeaseFenced)
	}
	return &op, nil
}

func (m *Manager) finishTaskExecution(ctx context.Context, op *domain.TaskExecutionOperation) error {
	if op == nil {
		return nil
	}
	store, ok := m.store.(ports.TaskExecutionStore)
	if !ok {
		return taskExecutionUnavailable()
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	rec, err := m.getRecord(finishCtx, op.SessionID)
	if err != nil {
		return err
	}
	owner := rec.ControllerOwner()
	generation := owner.RuntimeLaunchID
	if owner.Mode == domain.SessionModeChat {
		generation = owner.ControllerGeneration
	}
	adopted := owner.Mode == domain.SessionModeChat && owner.ControllerGeneration != "" && owner.ControllerGeneration == op.SourceOwner.ControllerGeneration && owner.ProviderConversationID == op.SourceOwner.ProviderConversationID && owner.Harness == op.SourceOwner.Harness
	if rec.IsTerminated || (generation != op.ID && !adopted) {
		return taskExecutionFenced(fmt.Errorf("native target generation was not confirmed"))
	}
	if err := store.ResolveTaskExecution(finishCtx, domain.TaskExecutionResolution{OperationID: op.ID, ObservedOwner: owner, Outcome: "connected", Reason: "Confirmed the reserved native worker controller", CreatedAt: m.clock()}); err != nil {
		return taskExecutionFenced(err)
	}
	return nil
}
