package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestEvolutionMigrationSealsExperimentsAndRecommendations(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 181)
	// The round trip must precede any rows: retained history refuses downgrade.
	var before, after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	downTo(t, db, 180)
	upTo(t, db, 181)
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || before != after {
		t.Fatalf("migration emitted CDC: %d %d %v", before, after, err)
	}
	ctx := context.Background()
	s := sqlitestore.NewStore(db, db)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('project','/repo',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	entry, err := s.CreateRegistryEntry(ctx, "evolution-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Candidate", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, MaxParallelWorkers: 1, Instructions: "Check work"}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Configure"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendRegistryVersion(ctx, entry.ID, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, MaxParallelWorkers: 2, Instructions: "Check work and tests"}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, ExpectedRevision: 1, Reason: "Improve"}); err != nil {
		t.Fatal(err)
	}

	experimentSnapshot := `{"id":"experiment","projectId":"project","kind":"agent_type","entryId":"evolution-type","controlVersion":1,"candidateVersion":2,"hypothesis":"Candidate reduces revisions","minimumSamples":3,"status":"running","createdAt":"2026-09-19T09:00:00Z"}`
	if _, err := db.Exec(`INSERT INTO adaptive_experiments(id,project_id,kind,entry_id,control_version,candidate_version,hypothesis,minimum_samples,status,snapshot,content_hash,created_at)
VALUES('experiment','project','agent_type','evolution-type',1,2,'Candidate reduces revisions',3,'running',?,printf('%064d',1),CURRENT_TIMESTAMP)`, experimentSnapshot); err != nil {
		t.Fatal(err)
	}
	// Wrong-kind and echo-mismatched inserts are refused at the database layer.
	if _, err := db.Exec(`INSERT INTO adaptive_experiments(id,project_id,kind,entry_id,control_version,candidate_version,hypothesis,minimum_samples,status,snapshot,content_hash,created_at)
VALUES('skill-experiment','project','skill','evolution-type',1,2,'Impossible pairing',3,'running',?,printf('%064d',2),CURRENT_TIMESTAMP)`, experimentSnapshot); err == nil {
		t.Fatal("kind/version pairing not enforced")
	}
	tampered := `{"id":"other","projectId":"project","kind":"agent_type","entryId":"evolution-type","controlVersion":1,"candidateVersion":2,"hypothesis":"x","minimumSamples":3,"status":"running","createdAt":"2026-09-19T09:00:00Z"}`
	_, err = db.Exec(`INSERT INTO adaptive_experiments(id,project_id,kind,entry_id,control_version,candidate_version,hypothesis,minimum_samples,status,snapshot,content_hash,created_at)
VALUES('echo-experiment','project','agent_type','evolution-type',1,2,'Echo mismatch',3,'running',?,printf('%064d',3),CURRENT_TIMESTAMP)`, tampered)
	if err == nil {
		t.Fatal("snapshot echo not enforced")
	}

	// The one allowed mutation is the single running→concluded seal.
	conclusion := `{"outcome":"keep_control","promotion":"none","reason":"Evidence reviewed","actor":{"origin":"USER","id":"human"},"decidedAt":"2026-09-19T10:00:00Z","evidence":{"from":"2026-09-18T00:00:00Z","to":"2026-09-19T00:00:00Z","observedAt":"2026-09-19T10:00:00Z","control":{"version":1,"comparableAttempts":3,"metrics":{"attempts":3}},"candidate":{"version":2,"comparableAttempts":3,"metrics":{"attempts":3}}},"verdict":{"eligible":true,"findings":[]}}`
	if _, err := db.Exec(`UPDATE adaptive_experiments SET status='concluded',concluded_at=CURRENT_TIMESTAMP,conclusion=? WHERE id='experiment'`, conclusion); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE adaptive_experiments SET hypothesis='Rewritten after seal' WHERE id='experiment'`); err == nil {
		t.Fatal("sealed experiment rewritten")
	}
	if _, err := db.Exec(`DELETE FROM adaptive_experiments WHERE id='experiment'`); err == nil {
		t.Fatal("experiment history deleted")
	}

	recommendationSnapshot := `{"id":"recommendation","projectId":"project","kind":"agent_type","entryId":"evolution-type","fromVersion":1,"observation":"Three tasks needed revisions","sampleSize":3,"status":"pending","createdAt":"2026-09-19T09:00:00Z","proposedHash":` + fmt.Sprintf("%q", strings.Repeat("0", 63)+"4") + `}`
	if _, err := db.Exec(`INSERT INTO adaptive_recommendations(id,project_id,kind,entry_id,from_version,observation,sample_size,proposed,proposed_hash,status,snapshot,content_hash,created_at)
VALUES('recommendation','project','agent_type','evolution-type',1,'Three tasks needed revisions',3,'{"agentType":{"harness":"codex","instructions":"Stronger checks"}}',printf('%064d',4),'pending',?,printf('%064d',5),CURRENT_TIMESTAMP)`, recommendationSnapshot); err != nil {
		t.Fatal(err)
	}
	decision := `{"disposition":"dismissed","reason":"Keep current version","actor":{"origin":"USER","id":"human"},"decidedAt":"2026-09-19T11:00:00Z"}`
	if _, err := db.Exec(`UPDATE adaptive_recommendations SET status='dismissed',decided_at=CURRENT_TIMESTAMP,decision=? WHERE id='recommendation'`, decision); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE adaptive_recommendations SET observation='Rewritten after decision' WHERE id='recommendation'`); err == nil {
		t.Fatal("decided recommendation rewritten")
	}
	if _, err := db.Exec(`DELETE FROM adaptive_recommendations WHERE id='recommendation'`); err == nil {
		t.Fatal("recommendation history deleted")
	}

	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	err = goose.DownTo(db, "migrations", 180)
	gooseMu.Unlock()
	if err == nil {
		t.Fatal("downgrade erased evolution history")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM adaptive_experiments`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("experiment lost: %d %v", count, err)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %s %v", integrity, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign keys: %d %v", count, err)
	}
}
