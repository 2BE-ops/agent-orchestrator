package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestTaskMigrationPreservesExistingCDCAndDowngrades(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 153)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('task-upgrade','/repo',CURRENT_TIMESTAMP);
INSERT INTO sessions(id,project_id,num,activity_last_at,created_at,updated_at) VALUES('task-upgrade-1','task-upgrade',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 155)
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before {
		t.Fatalf("migration changed existing events: %d -> %d %v", before, after, err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	s := sqlitestore.NewStore(db, db)
	definition := domain.TaskDefinition{Title: "Retain task", Brief: "Validate task migrations", MaxAttempts: 2}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "migration", Requirement: "Database remains consistent", EvidenceKind: "test"}}}
	mutation := domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Migration test"}
	ctx := context.Background()
	if _, err := s.CreateAdaptiveTask(ctx, "migration-task", "task-upgrade", definition, &criteria, mutation); err != nil {
		t.Fatal(err)
	}
	mutation.ExpectedRevision = 1
	if _, err := s.ReviseAcceptanceCriteria(ctx, "migration-task", criteria, mutation); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 164)
	if err := s.SetTaskMessageDispatchCursor(ctx, 19); err != nil {
		t.Fatal(err)
	}
	if cursor, err := s.TaskMessageDispatchCursor(ctx); err != nil || cursor != 19 {
		t.Fatalf("dispatch checkpoint: %d %v", cursor, err)
	}
	mutation.ExpectedRevision = 2
	_, lease, err := s.ReserveTask(ctx, domain.TaskReservation{ID: "upgrade-attempt", TaskID: "migration-task", LaunchIntentID: "upgrade-launch", HolderID: "scheduler", Mutation: mutation, Now: time.Now().UTC(), TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adaptive_task_dispatches(attempt_id,session_id,configuration_hash,created_at) VALUES('upgrade-attempt','task-upgrade-1',printf('%064d',0),CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	session, ok, err := s.GetSession(ctx, "task-upgrade-1")
	if err != nil || !ok {
		t.Fatalf("retained session: %v %v", ok, err)
	}
	if _, err := s.BeginTaskExecution(ctx, domain.TaskExecutionOperation{ID: "upgrade-native", SessionID: session.ID, Lease: lease.TaskLeaseToken, SourceOwner: session.ControllerOwner(), Kind: "dispatch", CreatedAt: lease.HeartbeatAt}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeTaskIntent(ctx, "migration-task", domain.TaskIntentChange{Intent: "cancel", Mutation: mutation}); err != nil {
		t.Fatal(err)
	}
	// Exercise populated context foreign keys and downgrade ordering. Exact
	// sealed content validation is covered by the context store tests.
	if _, err := db.Exec(`INSERT INTO adaptive_task_contexts(attempt_id,session_id,execution_operation_id,configuration_hash,snapshot,content_hash,created_at) VALUES('upgrade-attempt','task-upgrade-1','upgrade-native',printf('%064d',0),'{}',printf('%064d',0),CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO adaptive_task_results(id,attempt_id,task_id,number,session_id,native_generation,source_owner,task_revision,criteria_version,configuration_hash,configuration_sequence,context_hash,idempotency_key,definition,content_hash,created_at)
VALUES('result','upgrade-attempt','migration-task',1,'task-upgrade-1','upgrade-native','{}',2,2,printf('%064d',0),0,printf('%064d',0),'migration-result','{}',printf('%064d',0),CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	// Populate message and delivery references before testing downgrade order.
	if _, err := db.Exec(`INSERT INTO adaptive_task_messages(id,project_id,task_id,attempt_id,session_id,native_generation,source_owner,task_revision,criteria_version,configuration_hash,configuration_sequence,context_hash,target_task_id,correlation_id,idempotency_key,definition,content_hash,created_at)
VALUES('message','task-upgrade','migration-task','upgrade-attempt','task-upgrade-1','upgrade-native','{}',2,2,printf('%064d',0),0,printf('%064d',0),'migration-task','thread','migration-message','{}',printf('%064d',0),CURRENT_TIMESTAMP);
INSERT INTO adaptive_task_message_deliveries(id,message_id,number,target_attempt_id,session_id,owner,delivery_key,state,reason,created_at,updated_at)
VALUES('delivery','message',1,'upgrade-attempt','task-upgrade-1','{}','delivery-key','uncertain','Migration fixture',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %q %v", integrity, err)
	}
	var writable int
	if err := db.QueryRow(`PRAGMA writable_schema`).Scan(&writable); err != nil || writable != 0 {
		t.Fatalf("schema editing remains enabled: %d %v", writable, err)
	}
	if _, err := db.Exec(`INSERT INTO change_log(event_type,payload) VALUES('unknown_event','{}')`); err == nil {
		t.Fatal("event constraint removed")
	}
	downTo(t, db, 153)
	// Retained task events remain readable after downgrade; existing CDC works.
	if _, err := db.Exec(`UPDATE sessions SET activity_state='active' WHERE id='task-upgrade-1'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before+6 {
		t.Fatalf("retained events: %d %v", after, err)
	}
	upTo(t, db, 155)
	if _, err := s.CreateAdaptiveTask(ctx, "fresh-task", "task-upgrade", definition, nil, domain.TaskMutation{Actor: mutation.Actor, Reason: mutation.Reason}); err != nil {
		t.Fatal(err)
	}
}
