package sessionmanager

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type managerExecutionContextKey struct{}

func managerExecutionUnavailable() error {
	return apierr.NotImplemented("AGENT_MANAGER_EXECUTION_UNAVAILABLE", "Manager execution persistence is unavailable")
}

func managerExecutionFenced(err error) error {
	return apierr.Conflict("AGENT_MANAGER_RECOVERY_REQUIRED", "Manager native ownership requires reconciliation", map[string]any{"reason": err.Error()})
}

// A dedicated admission is mandatory even for internal Manager Spawn callers.
// Existing seeds are returned before live registry/readiness checks can drift.
func (m *Manager) managerDispatchReplay(ctx context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, bool, error) {
	if cfg.Kind != domain.KindAgentManager && cfg.ManagerController == nil {
		return domain.SessionRecord{}, false, nil
	}
	if cfg.Kind != domain.KindAgentManager || cfg.ManagerController == nil || cfg.ProjectID == "" || cfg.WorkerSelection == nil || cfg.TaskLease != nil || cfg.ParentSessionID != "" || cfg.IssueID != "" || len(cfg.Attachments) != 0 {
		return domain.SessionRecord{}, false, apierr.Invalid("AGENT_MANAGER_ADMISSION_REQUIRED", "Manager launch requires its dedicated project admission and exact controller Type", nil)
	}
	store, ok := m.store.(ports.AgentManagerControllerStore)
	if !ok {
		return domain.SessionRecord{}, false, managerExecutionUnavailable()
	}
	governance, ok := m.store.(ports.AgentManagerStore)
	if !ok {
		return domain.SessionRecord{}, false, managerExecutionUnavailable()
	}
	controller, err := store.GetAgentManagerController(ctx, cfg.ManagerController.ID)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	if controller.AgentManagerControllerToken != *cfg.ManagerController || controller.ProjectID != cfg.ProjectID || controller.ReleasedAt != nil {
		return domain.SessionRecord{}, false, managerExecutionFenced(ports.ErrAgentManagerFenced)
	}
	configuration, err := governance.GetAgentManagerConfiguration(ctx, cfg.ProjectID, controller.ConfigurationVersion)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	if cfg.WorkerSelection.AgentTypeID != configuration.ControllerType.ID || cfg.WorkerSelection.Version != configuration.ControllerType.Version || cfg.WorkerSelection.Overrides != (domain.WorkerOverrides{}) || cfg.WorkerActor.Origin != domain.RegistryUser || cfg.WorkerActor.ID != configuration.Actor.ID {
		return domain.SessionRecord{}, false, managerExecutionFenced(ports.ErrAgentManagerConflict)
	}
	dispatch, found, err := store.GetAgentManagerDispatch(ctx, controller.ID)
	if err != nil || !found {
		return domain.SessionRecord{}, false, err
	}
	rec, err := m.getRecord(ctx, dispatch.SessionID)
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	if rec.Kind != domain.KindAgentManager || rec.ProjectID != cfg.ProjectID {
		return domain.SessionRecord{}, false, managerExecutionFenced(ports.ErrAgentManagerFenced)
	}
	return rec, true, nil
}

// Manager lifecycle uses the existing native engine, with its own durable owner.
func (m *Manager) beginManagerExecution(ctx context.Context, rec domain.SessionRecord, kind, generation string) (*domain.AgentManagerExecutionOperation, error) {
	if rec.Kind != domain.KindAgentManager {
		return nil, nil
	}
	if op, ok := ctx.Value(managerExecutionContextKey{}).(*domain.AgentManagerExecutionOperation); ok && op.SessionID == rec.ID {
		return op, nil
	}
	store, ok := m.store.(ports.AgentManagerControllerStore)
	if !ok {
		return nil, managerExecutionUnavailable()
	}
	dispatch, found, err := store.GetAgentManagerDispatchBySession(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, managerExecutionFenced(ports.ErrAgentManagerFenced)
	}
	if generation == "" {
		generation = m.newLaunchID()
	}
	op := domain.AgentManagerExecutionOperation{ID: generation, ControllerID: dispatch.ControllerID, SessionID: rec.ID, SourceOwner: rec.ControllerOwner(), Kind: kind, CreatedAt: m.clock()}
	created, err := store.BeginAgentManagerExecution(ctx, op)
	if err != nil {
		return nil, managerExecutionFenced(err)
	}
	if !created {
		return nil, managerExecutionFenced(ports.ErrAgentManagerFenced)
	}
	return &op, nil
}

func (m *Manager) finishManagerExecution(ctx context.Context, op *domain.AgentManagerExecutionOperation) error {
	if op == nil {
		return nil
	}
	store, ok := m.store.(ports.AgentManagerControllerStore)
	if !ok {
		return managerExecutionUnavailable()
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	rec, err := m.getRecord(finishCtx, op.SessionID)
	if err != nil {
		return err
	}
	if err := store.ResolveAgentManagerExecution(finishCtx, domain.AgentManagerExecutionResolution{OperationID: op.ID, ObservedOwner: rec.ControllerOwner(), Outcome: "connected", Reason: "Confirmed reserved native Manager controller", CreatedAt: m.clock()}); err != nil {
		return managerExecutionFenced(err)
	}
	return nil
}
