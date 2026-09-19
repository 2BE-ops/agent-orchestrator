package sessionmanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	managersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agentmanager"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type nativeManagerFixture struct {
	storeDir   string
	manager    *Manager
	store      *sqlite.Store
	controller domain.AgentManagerController
	cfg        ports.SpawnConfig
	runtime    *fakeRuntime
	launcher   *recordingLauncher
	agent      *recordingAgent
	native     *workerNative
}

func TestManagerServiceStartsBothNativeModesAndOnlyInspectsRetries(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			f := newNativeManagerFixture(t, mode)
			if err := f.store.ReleaseAgentManagerController(ctx, domain.AgentManagerControllerRelease{Token: f.controller.AgentManagerControllerToken, Reason: "Release unseeded fixture admission", Now: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			service := managersvc.NewWithRuntime(f.store, f.manager)
			input := managersvc.ControllerStartInput{ID: "service-admission", ConfigurationVersion: 1, Reason: "Start configured native Manager"}
			actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
			receipt, err := service.StartController(ctx, actor, "manager-project", input)
			if err != nil || !receipt.Created || receipt.State.Dispatch == nil || receipt.State.PendingOperation != nil {
				t.Fatalf("native start: %+v %v", receipt, err)
			}
			rec, found, err := f.store.GetSession(ctx, receipt.State.Dispatch.SessionID)
			if err != nil || !found || rec.Kind != domain.KindAgentManager || rec.Mode != mode {
				t.Fatalf("native mode/role: %+v %v %v", rec, found, err)
			}
			checks, starts, chatStarts := f.native.checks, f.runtime.created, len(f.launcher.started)
			f.native.unavailable = true
			replay, err := service.StartController(ctx, actor, "manager-project", input)
			if err != nil || replay.Created || replay.State.Dispatch == nil || replay.State.Dispatch.SessionID != rec.ID || f.native.checks != checks || f.runtime.created != starts || len(f.launcher.started) != chatStarts {
				t.Fatalf("retry relaunched: %+v %v", replay, err)
			}
			current, err := service.CurrentController(ctx, "manager-project")
			if err != nil || current == nil || current.Controller.ID != input.ID {
				t.Fatalf("current: %+v %v", current, err)
			}
		})
	}
}

func TestManagerServiceRetainsPendingNativeStartAcrossRetry(t *testing.T) {
	ctx := context.Background()
	f := newNativeManagerFixture(t, domain.SessionModeChat)
	if err := f.store.ReleaseAgentManagerController(ctx, domain.AgentManagerControllerRelease{Token: f.controller.AgentManagerControllerToken, Reason: "Release unseeded fixture admission", Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	f.launcher.startErr = errors.New("native connection outcome unknown")
	service := managersvc.NewWithRuntime(f.store, f.manager)
	input := managersvc.ControllerStartInput{ID: "uncertain-admission", ConfigurationVersion: 1, Reason: "Start native Manager"}
	actor := domain.AdaptiveActor{Kind: "SYSTEM", ID: "daemon"}
	if _, err := service.StartController(ctx, actor, "manager-project", input); err == nil {
		t.Fatal("native start failure hidden")
	}
	current, err := service.CurrentController(ctx, "manager-project")
	if err != nil || current == nil || current.Dispatch == nil || current.PendingOperation == nil || current.Controller.ReleasedAt != nil {
		t.Fatalf("lost pending native ownership: %+v %v", current, err)
	}
	starts := len(f.launcher.started)
	f.launcher.startErr = nil
	replay, err := service.StartController(ctx, actor, "manager-project", input)
	if err != nil || replay.Created || replay.State.PendingOperation == nil || len(f.launcher.started) != starts {
		t.Fatalf("retry repeated uncertain launch: %+v %v", replay, err)
	}
}

func newNativeManagerFixture(t *testing.T, mode domain.SessionMode) nativeManagerFixture {
	t.Helper()
	ctx := context.Background()
	storeDir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, storeDir)
	project := domain.ProjectRecord{ID: "manager-project", Path: t.TempDir(), Kind: domain.ProjectKindScratch, RegisteredAt: time.Now().UTC(), Config: domain.ProjectConfig{AgentRules: "Implementation-only project rule", OrchestratorRules: "Decomposition-only project rule"}}
	if err := s.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	native := &workerNative{}
	registry := registrysvc.NewWithNative(s, native)
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
	skill, err := registry.Create(ctx, actor, domain.RegistrySkill, registrysvc.CreateInput{Metadata: domain.RegistryMetadata{Name: "Routing evidence", Enabled: true}, Definition: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Keep routing samples and exclusions"}}, Reason: "Configure routing skill"})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := registry.Create(ctx, actor, domain.RegistryAgentType, registrysvc.CreateInput{Metadata: domain.RegistryMetadata{Name: "Project Manager", Enabled: true}, Definition: domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessClaudeCode, SessionMode: mode, Config: domain.AgentConfig{Model: "provider/reviewer", Permissions: domain.PermissionModeAuto}, Instructions: "Inspect candidates before proposing routing", Skills: []domain.SkillVersionRef{{ID: skill.Entry.ID, Version: 1}}, MaxParallelWorkers: 1}}, Reason: "Configure Manager Type"})
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := s.ConfigureAgentManager(ctx, "manager-project", domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: entry.Entry.ID, AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Set Manager governance"})
	if err != nil {
		t.Fatal(err)
	}
	controller, _, err := s.ReserveAgentManagerController(ctx, domain.AgentManagerControllerReservation{AgentManagerControllerToken: domain.AgentManagerControllerToken{ID: "manager-admission", ProjectID: "manager-project", ConfigurationVersion: configuration.Number}, Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "daemon"}, Reason: "Run configured Manager", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{}
	launcher := &recordingLauncher{}
	agent := &recordingAgent{}
	messenger := &fakeMessenger{}
	manager := New(Deps{Store: s, Runtime: runtime, Agents: singleAgent{agent: agent}, Workspace: &fakeWorkspace{path: t.TempDir()}, Messenger: messenger, Lifecycle: lifecycle.New(s, messenger), Chat: launcher, DataDir: t.TempDir(), WorkerConfigurations: registry, LookPath: func(string) (string, error) { return "test-agent", nil }})
	return nativeManagerFixture{storeDir: storeDir, manager: manager, store: s, controller: controller, cfg: ports.SpawnConfig{ProjectID: "manager-project", Kind: domain.KindAgentManager, WorkerSelection: &domain.WorkerSelection{AgentTypeID: entry.Entry.ID, Version: 1}, WorkerActor: actor, ManagerController: &controller.AgentManagerControllerToken, Prompt: "Wait for durable Manager inbox work"}, runtime: runtime, launcher: launcher, agent: agent, native: native}
}

func TestNativeManagerLaunchReplayAndPinnedRestore(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			f := newNativeManagerFixture(t, mode)
			rec, _, _, err := f.manager.Spawn(ctx, f.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if rec.Kind != domain.KindAgentManager {
				t.Fatalf("Manager hidden as worker: %+v", rec)
			}
			dispatch, found, err := f.store.GetAgentManagerDispatch(ctx, f.controller.ID)
			if err != nil || !found || dispatch.SessionID != rec.ID {
				t.Fatalf("binding: %+v %v %v", dispatch, found, err)
			}
			if _, pending, err := f.store.PendingAgentManagerExecution(ctx, rec.ID); err != nil || pending {
				t.Fatalf("launch not resolved: %v %v", pending, err)
			}
			var prompt string
			if mode == domain.SessionModeChat {
				prompt = f.launcher.started[0].SystemPrompt
				if f.runtime.created != 0 {
					t.Fatal("Chat launched terminal runtime")
				}
			} else {
				prompt = f.agent.lastLaunch.SystemPrompt
				if len(f.launcher.started) != 0 {
					t.Fatal("TUI launched Chat")
				}
			}
			for _, part := range []string{"## AO Agent Manager", "Inspect candidates before proposing routing", "Skill 1: Routing evidence"} {
				if !strings.Contains(prompt, part) {
					t.Fatalf("missing pinned Manager context: %s", part)
				}
			}
			for _, part := range []string{"Implementation-only project rule", "Decomposition-only project rule"} {
				if strings.Contains(prompt, part) {
					t.Fatalf("Manager inherited another role: %s", part)
				}
			}
			checks, runtimeStarts, chatStarts := f.native.checks, f.runtime.created, len(f.launcher.started)
			f.native.unavailable = true
			replay, _, _, err := f.manager.Spawn(ctx, f.cfg)
			if err != nil || replay.ID != rec.ID || f.runtime.created != runtimeStarts || len(f.launcher.started) != chatStarts || f.native.checks != checks {
				t.Fatalf("replayed native launch: %+v %v", replay, err)
			}
			f.native.unavailable = false
			snapshot, found, err := f.store.GetWorkerConfiguration(ctx, rec.ID)
			if err != nil || !found || snapshot.AgentType.Version != 1 || len(snapshot.Skills) != 1 {
				t.Fatalf("pins: %+v %v %v", snapshot, found, err)
			}
			skillPath := filepath.Join(f.manager.dataDir, "worker-configurations", string(rec.ID), "00-"+snapshot.Skills[0].Reference.ContentHash[:16], "SKILL.md")
			if content, err := os.ReadFile(skillPath); err != nil || !strings.Contains(string(content), "Keep routing samples and exclusions") {
				t.Fatalf("pinned native Skill: %s %v", content, err)
			}
			// A reopened database and fresh lifecycle Manager restore exact role and
			// configuration through the established native execution paths.
			rec.IsTerminated = true
			rec.Metadata.AgentSessionID = "native-manager"
			if err := f.store.UpdateSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			if err := f.store.Close(); err != nil {
				t.Fatal(err)
			}
			f.store, err = sqlite.Open(f.storeDir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = f.store.Close() })
			f.manager = New(Deps{Store: f.store, Runtime: f.runtime, Agents: singleAgent{agent: f.agent}, Workspace: f.manager.workspace, Messenger: &fakeMessenger{}, Lifecycle: lifecycle.New(f.store, &fakeMessenger{}), Chat: f.launcher, DataDir: f.manager.dataDir, WorkerConfigurations: registrysvc.NewWithNative(f.store, f.native), LookPath: func(string) (string, error) { return "test-agent", nil }})
			if _, err := f.manager.RestoreWithMode(ctx, rec.ID); err != nil {
				t.Fatal(err)
			}
			if _, pending, err := f.store.PendingAgentManagerExecution(ctx, rec.ID); err != nil || pending {
				t.Fatalf("restore not resolved: %v %v", pending, err)
			}
			var restored string
			if mode == domain.SessionModeChat {
				restored = f.launcher.started[len(f.launcher.started)-1].SystemPrompt
			} else {
				restored = f.agent.lastRestore.SystemPrompt
			}
			if restored != prompt {
				t.Fatal("restored Manager context drifted")
			}
		})
	}
}

func TestNativeManagerRequiresDedicatedAdmissionBeforeEffects(t *testing.T) {
	manager := New(Deps{})
	for _, cfg := range []ports.SpawnConfig{{Kind: domain.KindAgentManager}, {Kind: domain.KindWorker, ManagerController: &domain.AgentManagerControllerToken{ID: "forged"}}, {Kind: domain.KindAgentManager, ProjectID: "project", WorkerSelection: &domain.WorkerSelection{AgentTypeID: "type"}}} {
		if _, _, _, err := manager.Spawn(context.Background(), cfg); err == nil {
			t.Fatal("Manager bypassed admission before dependencies")
		}
	}
	f := newNativeManagerFixture(t, domain.SessionModeTUI)
	bad := f.cfg
	bad.WorkerActor.ID = "different-human"
	if _, _, _, err := f.manager.Spawn(context.Background(), bad); err == nil {
		t.Fatal("borrowed governance actor")
	}
	if rows, err := f.store.ListAllSessions(context.Background()); err != nil || len(rows) != 0 || f.runtime.created != 0 {
		t.Fatalf("invalid admission effects: %+v %v", rows, err)
	}
}

func TestNativeManagerFailureRetainsOwnershipAndPreventsReplay(t *testing.T) {
	ctx := context.Background()
	f := newNativeManagerFixture(t, domain.SessionModeChat)
	f.launcher.startErr = errors.New("uncertain native launch")
	if _, _, _, err := f.manager.Spawn(ctx, f.cfg); err == nil {
		t.Fatal("launch failure hidden")
	}
	dispatch, found, err := f.store.GetAgentManagerDispatch(ctx, f.controller.ID)
	if err != nil || !found {
		t.Fatalf("launch lost binding: %+v %v %v", dispatch, found, err)
	}
	if _, pending, err := f.store.PendingAgentManagerExecution(ctx, dispatch.SessionID); err != nil || !pending {
		t.Fatalf("failure discarded pending native effects: %v %v", pending, err)
	}
	starts := len(f.launcher.started)
	if _, _, _, err := f.manager.Spawn(ctx, f.cfg); err != nil || len(f.launcher.started) != starts {
		t.Fatalf("retry launched again: %v", err)
	}
}
