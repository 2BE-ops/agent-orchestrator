package sqlite

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestTaskCheckProvenanceUpgradeDoesNotInventObservations(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 165)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('check-project','/repo',CURRENT_TIMESTAMP);
INSERT INTO sessions(id,project_id,num,activity_last_at,created_at,updated_at) VALUES('check-session','check-project',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO pr(url,session_id,number,updated_at) VALUES('https://example.test/pr/1','check-session',1,CURRENT_TIMESTAMP);
INSERT INTO pr_checks(pr_url,name,commit_hash,status,created_at,conclusion) VALUES('https://example.test/pr/1','test','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','passed',CURRENT_TIMESTAMP,'success');`); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 166)
	var observed sql.NullTime
	if err := db.QueryRow(`SELECT observed_at FROM pr_checks WHERE name='test'`).Scan(&observed); err != nil || observed.Valid {
		t.Fatalf("migration invented observation: %+v %v", observed, err)
	}
	now := time.Now().UTC()
	s := sqlitestore.NewStore(db, db)
	pr := domain.PullRequest{URL: "https://example.test/pr/1", SessionID: "check-session", Number: 1, HeadSHA: strings.Repeat("a", 40), UpdatedAt: now, CIObservedAt: now}
	if err := s.WriteSCMObservation(t.Context(), pr, []domain.PullRequestCheck{{Name: "test", CommitHash: pr.HeadSHA, Status: domain.PRCheckPassed, Conclusion: "success", CreatedAt: now}}, nil, nil, nil, ports.ReviewWritePreserve); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT observed_at FROM pr_checks WHERE name='test'`).Scan(&observed); err != nil || !observed.Valid || !observed.Time.Equal(now) {
		t.Fatalf("SCM observation not retained: %+v %v", observed, err)
	}
	downTo(t, db, 165)
	upTo(t, db, 166)
	if err := db.QueryRow(`SELECT observed_at FROM pr_checks WHERE name='test'`).Scan(&observed); err != nil || observed.Valid {
		t.Fatalf("re-upgrade invented provenance: %+v %v", observed, err)
	}
}
