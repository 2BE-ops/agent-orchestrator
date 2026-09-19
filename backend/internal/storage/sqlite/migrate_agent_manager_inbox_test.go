package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestAgentManagerInboxMigrationRetainsTaskPolicyAndHistory(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 170)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('inbox-project','/repo',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	s := sqlitestore.NewStore(db, db)
	ctx := context.Background()
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	_, err := s.CreateRegistryEntry(ctx, "inbox-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Manager", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, Instructions: "Route bounded tasks", MaxParallelWorkers: 1}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Configure routing"})
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := s.ConfigureAgentManager(ctx, "inbox-project", domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: "inbox-type", AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}, domain.TaskMutation{Actor: actor, Reason: "Set governance"})
	if err != nil {
		t.Fatal(err)
	}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "checked", Requirement: "Independent verification", EvidenceKind: "manual"}}}
	_, err = s.CreateAdaptiveTask(ctx, "inbox-task", "inbox-project", domain.TaskDefinition{Title: "Bounded task", Brief: "Keep this original task", MaxAttempts: 1}, &criteria, domain.TaskMutation{Actor: actor, Reason: "Plan exact task"})
	if err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 171)
	downTo(t, db, 170)
	upTo(t, db, 171)
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before {
		t.Fatalf("migration changed CDC: %d %d %v", before, after, err)
	}
	retained, err := s.GetAgentManager(ctx, "inbox-project")
	if err != nil || retained.ContentHash != configuration.ContentHash {
		t.Fatalf("policy changed: %+v %v", retained, err)
	}
	request, _, err := s.EnqueueAgentManagerRequest(ctx, domain.AgentManagerEnqueue{ID: "inbox-work", ProjectID: "inbox-project", TaskID: "inbox-task", TaskRevision: 1, ConfigurationVersion: 1, Actor: actor, Reason: "Route exact task", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	err = goose.DownTo(db, "migrations", 170)
	gooseMu.Unlock()
	if err == nil {
		t.Fatal("downgrade erased routing history")
	}
	got, err := s.GetAgentManagerRequest(ctx, "inbox-project", request.ID)
	if err != nil || got.ContentHash != request.ContentHash {
		t.Fatalf("failed downgrade damaged inbox: %+v %v", got, err)
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
