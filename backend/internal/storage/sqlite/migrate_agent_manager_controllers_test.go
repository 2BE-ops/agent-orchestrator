package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestAgentManagerControllerMigrationPreservesGovernanceAndRefusesHistoryLoss(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 169)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('manager-admission','/repo',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	s := sqlitestore.NewStore(db, db)
	ctx := context.Background()
	_, err := s.CreateRegistryEntry(ctx, "native-manager", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Manager", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, Instructions: "Route bounded tasks", MaxParallelWorkers: 1}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Configure controller"})
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := s.ConfigureAgentManager(ctx, "manager-admission", domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: "native-manager", AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Set user governance"})
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 170)
	downTo(t, db, 169)
	upTo(t, db, 170)
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before {
		t.Fatalf("controller migration changed CDC: %d %d %v", before, after, err)
	}
	retained, err := s.GetAgentManager(ctx, "manager-admission")
	if err != nil || retained.ContentHash != configuration.ContentHash {
		t.Fatalf("policy lost: %+v %v", retained, err)
	}
	request := domain.AgentManagerControllerReservation{AgentManagerControllerToken: domain.AgentManagerControllerToken{ID: "controller", ProjectID: "manager-admission", ConfigurationVersion: 1}, Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "daemon"}, Reason: "Reserve configured Manager", Now: time.Now().UTC()}
	_, _, err = s.ReserveAgentManagerController(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	err = goose.DownTo(db, "migrations", 169)
	gooseMu.Unlock()
	if err == nil {
		t.Fatal("downgrade erased retained native ownership")
	}
	active, ok, err := s.ActiveAgentManagerController(ctx, "manager-admission")
	if err != nil || !ok || active.ID != request.ID {
		t.Fatalf("failed downgrade damaged ownership: %+v %v %v", active, ok, err)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %s %v", integrity, err)
	}
	var violations int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign keys: %d %v", violations, err)
	}
}
