package sqlite

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestAgentManagerGovernanceMigrationRetainsCDCAndProtectsHistory(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 168)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('governance-upgrade','/repo',CURRENT_TIMESTAMP);
INSERT INTO change_log(project_id,event_type,payload) VALUES('governance-upgrade','project_knowledge_changed','{}')`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 169)
	downTo(t, db, 168)
	upTo(t, db, 169)
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before {
		t.Fatalf("schema upgrade changed CDC: %d -> %d %v", before, after, err)
	}
	s := sqlitestore.NewStore(db, db)
	ctx := context.Background()
	_, err := s.CreateRegistryEntry(ctx, "governance-controller", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Manager", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, Instructions: "Route bounded tasks", MaxParallelWorkers: 1}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Configure native controller"})
	if err != nil {
		t.Fatal(err)
	}
	definition := domain.AgentManagerDefinition{SchemaVersion: 1, AgentTypeID: "governance-controller", AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}
	configuration, err := s.ConfigureAgentManager(ctx, "governance-upgrade", definition, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Set manager governance"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO change_log(event_type,payload) VALUES('unknown_manager_event','{}')`); err == nil {
		t.Fatal("CDC CHECK was removed")
	}
	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	err = goose.DownTo(db, "migrations", 168)
	gooseMu.Unlock()
	if err == nil {
		t.Fatal("downgrade deleted governing policy history")
	}
	retained, err := s.GetAgentManager(ctx, "governance-upgrade")
	if err != nil || retained.ContentHash != configuration.ContentHash {
		t.Fatalf("failed downgrade damaged governance: %+v %v", retained, err)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("schema integrity: %s %v", integrity, err)
	}
	var violations int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign keys: %d %v", violations, err)
	}
}
