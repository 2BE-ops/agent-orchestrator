package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func managerControllerFixture(t *testing.T, s *sqlite.Store, clearance ...domain.ContextClass) (domain.AgentManagerControllerReservation, domain.SessionRecord, domain.WorkerConfiguration) {
	t.Helper()
	seedProject(t, s, "project")
	rec, snapshot := workerSnapshot(t, s, clearance...)
	rec.ProjectID, rec.Kind = "project", domain.KindAgentManager
	snapshot.Selection.Version = 1
	snapshot.ContentHash = snapshot.Hash()
	definition := domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: snapshot.AgentType.ID, AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}
	_, err := s.ConfigureAgentManager(context.Background(), "project", definition, taskMutation(0))
	mustNoError(t, err)
	request := domain.AgentManagerControllerReservation{AgentManagerControllerToken: domain.AgentManagerControllerToken{ID: "controller", ProjectID: "project", ConfigurationVersion: 1}, Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "daemon"}, Reason: "Start configured Manager", Now: time.Now().UTC()}
	return request, rec, snapshot
}

func managerControllerSeed(t *testing.T, s *sqlite.Store) (domain.AgentManagerController, domain.SessionRecord) {
	t.Helper()
	request, rec, snapshot := managerControllerFixture(t, s)
	controller, _, err := s.ReserveAgentManagerController(context.Background(), request)
	mustNoError(t, err)
	seed, _, err := s.CreateAgentManagerSession(context.Background(), controller.AgentManagerControllerToken, rec, snapshot, request.Now)
	mustNoError(t, err)
	return controller, seed
}

func TestAgentManagerAdmissionRacesAndExactReplay(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	request, rec, snapshot := managerControllerFixture(t, s)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := range 8 {
		wg.Go(func() {
			r := request
			r.ID = fmt.Sprintf("controller-%d", i)
			_, _, err := s.ReserveAgentManagerController(ctx, r)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ports.ErrAgentManagerFenced) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("admitted %d competing controllers", winners)
	}
	active, ok, err := s.ActiveAgentManagerController(ctx, "project")
	if err != nil || !ok {
		t.Fatalf("active: %+v %v %v", active, ok, err)
	}
	request.ID = active.ID
	request.Now = request.Now.Add(24 * time.Hour)
	got, created, err := s.ReserveAgentManagerController(ctx, request)
	if err != nil || created || got.ID != active.ID || !got.CreatedAt.Equal(active.CreatedAt) {
		t.Fatalf("replay admitted again: %+v %v %v", got, created, err)
	}
	bad := request
	bad.Reason = "Changed under the same key"
	if _, _, err := s.ReserveAgentManagerController(ctx, bad); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("changed idempotency: %v", err)
	}
	// An exact seed is also consumed once across competing callers.
	seeds := make(chan domain.SessionID, 8)
	createdSeeds := make(chan bool, 8)
	for range 8 {
		wg.Go(func() {
			seed, made, err := s.CreateAgentManagerSession(ctx, active.AgentManagerControllerToken, rec, snapshot, request.Now)
			if err != nil {
				t.Error(err)
			}
			seeds <- seed.ID
			createdSeeds <- made
		})
	}
	wg.Wait()
	close(seeds)
	close(createdSeeds)
	var id domain.SessionID
	for seed := range seeds {
		if id != "" && seed != id {
			t.Fatalf("duplicate seeds: %s %s", id, seed)
		}
		id = seed
	}
	winners = 0
	for made := range createdSeeds {
		if made {
			winners++
		}
	}
	if winners != 1 || id == "" {
		t.Fatalf("seed winners: %d %s", winners, id)
	}
	if _, err := s.CreateConfiguredSession(ctx, rec, snapshot); err == nil {
		t.Fatal("generic worker admission created Manager")
	}
	if _, ok, err := s.ActiveAgentManagerController(ctx, "other"); err != nil || ok {
		t.Fatalf("cross-project ownership: %v %v", ok, err)
	}
	page, err := s.ListAgentManagerControllers(ctx, "project", "", 1)
	if err != nil || len(page) != 1 || page[0].ID != active.ID {
		t.Fatalf("history: %+v %v", page, err)
	}
}

func TestAgentManagerAdmissionRechecksDesiredPolicyAndExactPins(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	request, rec, snapshot := managerControllerFixture(t, s)
	controller, _, err := s.ReserveAgentManagerController(ctx, request)
	mustNoError(t, err)
	bad := snapshot
	model := "other-model"
	bad.Selection.Overrides.Model = &model
	bad.ContentHash = bad.Hash()
	if _, _, err := s.CreateAgentManagerSession(ctx, controller.AgentManagerControllerToken, rec, bad, request.Now); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("controller override escaped governance: %v", err)
	}
	bad = snapshot
	bad.Selection.Version = 0
	bad.ContentHash = bad.Hash()
	if _, _, err := s.CreateAgentManagerSession(ctx, controller.AgentManagerControllerToken, rec, bad, request.Now); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("floating pin escaped: %v", err)
	}
	current, err := s.GetAgentManager(ctx, "project")
	mustNoError(t, err)
	current.Definition.Enabled = false
	_, err = s.ConfigureAgentManager(ctx, "project", current.Definition, taskMutation(1))
	mustNoError(t, err)
	if _, _, err := s.CreateAgentManagerSession(ctx, controller.AgentManagerControllerToken, rec, snapshot, request.Now); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("disabled after admission: %v", err)
	}
	release := domain.AgentManagerControllerRelease{Token: controller.AgentManagerControllerToken, Reason: "Cancel unseeded intent", Now: time.Now().UTC()}
	mustNoError(t, s.ReleaseAgentManagerController(ctx, release))
	if _, _, err := s.CreateAgentManagerSession(ctx, controller.AgentManagerControllerToken, rec, snapshot, release.Now); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("released intent reused: %v", err)
	}
	request.ID = "new-controller"
	request.ConfigurationVersion = 2
	if _, _, err := s.ReserveAgentManagerController(ctx, request); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("disabled manager admission: %v", err)
	}
	current.Definition.Enabled = true
	_, err = s.ConfigureAgentManager(ctx, "project", current.Definition, taskMutation(2))
	mustNoError(t, err)
	if _, _, err := s.ReserveAgentManagerController(ctx, request); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("stale desired revision: %v", err)
	}
	request.ConfigurationVersion = 3
	request.Now = time.Now().UTC()
	_, created, err := s.ReserveAgentManagerController(ctx, request)
	if err != nil || !created {
		t.Fatalf("new explicit admission: %v %v", created, err)
	}
}

func TestAgentManagerSeedAndReleaseRollbackWithAudit(t *testing.T) {
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	ctx := context.Background()
	request, rec, snapshot := managerControllerFixture(t, s)
	controller, _, err := s.ReserveAgentManagerController(ctx, request)
	mustNoError(t, err)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TRIGGER fail_controller_audit BEFORE INSERT ON adaptive_agent_manager_audit WHEN NEW.action IN ('controller_seeded','controller_released') BEGIN SELECT RAISE(ABORT,'injected audit failure'); END;`)
	mustNoError(t, err)
	events, err := s.EventsAfter(ctx, 0, 100)
	mustNoError(t, err)
	if seed, created, err := s.CreateAgentManagerSession(ctx, controller.AgentManagerControllerToken, rec, snapshot, request.Now); err == nil || created || seed.ID != "" {
		t.Fatalf("partial seed: %+v %v %v", seed, created, err)
	}
	if _, found, err := s.GetAgentManagerDispatch(ctx, controller.ID); err != nil || found {
		t.Fatalf("orphan dispatch: %v %v", found, err)
	}
	if rows, err := s.ListAllSessions(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("orphan session: %+v %v", rows, err)
	}
	if err := s.ReleaseAgentManagerController(ctx, domain.AgentManagerControllerRelease{Token: controller.AgentManagerControllerToken, Reason: "Cancel safely", Now: request.Now}); err == nil {
		t.Fatal("audit failure released ownership")
	}
	if active, ok, err := s.ActiveAgentManagerController(ctx, "project"); err != nil || !ok || active.ID != controller.ID {
		t.Fatalf("rollback lost ownership: %+v %v %v", active, ok, err)
	}
	after, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(after) != len(events) {
		t.Fatalf("rollback emitted CDC: %d %d %v", len(after), len(events), err)
	}
	_, err = db.Exec(`DROP TRIGGER fail_controller_audit`)
	mustNoError(t, err)
	seed, _, err := s.CreateAgentManagerSession(ctx, controller.AgentManagerControllerToken, rec, snapshot, request.Now)
	mustNoError(t, err)
	for _, statement := range []string{`UPDATE adaptive_agent_manager_controllers SET configuration_version=99`, `DELETE FROM adaptive_agent_manager_controllers`, `UPDATE adaptive_agent_manager_dispatches SET configuration_hash=configuration_hash`, `DELETE FROM adaptive_agent_manager_dispatches`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("mutable admission: %s", statement)
		}
	}
	metadata := registryMetadata("Disabled live controller Type")
	metadata.Enabled = false
	_, err = s.UpdateRegistryMetadata(ctx, snapshot.AgentType.ID, metadata, registryMutation(domain.RegistryUser, 1))
	mustNoError(t, err)
	reopened, err := sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	prior, created, err := reopened.CreateAgentManagerSession(ctx, controller.AgentManagerControllerToken, rec, snapshot, request.Now.Add(time.Hour))
	if err != nil || created || prior.ID != seed.ID {
		t.Fatalf("replay consulted live Type: %+v %v %v", prior, created, err)
	}
	pinned, found, err := reopened.GetWorkerConfiguration(ctx, seed.ID)
	if err != nil || !found || pinned.ContentHash != snapshot.ContentHash {
		t.Fatalf("retained pins: %+v %v %v", pinned, found, err)
	}
}
