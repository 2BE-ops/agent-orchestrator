package sqlite

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestProjectKnowledgeUpgradeRetainsCDCAndDowngrades(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 158)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('knowledge-upgrade','/repo',CURRENT_TIMESTAMP); INSERT INTO change_log(project_id,event_type,payload) VALUES('knowledge-upgrade','adaptive_task_changed','{}')`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 160)
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before {
		t.Fatalf("migration changed retained CDC: %d %v", after, err)
	}
	s := sqlitestore.NewStore(db, db)
	d := domain.KnowledgeDefinition{Title: "Migration invariant", Kind: "constraint", Content: "Retain prior events", Status: "accepted", Confidence: "high", Sources: []domain.KnowledgeSource{{Kind: "user", Reference: "Migration test"}}}
	if _, err := s.CreateProjectKnowledge(context.Background(), "knowledge", "knowledge-upgrade", d, domain.KnowledgeMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Migration verification"}); err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %s %v", integrity, err)
	}
	if _, err := db.Exec(`INSERT INTO change_log(event_type,payload) VALUES('unknown_knowledge_event','{}')`); err == nil {
		t.Fatal("CDC constraint was removed")
	}
	downTo(t, db, 158)
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before+1 {
		t.Fatalf("downgrade lost audit events: %d %v", after, err)
	}
	upTo(t, db, 160)
	if _, err := s.CreateProjectKnowledge(context.Background(), "after-reupgrade", "knowledge-upgrade", d, domain.KnowledgeMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Reupgrade verification"}); err != nil {
		t.Fatal(err)
	}
}
