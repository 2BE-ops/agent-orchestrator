package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestAgentManagerProposalMigrationPreservesInboxAndRefusesHistoryLoss(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 171)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('proposal-project','/repo',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	s := sqlitestore.NewStore(db, db)
	ctx := context.Background()
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	_, err := s.CreateRegistryEntry(ctx, "proposal-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Manager", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, Instructions: "Route bounded tasks", MaxParallelWorkers: 1}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Configure routing"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ConfigureAgentManager(ctx, "proposal-project", domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: "proposal-type", AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}, domain.TaskMutation{Actor: actor, Reason: "Set governance"})
	if err != nil {
		t.Fatal(err)
	}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "checked", Requirement: "Independent verification", EvidenceKind: "manual"}}}
	_, err = s.CreateAdaptiveTask(ctx, "proposal-task", "proposal-project", domain.TaskDefinition{Title: "Bounded task", Brief: "Preserve exact task", MaxAttempts: 1}, &criteria, domain.TaskMutation{Actor: actor, Reason: "Plan work"})
	if err != nil {
		t.Fatal(err)
	}
	request, _, err := s.EnqueueAgentManagerRequest(ctx, domain.AgentManagerEnqueue{ID: "proposal-request", ProjectID: "proposal-project", TaskID: "proposal-task", TaskRevision: 1, ConfigurationVersion: 1, Actor: actor, Reason: "Route task", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.ReserveAgentManagerController(ctx, domain.AgentManagerControllerReservation{AgentManagerControllerToken: domain.AgentManagerControllerToken{ID: "proposal-controller", ProjectID: "proposal-project", ConfigurationVersion: 1}, Actor: actor, Reason: "Reserve Manager", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	seed, err := s.CreateSession(ctx, domain.SessionRecord{ProjectID: "proposal-project", Kind: domain.KindAgentManager, Harness: domain.HarnessCodex, Mode: domain.SessionModeTUI, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO adaptive_agent_manager_dispatches(controller_id,session_id,configuration_hash,created_at) VALUES('proposal-controller',?,printf('%064d',0),CURRENT_TIMESTAMP)`, string(seed.ID))
	if err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 172)
	downTo(t, db, 171)
	upTo(t, db, 172)
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before {
		t.Fatalf("migration changed CDC: %d %d %v", before, after, err)
	}
	got, err := s.GetAgentManagerRequest(ctx, "proposal-project", request.ID)
	if err != nil || got.ContentHash != request.ContentHash {
		t.Fatalf("inbox lost: %+v %v", got, err)
	}
	// Migration history protection treats snapshots as opaque; semantic parsing
	// is exercised separately through the store's real native-owner tests.
	_, err = db.Exec(`INSERT INTO adaptive_agent_manager_proposals(id,request_id,number,idempotency_key,controller_id,session_id,source_owner,snapshot,content_hash,created_at) VALUES('retained','proposal-request',1,'key','proposal-controller',?,'{}','{}',printf('%064d',0),CURRENT_TIMESTAMP)`, string(seed.ID))
	if err != nil {
		t.Fatal(err)
	}
	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	err = goose.DownTo(db, "migrations", 171)
	gooseMu.Unlock()
	if err == nil {
		t.Fatal("downgrade erased native proposal history")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM adaptive_agent_manager_proposals WHERE id='retained'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retained proposal lost: %d %v", count, err)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %s %v", integrity, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign keys: %d %v", count, err)
	}
}
