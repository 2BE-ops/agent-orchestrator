package chat

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func nativeOptionValues(options []ports.ChatConfigOption) []domain.WorkerNativeOption {
	ordered := slices.Clone(options)
	// Restore a model before the controls whose catalogs depend on that model.
	slices.SortStableFunc(ordered, func(a, b ports.ChatConfigOption) int {
		modelA, modelB := a.ID == "model" || a.Category == "model", b.ID == "model" || b.Category == "model"
		if modelA == modelB {
			return 0
		}
		if modelA {
			return -1
		}
		return 1
	})
	values := make([]domain.WorkerNativeOption, 0, len(ordered))
	for _, option := range ordered {
		value := domain.WorkerNativeOption{ID: option.ID, Select: option.Current.Select}
		if option.Current.Boolean != nil {
			copied := *option.Current.Boolean
			value.Boolean = &copied
		}
		values = append(values, value)
	}
	return values
}

func sameNativeValue(a, b domain.WorkerNativeOption) bool {
	return a.Equal(b)
}

func nativeSettings(harness domain.AgentHarness, previous domain.ConversationSettings, options []ports.ChatConfigOption) domain.ConversationSettings {
	settings, _ := settingsFromConfigOptions(previous, permissionConfigOptions(harness, options))
	if harness == domain.HarnessOpenCode {
		for _, option := range options {
			if option.ID == "mode" {
				settings.OpenCodeMode = option.Current.Select
			}
		}
	}
	return settings
}

func (s *Service) resolveWorkerNativeSettings(ctx context.Context, record domain.SessionRecord, current domain.WorkerConfiguration, settings domain.ConversationSettings, options []ports.ChatConfigOption) (domain.WorkerConfiguration, error) {
	if s.workerConfigurations == nil {
		return current, apierr.NotImplemented("WORKER_CONFIGURATION_UNAVAILABLE", "Worker configuration changes are unavailable")
	}
	changed, err := s.workerConfigurations.ResolveWorkerChange(ctx, current, domain.WorkerOverrides{Model: &settings.Model, Effort: &settings.ReasoningEffort, Permissions: &settings.ApprovalMode}, domain.ProjectRecord{ID: string(record.ProjectID)})
	if err != nil {
		return current, err
	}
	changed.NativeSettings = &settings
	changed.NativeOptions = nativeOptionValues(options)
	changed.ContentHash = changed.Hash()
	return changed, changed.Validate()
}

// setConfiguredNativeOption returns handled=false for legacy sessions only.
func (s *Service) setConfiguredNativeOption(ctx context.Context, record domain.SessionRecord, controller *Controller, configurer ports.ChatConfigOptionController, configID string, value ports.ChatConfigOptionValue) ([]ports.ChatConfigOption, bool, error) {
	executions, ok := s.sessions.(ports.WorkerExecutionStore)
	if !ok {
		return nil, false, nil
	}
	current, sequence, found, err := executions.GetEffectiveWorkerConfiguration(ctx, record.ID)
	if err != nil || !found {
		return nil, found || err != nil, err
	}
	changes, ok := s.store.(ports.WorkerNativeChangeStore)
	writer, writable := s.store.(ports.WorkerConversationSettingsStore)
	if !ok || !writable {
		return nil, true, apierr.NotImplemented("WORKER_CONFIGURATION_UNAVAILABLE", "Native configuration history is unavailable")
	}
	controller.sendMu.Lock()
	defer controller.sendMu.Unlock()
	controller.mu.Lock()
	busy := controller.pendingTurnID != "" || controller.dispatchingTurnID != "" || controller.handoff != controllerHandoffNone
	controller.mu.Unlock()
	if busy {
		return nil, true, apierr.Conflict("WORKER_CONFIGURATION_BUSY", "Wait for the current turn or controller handoff before changing native controls", nil)
	}
	if record.Metadata.ControllerGeneration != controller.Generation() || record.Harness != controller.harness {
		return nil, true, ports.ErrRegistryConflict
	}
	if err := s.restoreWorkerNativeConfiguration(ctx, StartConfig{SessionID: record.ID, Harness: record.Harness}, controller.conv, true); err != nil {
		return nil, true, err
	}
	before, err := configurer.ListConfigOptions(ctx)
	if err != nil {
		return nil, true, err
	}
	proposed := slices.Clone(before)
	optionIndex := slices.IndexFunc(proposed, func(option ports.ChatConfigOption) bool { return option.ID == configID })
	if optionIndex < 0 {
		return nil, true, apierr.Invalid("NATIVE_OPTION_UNAVAILABLE", "This native control is no longer available", nil)
	}
	option := proposed[optionIndex]
	valid := value.Boolean != nil && value.Select == "" && option.Type == ports.ChatConfigOptionBoolean
	if option.Type == ports.ChatConfigOptionSelect {
		valid = value.Boolean == nil && slices.ContainsFunc(option.Choices, func(choice ports.ChatConfigOptionChoice) bool { return choice.Value == value.Select })
	}
	if !valid {
		return nil, true, apierr.Invalid("NATIVE_OPTION_INVALID", "Choose a value advertised by the native provider", nil)
	}
	proposed[optionIndex].Current = value
	settings := nativeSettings(record.Harness, controller.Settings(), proposed)
	if option.ID == "model" || option.Category == "model" {
		settings.ReasoningEffort = ""
	}
	if _, err := s.resolveWorkerNativeSettings(ctx, record, current, settings, proposed); err != nil {
		return nil, true, err
	}
	change := domain.WorkerNativeChange{ID: uuid.NewString(), SessionID: record.ID, ConversationID: controller.conversation.ID, Owner: record.ControllerOwner(), PreviousActivation: sequence, Previous: nativeOptionValues(before), Requested: domain.WorkerNativeOption{ID: configID, Select: value.Select, Boolean: value.Boolean}, CreatedAt: s.now()}
	if err := changes.BeginWorkerNativeChange(ctx, change); err != nil {
		return nil, true, err
	}
	options, applyErr := configurer.SetConfigOption(ctx, configID, value)
	if applyErr == nil {
		confirmed := slices.ContainsFunc(nativeOptionValues(options), func(actual domain.WorkerNativeOption) bool { return sameNativeValue(actual, change.Requested) })
		if !confirmed {
			applyErr = fmt.Errorf("native provider did not confirm the requested configuration")
		}
	}
	if applyErr == nil {
		settings = nativeSettings(record.Harness, controller.Settings(), options)
		var configuration domain.WorkerConfiguration
		configuration, applyErr = s.resolveWorkerNativeSettings(ctx, record, current, settings, options)
		if applyErr == nil {
			execution := domain.WorkerExecution{ID: change.ID, SessionID: record.ID, SourceKind: "conversation_settings", SourceID: change.ID, PreviousActivation: sequence, Configuration: configuration, Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "local-user"}, Reason: "Change native provider control " + configID, CreatedAt: s.now()}
			applyErr = writer.CommitWorkerConversationSettings(ctx, record.ControllerOwner(), controller.conversation.ID, settings, execution)
		}
	}
	if applyErr == nil {
		controller.mu.Lock()
		controller.settings = settings
		controller.mu.Unlock()
		controller.drainLocked(ctx, true)
		return permissionConfigOptions(record.Harness, options), true, nil
	}
	// Cancellation is an unknown native outcome too. Bounded compensation uses a
	// detached context; only a confirmed readback can resolve the durable intent.
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := restoreWorkerNativeOptions(recoveryCtx, configurer, change.Previous); err != nil {
		return nil, true, workerNativeRecoveryError(change.ID)
	}
	if err := changes.RevertWorkerNativeChange(recoveryCtx, record.ControllerOwner(), change.ID, "Native change was rejected or could not be recorded; previous controls restored"); err != nil {
		return nil, true, workerNativeRecoveryError(change.ID)
	}
	controller.drainLocked(recoveryCtx, true)
	return nil, true, applyErr
}

func workerNativeRecoveryError(id string) error {
	return apierr.Conflict("WORKER_NATIVE_RECOVERY_REQUIRED", "Native settings recovery is incomplete. Retry the native control or resume the worker before sending another turn.", map[string]any{"changeId": id})
}

func restoreWorkerNativeOptions(ctx context.Context, configurer ports.ChatConfigOptionController, expected []domain.WorkerNativeOption) error {
	if err := domain.ValidateWorkerNativeOptions(expected); err != nil {
		return err
	}
	for pass := 0; pass < 3; pass++ {
		options, err := configurer.ListConfigOptions(ctx)
		if err != nil {
			return err
		}
		actual := nativeOptionValues(options)
		matched := true
		for _, wanted := range expected {
			index := slices.IndexFunc(actual, func(value domain.WorkerNativeOption) bool { return value.ID == wanted.ID })
			if index >= 0 && sameNativeValue(actual[index], wanted) {
				continue
			}
			matched = false
			if pass == 2 || index < 0 {
				return fmt.Errorf("native configuration control %q cannot be restored", wanted.ID)
			}
			options, err = configurer.SetConfigOption(ctx, wanted.ID, ports.ChatConfigOptionValue{Select: wanted.Select, Boolean: wanted.Boolean})
			if err != nil {
				return err
			}
			actual = nativeOptionValues(options)
		}
		if matched {
			return nil
		}
	}
	return fmt.Errorf("native configuration restoration was not confirmed")
}

func (s *Service) restoreWorkerNativeConfiguration(ctx context.Context, cfg StartConfig, conv ports.ChatConversation, live bool) error {
	changes, ok := s.store.(ports.WorkerNativeChangeStore)
	if !ok {
		return nil
	}
	pending, found, err := changes.PendingWorkerNativeChange(ctx, cfg.SessionID)
	if err != nil {
		return err
	}
	configurer, canConfigure := conv.(ports.ChatConfigOptionController)
	if found {
		if !canConfigure || cfg.Harness != pending.Owner.Harness {
			return workerNativeRecoveryError(pending.ID)
		}
		if err := restoreWorkerNativeOptions(ctx, configurer, pending.Previous); err != nil {
			return workerNativeRecoveryError(pending.ID)
		}
		record, exists, err := s.sessions.GetSession(ctx, cfg.SessionID)
		if err != nil {
			return err
		}
		if !exists {
			return ports.ErrSessionNotFound
		}
		if err := changes.RevertWorkerNativeChange(ctx, record.ControllerOwner(), pending.ID, "Restored previous native controls during controller recovery"); err != nil {
			return err
		}
	}
	if live {
		return nil
	}
	if cfg.WorkerNativeOptions != nil {
		if len(*cfg.WorkerNativeOptions) == 0 {
			return nil
		}
		if !canConfigure {
			return fmt.Errorf("prepared native controls are unavailable")
		}
		return restoreWorkerNativeOptions(ctx, configurer, *cfg.WorkerNativeOptions)
	}
	executions, ok := s.sessions.(ports.WorkerExecutionStore)
	if !ok {
		return nil
	}
	configuration, _, configured, err := executions.GetEffectiveWorkerConfiguration(ctx, cfg.SessionID)
	if err != nil || !configured {
		return err
	}
	// A prepared harness/interface transfer is activated by ControllerReady.
	if configuration.Effective.Harness != cfg.Harness || configuration.Effective.SessionMode != domain.SessionModeChat || len(configuration.NativeOptions) == 0 {
		return nil
	}
	if !canConfigure {
		return fmt.Errorf("retained native controls are unavailable")
	}
	return restoreWorkerNativeOptions(ctx, configurer, configuration.NativeOptions)
}

func (c *Controller) checkWorkerNativeRecovery(ctx context.Context) error {
	changes, ok := c.store.(ports.WorkerNativeChangeStore)
	if !ok {
		return nil
	}
	change, found, err := changes.PendingWorkerNativeChange(ctx, c.sessionID)
	if err != nil {
		return err
	}
	if found {
		return workerNativeRecoveryError(change.ID)
	}
	return nil
}
