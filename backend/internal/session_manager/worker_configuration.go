package sessionmanager

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (m *Manager) resolveConfiguredWorker(ctx context.Context, cfg ports.SpawnConfig, project domain.ProjectRecord) (ports.SpawnConfig, *domain.WorkerConfiguration, error) {
	if cfg.Kind != domain.KindWorker {
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
