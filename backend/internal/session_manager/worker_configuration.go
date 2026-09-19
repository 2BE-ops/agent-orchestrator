package sessionmanager

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (m *Manager) resolveConfiguredWorker(ctx context.Context, cfg ports.SpawnConfig, project domain.ProjectRecord) (ports.SpawnConfig, *domain.WorkerConfiguration, error) {
	if cfg.Kind != domain.KindWorker && (cfg.Kind != domain.KindAgentManager || cfg.ManagerController == nil) {
		return cfg, nil, apierr.Invalid("WORKER_TYPE_REQUIRED", "Agent Types may only launch worker sessions", nil)
	}
	if m.workerConfigurations == nil {
		return cfg, nil, apierr.NotImplemented("WORKER_CONFIGURATION_UNAVAILABLE", "Worker configuration service is unavailable")
	}
	if cfg.Harness != "" || cfg.AgentConfig != (domain.AgentConfig{}) || cfg.EffortOverride || cfg.RequestedMode != "" {
		return cfg, nil, apierr.Invalid("AMBIGUOUS_WORKER_CONFIGURATION", "Use the Agent Type's one-off overrides instead of mixing legacy launch options", nil)
	}
	actor := cfg.WorkerActor
	if actor.ID == "" && actor.Origin == "" {
		actor = domain.RegistryActor{Origin: domain.RegistryUser, ID: "local-user"}
	}
	snapshot, err := m.workerConfigurations.ResolveWorker(ctx, *cfg.WorkerSelection, project, m.resolveSessionMode(ctx, ""), actor)
	if err != nil {
		return cfg, nil, err
	}
	cfg.Harness = snapshot.Effective.Harness
	cfg.AgentConfig = snapshot.Effective.Config
	cfg.AgentConfigResolved = true
	cfg.RequestedMode = snapshot.Effective.SessionMode
	return cfg, &snapshot, nil
}

func (m *Manager) workerSnapshot(ctx context.Context, id domain.SessionID) (*domain.WorkerConfiguration, error) {
	if store, ok := m.store.(ports.WorkerExecutionStore); ok {
		snapshot, _, found, err := store.GetEffectiveWorkerConfiguration(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("read worker execution: %w", err)
		}
		if !found {
			return nil, nil
		}
		return &snapshot, nil
	}
	store, ok := m.store.(ports.WorkerConfigurationStore)
	if !ok {
		return nil, nil
	}
	snapshot, found, err := store.GetWorkerConfiguration(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read worker configuration: %w", err)
	}
	if !found {
		return nil, nil
	}
	return &snapshot, nil
}

func (m *Manager) prepareWorkerInterface(ctx context.Context, rec domain.SessionRecord, transition domain.SessionInterfaceTransition) (*domain.WorkerConfiguration, error) {
	snapshot, err := m.workerSnapshot(ctx, rec.ID)
	if err != nil || snapshot == nil {
		return snapshot, err
	}
	store, ok := m.store.(ports.WorkerExecutionStore)
	if !ok || m.workerConfigurations == nil {
		return nil, apierr.NotImplemented("WORKER_CONFIGURATION_UNAVAILABLE", "Worker execution history is unavailable")
	}
	current, sequence, found, err := store.GetEffectiveWorkerConfiguration(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("worker configuration disappeared")
	}
	if current.Effective.Harness != rec.Harness || current.Effective.SessionMode != domain.NormalizeSessionMode(rec.Mode) {
		return nil, apierr.Conflict("WORKER_EXECUTION_SEGMENT_REQUIRED", "Resolve the current worker execution configuration before changing interface", nil)
	}
	current.Effective.SessionMode = transition.TargetMode
	if transition.TargetMode != domain.SessionModeChat {
		current.NativeOptions = nil
	}
	current.Selection.Overrides.SessionMode = &transition.TargetMode
	current.ContentHash = current.Hash()
	if err := m.workerConfigurations.ValidateWorkerRestore(ctx, current, string(rec.ProjectID)); err != nil {
		return nil, err
	}
	execution, err := store.PrepareWorkerExecution(ctx, rec.ControllerOwner(), domain.WorkerExecution{ID: "interface-" + transition.ID, SessionID: rec.ID, SourceKind: "interface_transition", SourceID: transition.ID, PreviousActivation: sequence, Configuration: current, Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "local-user"}, Reason: "Change worker interface to " + string(transition.TargetMode), CreatedAt: m.clock()})
	if err != nil {
		return nil, err
	}
	return &execution.Configuration, nil
}

func (m *Manager) validateWorkerFreshRestore(ctx context.Context, snapshot *domain.WorkerConfiguration, rec domain.SessionRecord) error {
	if snapshot == nil {
		return nil
	}
	if rec.Harness != snapshot.Effective.Harness || domain.NormalizeSessionMode(rec.Mode) != snapshot.Effective.SessionMode {
		return apierr.Conflict("WORKER_EXECUTION_SEGMENT_REQUIRED", "The worker's current execution configuration must be resolved before restoration", nil)
	}
	if m.workerConfigurations == nil {
		return apierr.NotImplemented("WORKER_CONFIGURATION_UNAVAILABLE", "Worker configuration service is unavailable")
	}
	return m.workerConfigurations.ValidateWorkerRestore(ctx, *snapshot, string(rec.ProjectID))
}

func (m *Manager) prepareWorkerHarness(ctx context.Context, rec domain.SessionRecord, project domain.ProjectRecord, sw domain.AgentSwitch, model string) (*domain.WorkerConfiguration, error) {
	snapshot, err := m.workerSnapshot(ctx, rec.ID)
	if err != nil || snapshot == nil {
		return snapshot, err
	}
	store, ok := m.store.(ports.WorkerExecutionStore)
	if !ok || m.workerConfigurations == nil {
		return nil, apierr.NotImplemented("WORKER_CONFIGURATION_UNAVAILABLE", "Worker execution history is unavailable")
	}
	current, sequence, found, err := store.GetEffectiveWorkerConfiguration(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("worker configuration disappeared")
	}
	if current.Effective.Harness != rec.Harness || current.Effective.SessionMode != domain.NormalizeSessionMode(rec.Mode) {
		return nil, apierr.Conflict("WORKER_EXECUTION_SEGMENT_REQUIRED", "Resolve the current worker configuration before switching harness", nil)
	}
	// An explicit different harness uses that harness's native provider reference.
	// The old binding cannot be transplanted; this clearing is retained in history.
	binding := ""
	override := domain.WorkerOverrides{Harness: &sw.TargetHarness, ProviderBindingID: &binding}
	if model != "" {
		override.Model = &model
	}
	changed, err := m.workerConfigurations.ResolveWorkerChange(ctx, current, override, project)
	if err != nil {
		return nil, err
	}
	execution, err := store.PrepareWorkerExecution(ctx, rec.ControllerOwner(), domain.WorkerExecution{ID: "switch-" + string(sw.ID), SessionID: rec.ID, SourceKind: "agent_switch", SourceID: string(sw.ID), PreviousActivation: sequence, Configuration: changed, Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "local-user"}, Reason: "Switch worker harness to " + string(sw.TargetHarness) + " using its native provider configuration", CreatedAt: m.clock()})
	if err != nil {
		return nil, err
	}
	return &execution.Configuration, nil
}
