package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestManagerRegistryMigrationRoundTripAndRetainedProof(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 177)
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
	_, err = db.Exec(`INSERT INTO adaptive_agent_manager_contexts(id,request_id,number,controller_id,session_id,native_generation,source_owner,classification,engagement_id,previous_context_hash,snapshot,content_hash,created_at) VALUES('context','request',1,'controller',?,'native','{}','technical','','','{"classification":"technical"}',printf('%064d',0),CURRENT_TIMESTAMP)`, string(seed.ID))
	if err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 178)
	downTo(t, db, 177)
	upTo(t, db, 178)
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || before != after {
		t.Fatalf("migration emitted CDC: %d %d %v", before, after, err)
	}
	retained, err := s.GetAgentManagerRequest(ctx, "project", "request")
	if err != nil || retained.ContentHash != request.ContentHash {
		t.Fatalf("request rewritten: %+v %v", retained, err)
	}
	entry, err := s.CreateRegistryEntry(ctx, "created-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Authored", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, MaxParallelWorkers: 1}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryManager, ID: "controller"}, Reason: "Reusable specialization"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.GetRegistryVersion(ctx, entry.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	// The migration retains opaque historical snapshots unchanged; actual native
	// attribution, content hashing and atomic writes are covered by store tests.
	for _, class := range []string{"mission", "engagement", "technical"} {
		_, err = db.Exec(`INSERT INTO adaptive_agent_manager_registry_actions(id,project_id,request_id,idempotency_key,action,kind,entry_id,version,source_owner,snapshot,content_hash,created_at)
VALUES('receipt','project','request','key','create','agent_type','created-type',1,'{}',
json_object('id','receipt','projectId','project','requestId','request','requestHash',?,'contextId','context','contextHash',printf('%064d',0),'conversationContextHash',printf('%064d',0),'controllerId','controller','sessionId',?,'classification',?,'action',json_object('action','create','kind','agent_type'),'target',json_object('id','created-type','version',1,'contentHash',?),'contentHash',printf('%064d',0)),printf('%064d',0),CURRENT_TIMESTAMP)`, request.ContentHash, string(seed.ID), class, version.ContentHash)
		if class == "technical" && err != nil {
			t.Fatal(err)
		}
		if class != "technical" && err == nil {
			t.Fatal("migration accepted classified reusable content")
		}
	}
	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	err = goose.DownTo(db, "migrations", 177)
	gooseMu.Unlock()
	if err == nil {
		t.Fatal("downgrade erased Manager registry history")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM adaptive_agent_manager_registry_actions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("receipt lost: %d %v", count, err)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %s %v", integrity, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign keys: %d %v", count, err)
	}
}
