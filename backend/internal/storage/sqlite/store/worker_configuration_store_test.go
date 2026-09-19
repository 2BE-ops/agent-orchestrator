package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func workerSnapshot(t *testing.T, s *sqlite.Store, clearance ...domain.ContextClass) (domain.SessionRecord, domain.WorkerConfiguration) {
	t.Helper()
	ctx := context.Background()
	definition := registryAgentDefinition()
	if len(clearance) > 0 {
		definition.AgentType.MaxContextClass = clearance[0]
	}
	definition.AgentType.SessionMode = domain.SessionModeTUI
	definition.AgentType.Config.Permissions = domain.PermissionModeAuto
	entry, err := s.CreateRegistryEntry(ctx, "snapshot-type", domain.RegistryAgentType, registryMetadata("Snapshot reviewer"), definition, registryMutation(domain.RegistryUser, 0))
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.GetRegistryVersion(ctx, entry.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := domain.WorkerConfiguration{SchemaVersion: 1, AgentType: domain.WorkerDefinitionRef{ID: entry.ID, Version: 1, Name: entry.Metadata.Name, ContentHash: version.ContentHash}, Selection: domain.WorkerSelection{AgentTypeID: entry.ID}, Effective: *version.Definition.AgentType, Origin: domain.RegistryUser, ActorID: "human", SystemPrompt: "Fixed project instructions", CreatedAt: time.Now().UTC()}
	snapshot.ContentHash = snapshot.Hash()
	rec := sampleRecord("")
	rec.Harness = snapshot.Effective.Harness
	rec.Mode = snapshot.Effective.SessionMode
	rec.Metadata = domain.SessionMetadata{Permissions: snapshot.Effective.Config.Permissions}
	return rec, snapshot
}

func TestWorkerConfigurationAtomicSeedAndHistory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	rec, snapshot := workerSnapshot(t, s)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_worker BEFORE INSERT ON adaptive_worker_configurations BEGIN SELECT RAISE(ABORT,'injected failure'); END;`); err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := s.EventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateConfiguredSession(ctx, rec, snapshot); err == nil {
		t.Fatal("snapshot insertion failure accepted")
	}
	sessions, err := s.ListAllSessions(ctx)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("orphan session: %v %v", sessions, err)
	}
	eventsAfter, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(eventsBefore) != len(eventsAfter) {
		t.Fatalf("rolled-back seed emitted CDC: %v %v", eventsAfter, err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_worker`); err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateConfiguredSession(ctx, rec, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`UPDATE adaptive_worker_configurations SET content_hash=content_hash`, `DELETE FROM adaptive_worker_configurations`} {
		if _, err := db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("mutable snapshot: %s", statement)
		}
	}
	metadata := registryMetadata("Renamed and disabled")
	metadata.Enabled = false
	if _, err := s.UpdateRegistryMetadata(ctx, snapshot.AgentType.ID, metadata, registryMutation(domain.RegistryUser, 1)); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	got, ok, err := reopened.GetWorkerConfiguration(ctx, created.ID)
	if err != nil || !ok || got.Hash() != snapshot.ContentHash || got.AgentType.Name != "Snapshot reviewer" {
		t.Fatalf("history drift after reopen: %+v %v %v", got, ok, err)
	}
	if _, err := s.CreateConfiguredSession(ctx, rec, snapshot); !errors.Is(err, ports.ErrRegistryInvalid) {
		t.Fatalf("disabled type admitted: %v", err)
	}
	// Existing seed rollback is allowed to remove both rows, without weakening
	// immutability of snapshots for sessions that reached runtime/workspace state.
	deleted, err := s.DeleteSession(ctx, created.ID)
	if err != nil || !deleted {
		t.Fatalf("seed rollback failed: %v %v", deleted, err)
	}
	if _, ok, err := s.GetWorkerConfiguration(ctx, created.ID); err != nil || ok {
		t.Fatalf("orphan snapshot after seed rollback: %v %v", ok, err)
	}
}

func TestWorkerConfigurationRejectsTamperingAndRevokedSelection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	rec, snapshot := workerSnapshot(t, s)
	snapshot.SystemPrompt = "Changed after sealing"
	if _, err := s.CreateConfiguredSession(ctx, rec, snapshot); err == nil {
		t.Fatal("unsealed configuration accepted")
	}
	snapshot.ContentHash = snapshot.Hash()
	snapshot.Origin = domain.RegistryManager
	snapshot.ContentHash = snapshot.Hash()
	metadata := registryMetadata("Human only")
	metadata.Policy.ManagerCanSelect = false
	if _, err := s.UpdateRegistryMetadata(ctx, snapshot.AgentType.ID, metadata, registryMutation(domain.RegistryUser, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateConfiguredSession(ctx, rec, snapshot); !errors.Is(err, ports.ErrRegistryForbidden) {
		t.Fatalf("revoked manager selection accepted: %v", err)
	}
	snapshot.Origin = domain.RegistryUser
	snapshot.ContentHash = snapshot.Hash()
	if _, err := s.CreateConfiguredSession(ctx, rec, snapshot); err != nil {
		t.Fatalf("human selection incorrectly restricted: %v", err)
	}
}
