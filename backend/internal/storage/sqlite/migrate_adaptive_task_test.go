package sqlite

import (
	"context"
	"testing"

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
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before+3 {
		t.Fatalf("retained events: %d %v", after, err)
	}
	upTo(t, db, 155)
	if _, err := s.CreateAdaptiveTask(ctx, "fresh-task", "task-upgrade", definition, nil, domain.TaskMutation{Actor: mutation.Actor, Reason: mutation.Reason}); err != nil {
		t.Fatal(err)
	}
}
