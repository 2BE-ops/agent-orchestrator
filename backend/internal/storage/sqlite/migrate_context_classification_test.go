package sqlite

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestContextClassificationUpgradePreservesLegacyHashesAndCDC(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 172)
	s := sqlitestore.NewStore(db, db)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('classified-project','/repo',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	definition := domain.KnowledgeDefinition{Title: "Retained", Kind: "convention", Content: "Legacy fact", Status: "accepted", Confidence: "high", Sources: []domain.KnowledgeSource{{Kind: "user", Reference: "Migration test"}}}
	mutation := domain.KnowledgeMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Verify migration"}
	if _, err := s.CreateProjectKnowledge(ctx, "legacy", "classified-project", definition, mutation); err != nil {
		t.Fatal(err)
	}
	old, err := s.GetKnowledgeVersion(ctx, "legacy", 1)
	if err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 173)
	downTo(t, db, 172)
	upTo(t, db, 173)
	got, err := s.GetKnowledgeVersion(ctx, "legacy", 1)
	if err != nil || got.ContentHash != old.ContentHash || got.Definition.Classification.Effective() != domain.ContextTechnical {
		t.Fatalf("legacy content changed: %+v %v", got, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before {
		t.Fatalf("migration changed CDC: %d %d %v", before, after, err)
	}
	for _, invalid := range []string{`{"classification":"secret"}`, `{"classification":null}`, `{"classification":1}`, `{"classification":"engagement"}`, `{"classification":"engagement","engagementId":null}`, `{"classification":"mission","engagementId":" bad "}`} {
		if _, err := db.Exec(`INSERT INTO project_knowledge_versions(knowledge_id,number,definition,content_hash,actor,reason,created_at) VALUES('legacy',2,?,printf('%064d',0),'{}','invalid',CURRENT_TIMESTAMP)`, invalid); err == nil {
			t.Fatalf("SQL accepted invalid classification: %s", invalid)
		}
	}
	definition.Classification, definition.EngagementID = domain.ContextEngagement, "client-a"
	mutation.ExpectedVersion = 1
	if _, err := s.ReviseProjectKnowledge(ctx, "legacy", definition, mutation); err != nil {
		t.Fatal(err)
	}
	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	err = goose.DownTo(db, "migrations", 172)
	gooseMu.Unlock()
	if err == nil {
		t.Fatal("downgrade removed classification enforcement")
	}
	got, err = s.GetKnowledgeVersion(ctx, "legacy", 2)
	if err != nil || got.Definition.Classification != domain.ContextEngagement || got.Definition.EngagementID != "client-a" {
		t.Fatalf("classified history lost: %+v %v", got, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign keys: %d %v", count, err)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %s %v", integrity, err)
	}
}
