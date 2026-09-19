package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func admitSeedWorker(t *testing.T, s *sqlite.Store, harness domain.AgentHarness) domain.SessionRecord {
	t.Helper()
	rec := sampleRecord("")
	rec.Kind = domain.KindWorker
	rec.Harness = harness
	created, err := s.CreateSession(context.Background(), rec)
	if err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	return created
}

func TestSchedulerAdmissionEnforcesGlobalCapAndFreesOnTermination(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.SetMaxConcurrentWorkers(ctx, 2, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	admitSeedWorker(t, s, domain.HarnessClaudeCode)
	admitSeedWorker(t, s, domain.HarnessCodex)
	if _, err := s.CreateSession(ctx, sampleRecord("")); !errors.Is(err, ports.ErrSchedulerWorkerLimit) {
		t.Fatalf("third worker admitted: %v", err)
	}
	// Non-worker roles never consume worker capacity.
	orchestrator := sampleRecord("")
	orchestrator.Kind = domain.KindOrchestrator
	if _, err := s.CreateSession(ctx, orchestrator); err != nil {
		t.Fatalf("orchestrator refused at worker cap: %v", err)
	}
	// Terminating a worker frees capacity for the next launch.
	first, err := s.ListAllSessions(ctx)
	if err != nil || len(first) != 3 {
		t.Fatalf("unexpected session population: %+v %v", first, err)
	}
	for _, rec := range first {
		if rec.Kind != domain.KindWorker {
			continue
		}
		rec.IsTerminated = true
		if err := s.UpdateSession(ctx, rec); err != nil {
			t.Fatal(err)
		}
		break
	}
	if _, err := s.CreateSession(ctx, sampleRecord("")); err != nil {
		t.Fatalf("terminated worker did not free capacity: %v", err)
	}
	// Raising the cap admits immediately; lowering it never kills a worker.
	if err := s.SetMaxConcurrentWorkers(ctx, 3, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSession(ctx, sampleRecord("")); err != nil {
		t.Fatalf("raised cap did not admit: %v", err)
	}
	if err := s.SetMaxConcurrentWorkers(ctx, 1, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListAllSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, rec := range rows {
		if rec.Kind == domain.KindWorker && !rec.IsTerminated {
			active++
		}
	}
	if active != 3 {
		t.Fatalf("lowering the cap must not kill workers: %d active", active)
	}
}

func TestSchedulerAdmissionRacesConcurrentWorkerSpawns(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.SetMaxConcurrentWorkers(ctx, 4, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	created := make(chan bool, 12)
	refused := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			rec := sampleRecord("")
			rec.Kind = domain.KindWorker
			if _, err := s.CreateSession(ctx, rec); err != nil {
				if !errors.Is(err, ports.ErrSchedulerWorkerLimit) {
					t.Errorf("unexpected refusal: %v", err)
				}
				refused <- err
				return
			}
			created <- true
		})
	}
	wg.Wait()
	close(created)
	close(refused)
	if len(created) != 4 || len(refused) != 8 {
		t.Fatalf("cap oversubscribed under concurrency: %d created, %d refused", len(created), len(refused))
	}
}

func TestSchedulerAdmissionEnforcesPerTypeParallelLimit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	rec, snapshot := workerSnapshot(t, s)
	if _, err := s.CreateConfiguredSession(ctx, rec, snapshot); err != nil {
		t.Fatal(err)
	}
	// The same pinned type cannot run a second worker past its limit of one.
	snapshot.Effective.MaxParallelWorkers = 1
	snapshot.ContentHash = snapshot.Hash()
	if _, err := s.CreateConfiguredSession(ctx, rec, snapshot); !errors.Is(err, ports.ErrSchedulerAgentTypeLimit) {
		t.Fatalf("per-type oversubscription admitted: %v", err)
	}
	// A different type is unaffected by the first type's limit.
	entry, err := s.CreateRegistryEntry(ctx, "parallel-other", domain.RegistryAgentType, registryMetadata("Parallel other"), registryAgentDefinition(), registryMutation(domain.RegistryUser, 0))
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.GetRegistryVersion(ctx, entry.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	otherSnapshot := domain.WorkerConfiguration{SchemaVersion: 1, AgentType: domain.WorkerDefinitionRef{ID: entry.ID, Version: 1, Name: entry.Metadata.Name, ContentHash: version.ContentHash}, Selection: domain.WorkerSelection{AgentTypeID: entry.ID}, Effective: *version.Definition.AgentType, Origin: domain.RegistryUser, ActorID: "human", SystemPrompt: "Fixed project instructions", CreatedAt: time.Now().UTC()}
	otherSnapshot.Effective.SessionMode = domain.SessionModeTUI
	otherSnapshot.Effective.Config.Permissions = domain.PermissionModeAuto
	otherSnapshot.ContentHash = otherSnapshot.Hash()
	otherRec := sampleRecord("")
	otherRec.Harness = otherSnapshot.Effective.Harness
	otherRec.Mode = otherSnapshot.Effective.SessionMode
	otherRec.Metadata = domain.SessionMetadata{Permissions: otherSnapshot.Effective.Config.Permissions}
	if _, err := s.CreateConfiguredSession(ctx, otherRec, otherSnapshot); err != nil {
		t.Fatalf("unrelated type refused: %v", err)
	}
}

func TestSchedulerAdmissionSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	if err := s.SetMaxConcurrentWorkers(ctx, 1, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	admitSeedWorker(t, s, domain.HarnessClaudeCode)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if _, err := reopened.CreateSession(ctx, sampleRecord("")); !errors.Is(err, ports.ErrSchedulerWorkerLimit) {
		t.Fatalf("restart lost or inflated capacity: %v", err)
	}
	rows, err := reopened.ListAllSessions(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("restart changed durable session state: %+v %v", rows, err)
	}
	rows[0].IsTerminated = true
	if err := reopened.UpdateSession(ctx, rows[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.CreateSession(ctx, sampleRecord("")); err != nil {
		t.Fatalf("capacity not freed after restart termination: %v", err)
	}
}

func TestSchedulerAdmissionSettingsBounds(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.SetMaxConcurrentWorkers(ctx, 0, time.Now().UTC()); err == nil {
		t.Fatal("zero cap accepted")
	}
	if err := s.SetMaxConcurrentWorkers(ctx, 1001, time.Now().UTC()); err == nil {
		t.Fatal("oversized cap accepted")
	}
	settings, err := s.GetAppSettings(ctx)
	if err != nil || settings.MaxConcurrentWorkers != 100 {
		t.Fatalf("default cap: %+v %v", settings, err)
	}
}
