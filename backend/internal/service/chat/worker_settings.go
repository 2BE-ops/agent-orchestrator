package chat

import (
	"context"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// SetWorkerConfigurationResolver wires the shared native configuration authority
// before the daemon accepts commands or reconciles live controllers.
func (s *Service) SetWorkerConfigurationResolver(resolver ports.WorkerConfigurationResolver) {
	s.workerConfigurations = resolver
}

func (s *Service) setWorkerTurnSettings(ctx context.Context, record domain.SessionRecord, controller *Controller, settings domain.ConversationSettings) error {
	store, ok := s.sessions.(ports.WorkerExecutionStore)
	if !ok {
		return controller.SetSettings(ctx, settings)
	}
	current, sequence, found, err := store.GetEffectiveWorkerConfiguration(ctx, record.ID)
	if err != nil {
		return err
	}
	if !found {
		return controller.SetSettings(ctx, settings)
	}
	if record.Metadata.ControllerGeneration != controller.Generation() || record.Harness != controller.harness {
		return ports.ErrRegistryConflict
	}
	if len(current.NativeOptions) > 0 {
		return apierr.Invalid("WORKER_NATIVE_CONTROLS_REQUIRED", "Use this worker's native controls to change its provider settings", nil)
	}
	writer, ok := s.store.(ports.WorkerConversationSettingsStore)
	if !ok || s.workerConfigurations == nil {
		return apierr.NotImplemented("WORKER_CONFIGURATION_UNAVAILABLE", "Worker configuration changes are unavailable")
	}
	changed, err := s.workerConfigurations.ResolveWorkerChange(ctx, current, domain.WorkerOverrides{Model: &settings.Model, Effort: &settings.ReasoningEffort, Permissions: &settings.ApprovalMode}, domain.ProjectRecord{ID: string(record.ProjectID)})
	if err != nil {
		return err
	}
	changed.NativeSettings = &settings
	changed.ContentHash = changed.Hash()
	if current.ContentHash == changed.ContentHash {
		controller.mu.Lock()
		controller.settings = settings
		controller.mu.Unlock()
		return nil
	}
	operationID := uuid.NewString()
	execution := domain.WorkerExecution{ID: operationID, SessionID: record.ID, SourceKind: "conversation_settings", SourceID: operationID, PreviousActivation: sequence, Configuration: changed, Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "local-user"}, Reason: "Change native conversation settings", CreatedAt: s.now()}
	if err := writer.CommitWorkerConversationSettings(ctx, record.ControllerOwner(), controller.conversation.ID, settings, execution); err != nil {
		return err
	}
	controller.mu.Lock()
	controller.settings = settings
	controller.mu.Unlock()
	return nil
}
