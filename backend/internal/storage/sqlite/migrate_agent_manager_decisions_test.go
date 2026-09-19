package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestManagerDecisionMigrationRoundTripAndRetainedProof(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 175)
	ctx := context.Background()
	s := sqlitestore.NewStore(db, db)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('project','/repo',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	_, err := s.CreateRegistryEntry(ctx, "type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Manager", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, MaxParallelWorkers: 1}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Configure"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ConfigureAgentManager(ctx, "project", domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: "type", AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}, domain.TaskMutation{Actor: actor, Reason: "Enable"})
	if err != nil {
		t.Fatal(err)
	}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "checked", Requirement: "Verified", EvidenceKind: "manual"}}}
	_, err = s.CreateAdaptiveTask(ctx, "task", "project", domain.TaskDefinition{Title: "Work", Brief: "Pinned task", MaxAttempts: 1}, &criteria, domain.TaskMutation{Actor: actor, Reason: "Plan"})
	if err != nil {
		t.Fatal(err)
	}
	request, _, err := s.EnqueueAgentManagerRequest(ctx, domain.AgentManagerEnqueue{ID: "request", ProjectID: "project", TaskID: "task", TaskRevision: 1, ConfigurationVersion: 1, Actor: actor, Reason: "Route", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.ReserveAgentManagerController(ctx, domain.AgentManagerControllerReservation{AgentManagerControllerToken: domain.AgentManagerControllerToken{ID: "controller", ProjectID: "project", ConfigurationVersion: 1}, Actor: actor, Reason: "Reserve", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	seed, err := s.CreateSession(ctx, domain.SessionRecord{ProjectID: "project", Kind: domain.KindAgentManager, Harness: domain.HarnessCodex, Mode: domain.SessionModeTUI, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO adaptive_agent_manager_dispatches(controller_id,session_id,configuration_hash,created_at) VALUES('controller',?,printf('%064d',0),CURRENT_TIMESTAMP)`, string(seed.ID))
	if err != nil {
		t.Fatal(err)
	}
	// Opaque legacy history exercises migration preservation; full semantic/native
	// attribution and decision transactions are verified in the store suite.
	_, err = db.Exec(`INSERT INTO adaptive_agent_manager_contexts(id,request_id,number,controller_id,session_id,native_generation,source_owner,classification,engagement_id,previous_context_hash,snapshot,content_hash,created_at) VALUES('context','request',1,'controller',?,'native','{}','technical','','','{}',printf('%064d',0),CURRENT_TIMESTAMP)`, string(seed.ID))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO adaptive_agent_manager_deliveries(id,request_id,context_id,controller_id,number,session_id,owner,delivery_key,state,reason,created_at,updated_at) VALUES('delivery','request','context','controller',1,?,'{}','key','handed_off','Accepted input',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, string(seed.ID))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO adaptive_agent_manager_proposals(id,request_id,number,idempotency_key,controller_id,session_id,source_owner,snapshot,content_hash,created_at) VALUES('proposal','request',1,'key','controller',?,'{}',json_object('definition',json_object('action','select_existing'),'contextId','context','contextHash',printf('%064d',0),'nativeGeneration','native'),printf('%064d',0),CURRENT_TIMESTAMP)`, string(seed.ID))
	if err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 176)
	downTo(t, db, 175)
	upTo(t, db, 176)
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || before != after {
		t.Fatalf("migration emitted CDC: %d %d %v", before, after, err)
	}
	retained, err := s.GetAgentManagerRequest(ctx, "project", "request")
	if err != nil || retained.ContentHash != request.ContentHash {
		t.Fatalf("request rewritten: %+v %v", retained, err)
	}
	_, err = db.Exec(`INSERT INTO adaptive_agent_manager_decisions VALUES('proposal','request','accepted',json_object('proposalHash',printf('%064d',0),'requestHash',?,'projectId','project'),printf('%064d',0),CURRENT_TIMESTAMP)`, request.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO adaptive_agent_manager_request_resolutions VALUES('request','selected','{"kind":"SYSTEM","id":"manager-selector"}','Recorded selection',CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatal(err)
	}
	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	err = goose.DownTo(db, "migrations", 175)
	gooseMu.Unlock()
	if err == nil {
		t.Fatal("downgrade erased decision history")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM adaptive_agent_manager_decisions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("decision lost: %d %v", count, err)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %s %v", integrity, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign keys: %d %v", count, err)
	}
}
