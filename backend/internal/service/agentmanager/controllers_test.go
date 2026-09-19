package agentmanager

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type failingControllerRuntime struct {
	calls    atomic.Int32
	err      error
	captured ports.SpawnConfig
}

func (n *failingControllerRuntime) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	n.calls.Add(1)
	n.captured = cfg
	return domain.SessionRecord{}, 0, 0, n.err
}

func TestManagerControllerStartRacesAndRetryNeverRepeatNativeEffects(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: t.TempDir(), Kind: domain.ProjectKindScratch, RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	definition := domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeChat, MaxParallelWorkers: 1, MaxContextClass: domain.ContextMission}}
	if _, err := s.CreateRegistryEntry(ctx, "manager-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Manager", Enabled: true}, definition, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: actor.ID}, Reason: "Configure native routing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfigureAgentManager(ctx, "project", domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: "manager-type", AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}, domain.TaskMutation{Actor: actor, Reason: "Enable Manager"}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("native effect outcome unknown")
	native := &failingControllerRuntime{err: failure}
	m := NewWithRuntime(s, native)
	input := ControllerStartInput{ID: "admission", ConfigurationVersion: 1, Reason: "Start routing"}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			receipt, err := m.StartController(ctx, actor, "project", input)
			if err == nil && (receipt.Created || receipt.State.Controller.ID != input.ID || receipt.State.Dispatch != nil) {
				err = errors.New("retry did not inspect unseeded admission")
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	failures := 0
	for err := range errs {
		if errors.Is(err, failure) {
			failures++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if failures != 1 || native.calls.Load() != 1 {
		t.Fatalf("duplicate native permission: errors=%d calls=%d", failures, native.calls.Load())
	}
	cfg := native.captured
	if cfg.Kind != domain.KindAgentManager || cfg.ManagerController == nil || cfg.ManagerController.ID != input.ID || cfg.TaskLease != nil || cfg.ParentSessionID != "" || cfg.WorkerSelection == nil || cfg.WorkerSelection.AgentTypeID != "manager-type" || cfg.WorkerSelection.Version != 1 || cfg.WorkerActor.Origin != domain.RegistryUser || cfg.WorkerActor.ID != actor.ID {
		t.Fatalf("wrong native authority: %+v", cfg)
	}
	state, err := m.CurrentController(ctx, "project")
	if err != nil || state == nil || state.Controller.ID != input.ID || state.Dispatch != nil {
		t.Fatalf("lost unseeded admission: %+v %v", state, err)
	}
	if _, err := m.Controller(ctx, "foreign", input.ID); err == nil {
		t.Fatal("cross-project admission disclosed")
	}
	input.ID = "second"
	if _, err := m.StartController(ctx, actor, "project", input); err == nil || native.calls.Load() != 1 {
		t.Fatal("uncertain controller replaced")
	}
	input.ID = "admission"
	input.Reason = "Changed retry"
	if _, err := m.StartController(ctx, actor, "project", input); err == nil || native.calls.Load() != 1 {
		t.Fatal("changed retry relaunched")
	}
	// A fresh service cannot convert the prior crash window into another launch.
	input.Reason = "Start routing"
	receipt, err := NewWithRuntime(s, native).StartController(ctx, actor, "project", input)
	if err != nil || receipt.Created || native.calls.Load() != 1 {
		t.Fatalf("service replacement relaunched: %+v %v", receipt, err)
	}
}

type controllerBoundaryStore struct {
	ports.AgentManagerStore
	ports.AgentManagerControllerStore
	reads int
	err   error
}

func (s *controllerBoundaryStore) GetAgentManagerConfiguration(context.Context, domain.ProjectID, int64) (domain.AgentManagerConfiguration, error) {
	s.reads++
	return domain.AgentManagerConfiguration{}, s.err
}

func TestManagerControllerStartRejectsAuthorityBoundsAndMissingRuntimeBeforeWrites(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("configuration database unavailable")
	s := &controllerBoundaryStore{err: failure}
	native := &failingControllerRuntime{}
	m := NewWithRuntime(s, native)
	input := ControllerStartInput{ID: "admission", ConfigurationVersion: 1, Reason: "Start routing"}
	for _, actor := range []domain.AdaptiveActor{{Kind: "WORKER", ID: "worker"}, {Kind: "AGENT_MANAGER", ID: "manager"}, {Kind: "ORCHESTRATOR", ID: "planner", SessionID: "planner"}, {Kind: "USER", ID: "human", SessionID: "worker"}} {
		if _, err := m.StartController(ctx, actor, "project", input); err == nil {
			t.Fatalf("invalid authority accepted: %+v", actor)
		}
	}
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	for _, change := range []func(*ControllerStartInput){func(i *ControllerStartInput) { i.ID = "" }, func(i *ControllerStartInput) { i.ID = "bad\nidentity" }, func(i *ControllerStartInput) { i.ConfigurationVersion = 0 }, func(i *ControllerStartInput) { i.ConfigurationVersion = 1001 }, func(i *ControllerStartInput) { i.Reason = "" }} {
		bad := input
		change(&bad)
		if _, err := m.StartController(ctx, actor, "project", bad); err == nil {
			t.Fatal("invalid admission accepted")
		}
	}
	if _, err := New(s).StartController(ctx, actor, "project", input); err == nil {
		t.Fatal("missing native engine accepted")
	}
	if s.reads != 0 || native.calls.Load() != 0 {
		t.Fatal("invalid request reached persistence or runtime")
	}
	if _, err := m.StartController(ctx, actor, "project", input); !errors.Is(err, failure) || s.reads != 1 || native.calls.Load() != 0 {
		t.Fatalf("operational failure hidden: %v", err)
	}
}
