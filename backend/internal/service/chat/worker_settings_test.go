package chat_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type workerSettingsNative struct{}

func (workerSettingsNative) Configuration(context.Context, string, domain.SessionMode) (agentsvc.Configuration, error) {
	return agentsvc.Configuration{Fields: []agentsvc.ConfigurationField{{Key: "model"}, {Key: "permissions", Options: []string{"default", "auto"}}}, CapabilityState: "supported", ChatCapabilities: []string{"streaming", "tools", "approvals", "interrupt", "resume"}}, nil
}
func (workerSettingsNative) Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error) {
	return ports.AgentModelCatalog{SelectionMode: ports.ModelSelectionCatalog, BinaryVersion: "test-native", Models: []ports.AgentModelInfo{{ID: "first", Efforts: []string{"high"}}, {ID: "second", Efforts: []string{"low", "high"}}}}, nil
}
func (workerSettingsNative) EnsureAgentReadiness(context.Context, string, domain.AgentReadinessPurpose) (domain.AgentReadinessSnapshot, error) {
	return domain.AgentReadinessSnapshot{EffectiveReadiness: domain.AgentReadinessReady}, nil
}

func configuredSettingsWorker(t *testing.T, conv ports.ChatConversation) (*sqlite.Store, *chatsvc.Service, domain.SessionRecord, domain.WorkerConfiguration) {
	t.Helper()
	ctx := context.Background()
	st := openStore(t)
	reg := registry.NewWithNative(st, workerSettingsNative{})
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
	entry, err := reg.Create(ctx, actor, domain.RegistryAgentType, registry.CreateInput{Metadata: domain.RegistryMetadata{Name: "Settings worker", Enabled: true}, Definition: domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeChat, Config: domain.AgentConfig{Model: "first", Effort: "high", Permissions: domain.PermissionModeAuto}, MaxParallelWorkers: 1, Instructions: "Retain these instructions"}}, Reason: "Create settings worker"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reg.ResolveWorker(ctx, domain.WorkerSelection{AgentTypeID: entry.Entry.ID}, domain.ProjectRecord{ID: string(testProject)}, domain.SessionModeChat, actor)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ContentHash = snapshot.Hash()
	rec, err := st.CreateConfiguredSession(ctx, domain.SessionRecord{ProjectID: testProject, Kind: domain.KindWorker, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, Metadata: domain.SessionMetadata{Permissions: domain.PermissionModeAuto}, CreatedAt: time.Now(), UpdatedAt: time.Now()}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	svc := chatsvc.New(chatsvc.Options{Store: st, Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: conv}}, Log: slog.New(slog.DiscardHandler), NewID: uuid.NewString})
	svc.SetWorkerConfigurationResolver(reg)
	t.Cleanup(func() { _ = svc.Stop(ctx, rec.ID) })
	if _, err := svc.Start(ctx, chatsvc.StartConfig{SessionID: rec.ID, ProjectID: rec.ProjectID, Harness: rec.Harness, WorkspacePath: t.TempDir(), Model: "first", Effort: "high", Permissions: domain.PermissionModeAuto}); err != nil {
		t.Fatal(err)
	}
	return st, svc, rec, snapshot
}

func TestWorkerTurnSettingsValidateAndRetainExecutionHistory(t *testing.T) {
	ctx := context.Background()
	st, svc, rec, snapshot := configuredSettingsWorker(t, newFakeConversation())
	controller, err := svc.Controller(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	initial := controller.Settings()
	for _, settings := range []domain.ConversationSettings{
		{Model: "missing", ApprovalMode: domain.PermissionModeAuto},
		{Model: "second", ReasoningEffort: "unknown", ApprovalMode: domain.PermissionModeAuto},
		{Model: "second", ApprovalMode: domain.PermissionModeBypassPermissions},
	} {
		if _, err := svc.SetTurnSettings(ctx, rec.ID, settings); err == nil {
			t.Fatalf("invalid settings accepted: %+v", settings)
		}
		if controller.Settings() != initial {
			t.Fatal("failed validation changed controller")
		}
	}
	settings := domain.ConversationSettings{Model: "second", ReasoningEffort: "low", ApprovalMode: domain.PermissionModeDefault}
	if got, err := svc.SetTurnSettings(ctx, rec.ID, settings); err != nil || got != settings {
		t.Fatalf("settings: %+v %v", got, err)
	}
	current, sequence, found, err := st.GetEffectiveWorkerConfiguration(ctx, rec.ID)
	if err != nil || !found || sequence == 0 || current.NativeSettings == nil || *current.NativeSettings != settings || current.Effective.Instructions != snapshot.Effective.Instructions {
		t.Fatalf("missing execution: %+v %v", current, err)
	}
	if _, err := svc.SetTurnSettings(ctx, rec.ID, settings); err != nil {
		t.Fatal(err)
	}
	history, err := st.ListWorkerExecutions(ctx, rec.ID, 0, 100)
	if err != nil || len(history) != 1 {
		t.Fatalf("identical settings duplicated history: %+v %v", history, err)
	}
	stored, err := st.ConversationForSession(ctx, rec.ID)
	if err != nil || stored.Settings != settings {
		t.Fatalf("settings not durable: %+v %v", stored.Settings, err)
	}
	original, found, err := st.GetWorkerConfiguration(ctx, rec.ID)
	if err != nil || !found || original.ContentHash != snapshot.ContentHash {
		t.Fatalf("original launch changed: %v", err)
	}
	if err := st.ClaimChatControllerGeneration(ctx, rec.ID, "replacement-controller"); err != nil {
		t.Fatal(err)
	}
	settings.Model = "first"
	if _, err := svc.SetTurnSettings(ctx, rec.ID, settings); !errors.Is(err, ports.ErrRegistryConflict) {
		t.Fatalf("stale controller accepted: %v", err)
	}
}
