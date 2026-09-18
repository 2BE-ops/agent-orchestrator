package sessionmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type workerNative struct {
	unavailable bool
	checks      int
}

func (n *workerNative) Configuration(context.Context, string, domain.SessionMode) (agentsvc.Configuration, error) {
	return agentsvc.Configuration{Fields: []agentsvc.ConfigurationField{{Key: "model"}, {Key: "permissions", Options: []string{"auto"}}}, CapabilityState: "supported", ChatCapabilities: []string{string(ports.ChatCapabilityStreaming), string(ports.ChatCapabilityApprovals), string(ports.ChatCapabilityInterrupt), string(ports.ChatCapabilityResume)}}, nil
}
func (n *workerNative) Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error) {
	return ports.AgentModelCatalog{Models: []ports.AgentModelInfo{{ID: "provider/reviewer", Efforts: []string{"high"}}}, CustomModelEntry: ports.CustomModelEntryDirect}, nil
}
func (n *workerNative) EnsureAgentReadiness(context.Context, string, domain.AgentReadinessPurpose) (domain.AgentReadinessSnapshot, error) {
	n.checks++
	state := domain.AgentReadinessReady
	if n.unavailable {
		state = domain.AgentReadinessUnknown
	}
	return domain.AgentReadinessSnapshot{EffectiveReadiness: state}, nil
}

// A persistent native host is adopted without invoking the fresh-process hook.
// This mirrors the production Chat driver contract at the manager boundary.
type workerAdoptionLauncher struct {
	*recordingLauncher
	adopt bool
}

func (l *workerAdoptionLauncher) StartChat(ctx context.Context, cfg ChatStart) (ChatStarted, error) {
	if l.adopt {
		cfg.PrepareControllerEnv = nil
	}
	l.liveReconnect = l.adopt
	return l.recordingLauncher.StartChat(ctx, cfg)
}

// Production store/lifecycle and a reopened database prove configuration is not
// an in-memory registry cache. The harness/runtime alone are injected.
func TestConfiguredWorkerLaunchAndRestoreFreezeNativeOptionsAndSkills(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			dataDir := t.TempDir()
			store, err := sqlitetest.Open(dataDir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if store != nil {
					_ = store.Close()
				}
			})
			project := domain.ProjectRecord{ID: "review", Path: t.TempDir(), RegisteredAt: time.Now().UTC(), Config: domain.ProjectConfig{AgentRules: "Frozen project rules", Worker: domain.RoleOverride{Harness: domain.HarnessClaudeCode, AgentConfig: domain.AgentConfig{Model: "project-model", Effort: "project-effort"}}}}
			if err := store.UpsertProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			native := &workerNative{}
			registry := registrysvc.NewWithNative(store, native)
			actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
			var pins []domain.SkillVersionRef
			for _, name := range []string{"Review evidence", "Review interfaces"} {
				skill, err := registry.Create(ctx, actor, domain.RegistrySkill, registrysvc.CreateInput{Metadata: domain.RegistryMetadata{Name: name, Enabled: true}, Definition: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Apply " + name, Resources: []domain.SkillResource{{Path: "references/checklist.md", Content: "Retained checklist"}}}}, Reason: "Author skill"})
				if err != nil {
					t.Fatal(err)
				}
				pins = append(pins, domain.SkillVersionRef{ID: skill.Entry.ID, Version: 1})
			}
			entry, err := registry.Create(ctx, actor, domain.RegistryAgentType, registrysvc.CreateInput{Metadata: domain.RegistryMetadata{Name: "Reviewer", Enabled: true}, Definition: domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessClaudeCode, SessionMode: mode, Config: domain.AgentConfig{Model: "provider/reviewer", Effort: "high"}, Instructions: "Frozen reviewer instructions", Skills: pins, MaxParallelWorkers: 2}}, Reason: "Author reviewer"})
			if err != nil {
				t.Fatal(err)
			}
			agent := &recordingAgent{}
			launcher := &workerAdoptionLauncher{recordingLauncher: &recordingLauncher{}}
			runtime := &fakeRuntime{}
			workspace := &fakeWorkspace{path: t.TempDir()}
			newConfiguredManager := func() *Manager {
				registry = registrysvc.NewWithNative(store, native)
				messenger := &fakeMessenger{}
				return New(Deps{Store: store, Runtime: runtime, Agents: singleAgent{agent: agent}, Workspace: workspace, Messenger: messenger, Lifecycle: lifecycle.New(store, messenger), Chat: launcher, DataDir: dataDir, WorkerConfigurations: registry, LookPath: func(string) (string, error) { return "/bin/true", nil }})
			}
			manager := newConfiguredManager()
			rec, _, _, err := manager.Spawn(ctx, ports.SpawnConfig{ProjectID: "review", Kind: domain.KindWorker, Prompt: "Review this change", WorkerSelection: &domain.WorkerSelection{AgentTypeID: entry.Entry.ID}})
			if err != nil {
				t.Fatal(err)
			}
			snapshot, ok, err := store.GetWorkerConfiguration(ctx, rec.ID)
			if err != nil || !ok {
				t.Fatalf("snapshot missing: %v %v", ok, err)
			}
			var initialPrompt string
			if mode == domain.SessionModeChat {
				initialPrompt = launcher.started[0].SystemPrompt
				if runtime.created != 0 {
					t.Fatal("Chat also launched terminal")
				}
			} else {
				initialPrompt = agent.lastLaunch.SystemPrompt
				if len(launcher.started) != 0 {
					t.Fatal("terminal also launched Chat")
				}
			}
			for _, text := range []string{"Frozen project rules", "Frozen reviewer instructions", "Skill 1: Review evidence", "Skill 2: Review interfaces"} {
				if !strings.Contains(initialPrompt, text) {
					t.Fatalf("missing pinned instructions %q", text)
				}
			}
			resource := filepath.Join(dataDir, "worker-configurations", string(rec.ID), "00-"+snapshot.Skills[0].Reference.ContentHash[:16], "references", "checklist.md")
			if content, err := os.ReadFile(resource); err != nil || string(content) != "Retained checklist" {
				t.Fatalf("resource: %s %v", content, err)
			}
			if mode == domain.SessionModeChat {
				current, found, err := store.GetSession(ctx, rec.ID)
				if err != nil || !found {
					t.Fatalf("session missing: %v", err)
				}
				native.unavailable = true
				checks := native.checks
				launcher.adopt = true
				if _, err := manager.resumeChatController(ctx, "adopt", current, project, ports.WorkspaceInfo{Path: workspace.path}, false, "", ""); err != nil {
					t.Fatal(err)
				}
				if native.checks != checks {
					t.Fatal("active host adoption reran launch readiness")
				}
				launcher.adopt = false
				before := len(launcher.started)
				if _, err := manager.resumeChatController(ctx, "fresh", current, project, ports.WorkspaceInfo{Path: workspace.path}, false, "", ""); err == nil {
					t.Fatal("fresh controller ignored unavailable native configuration")
				}
				if native.checks != checks+1 || len(launcher.started) != before {
					t.Fatal("fresh controller started before readiness validation")
				}
				native.unavailable = false
			}
			project.Config.AgentRules = "Changed project rules"
			project.Config.Worker.AgentConfig = domain.AgentConfig{Model: "changed-model", Effort: "changed-effort"}
			if err := store.UpsertProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			metadata := entry.Entry.Metadata
			metadata.Enabled = false
			if _, err := registry.Update(ctx, actor, domain.RegistryAgentType, entry.Entry.ID, registrysvc.MetadataInput{Metadata: metadata, ExpectedRevision: 1, Reason: "Disable future workers"}); err != nil {
				t.Fatal(err)
			}
			rec.IsTerminated = true
			rec.Activity.State = domain.ActivityExited
			rec.Metadata.AgentSessionID = "native-review"
			if err := store.UpdateSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = nil
			store, err = sqlite.Open(dataDir)
			if err != nil {
				t.Fatal(err)
			}
			manager = newConfiguredManager()
			runtime.created = 0
			launcher.started = nil
			*agent = recordingAgent{}
			if _, err := manager.RestoreWithMode(ctx, rec.ID); err != nil {
				t.Fatal(err)
			}
			var restoredPrompt string
			if mode == domain.SessionModeChat {
				got := launcher.started[0]
				restoredPrompt = got.SystemPrompt
				if got.Model != "provider/reviewer" || got.Effort != "high" {
					t.Fatalf("Chat default drift: %+v", got)
				}
			} else {
				restoredPrompt = agent.lastRestore.SystemPrompt
				if agent.lastConfig.Model != "provider/reviewer" || agent.lastConfig.Effort != "high" {
					t.Fatalf("terminal default drift: %+v", agent.lastConfig)
				}
			}
			if initialPrompt != restoredPrompt {
				t.Fatal("standing snapshot prompt changed on restore")
			}
			if err := os.WriteFile(resource, []byte("Tampered checklist"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.workerSnapshotPrompt(ctx, rec.ID, snapshot); err == nil {
				t.Fatal("modified resource silently reused or overwritten")
			}
		})
	}
}

func TestConfiguredWorkerUnavailableBeforeAnySeedOrResource(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store := sqlitetest.MustOpen(t)
	native := &workerNative{unavailable: true}
	registry := registrysvc.NewWithNative(store, native)
	entry, err := registry.Create(ctx, domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, domain.RegistryAgentType, registrysvc.CreateInput{Metadata: domain.RegistryMetadata{Name: "Unavailable reviewer", Enabled: true}, Definition: domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessClaudeCode, SessionMode: domain.SessionModeTUI, MaxParallelWorkers: 1}}, Reason: "Author reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{}
	manager := New(Deps{Store: store, Runtime: runtime, WorkerConfigurations: registry, DataDir: dataDir})
	if _, _, _, err := manager.Spawn(ctx, ports.SpawnConfig{Kind: domain.KindWorker, WorkerSelection: &domain.WorkerSelection{AgentTypeID: entry.Entry.ID}}); err == nil {
		t.Fatal("unavailable configuration launched")
	}
	sessions, err := store.ListSessions(ctx, "")
	if err != nil || len(sessions) != 0 || runtime.created != 0 {
		t.Fatalf("launch left durable/process state: %v %v", sessions, err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "worker-configurations")); !os.IsNotExist(err) {
		t.Fatalf("premature resource materialization: %v", err)
	}
}

func TestWorkerResourceRejectsTraversalAndRetainsExistingContent(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	if err := writeWorkerResource(root, "../outside.md", "invalid"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if err := writeWorkerResource(root, "skill/reference.md", "original"); err != nil {
		t.Fatal(err)
	}
	if err := writeWorkerResource(root, "skill/reference.md", "original"); err != nil {
		t.Fatal(err)
	}
	if err := writeWorkerResource(root, "skill/reference.md", "changed!"); err == nil {
		t.Fatal("changed retained content accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeWorkerResource(root, "file/reference.md", "invalid"); err == nil {
		t.Fatal("non-directory parent accepted")
	}
}
