package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

// The stage-23 upgrade matrix: an existing installation upgraded in place and
// a clean database must both arrive at the same adaptive platform baseline —
// retained user data, initialized scheduler/dispatch defaults and working
// adaptive surfaces — without manual repair.
func TestUpgradeMatrixExistingDatabaseRetainsDataAndInitializesAdaptiveDefaults(t *testing.T) {
	ctx := context.Background()
	// 140 is the last pre-adaptive schema: every adaptive surface (leases,
	// contexts, manager, orchestrator, controls, notices) arrives afterwards.
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "ao.db")
	if err := os.WriteFile(databasePath, migratedDatabaseSnapshot(t, 140), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+databasePath+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('legacy','/legacy/repo',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}

	// Upgrade the existing database to head.
	upTo(t, db, 1<<30)

	assertAdaptiveBaseline(t, db.QueryRow)

	// Pre-adaptive user data survives the whole adaptive stack.
	var path string
	if err := db.QueryRow(`SELECT path FROM projects WHERE id='legacy'`).Scan(&path); err != nil || path != "/legacy/repo" {
		t.Fatalf("legacy project lost: %q %v", path, err)
	}

	// The upgraded database is fully functional for adaptive work, including
	// this stage's new notice journal, and keeps it across a restart.
	s := sqlitestore.NewStore(db, db)
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdaptiveTask(ctx, "task", "project", domain.TaskDefinition{Title: "Upgraded work", Brief: "Post-upgrade task", MaxAttempts: 2}, nil,
		domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Plan after upgrade"}); err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.BeginOrchestratorNotice(ctx, domain.OrchestratorNotice{ID: "notice", ProjectID: "project", TaskID: "task",
		Fact: "cancelled", Anchor: "cancelled:1", Revision: 1, Detail: "cancelled \"Upgraded work\" revision 1", CreatedAt: time.Now().UTC()}); err != nil || !created {
		t.Fatalf("post-upgrade notice: %v %v", created, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.ListPendingOrchestratorNotices(ctx, 100); err != nil {
		t.Fatalf("reopened notice journal: %v", err)
	}
	if _, err := reopened.GetAdaptiveTask(ctx, "task"); err != nil {
		t.Fatalf("reopened adaptive task: %v", err)
	}
}

func TestUpgradeMatrixCleanDatabaseStartsAtAdaptiveBaseline(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// A clean install reaches the same head version, defaults and empty
	// adaptive surfaces as an upgraded one.
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db")+pragmas)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertAdaptiveBaseline(t, db.QueryRow)
	var notices int
	if err := db.QueryRow(`SELECT count(*) FROM adaptive_orchestrator_notices`).Scan(&notices); err != nil || notices != 0 {
		t.Fatalf("clean notice journal: %d %v", notices, err)
	}
}

func assertAdaptiveBaseline(t *testing.T, queryRow func(string, ...any) *sql.Row) {
	t.Helper()
	var version int64
	if err := queryRow(`SELECT max(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	// 0184 is the renumbered upstream notification_dismissal migration that
	// arrived after the fork had claimed 0148–0183 (see the shippedMigrations
	// ledger); the fork sequence remains unique and contiguous.
	if version != 184 {
		t.Fatalf("database at version %d, want 184", version)
	}
	var workers int
	if err := queryRow(`SELECT max_concurrent_workers FROM app_settings`).Scan(&workers); err != nil || workers != 100 {
		t.Fatalf("scheduler default: %d %v", workers, err)
	}
	for _, table := range []string{
		"adaptive_task_message_dispatch_cursor",
		"adaptive_agent_manager_dispatch_cursor",
		"adaptive_agent_manager_decision_cursor",
		"adaptive_orchestrator_notices",
		"adaptive_project_controls",
	} {
		var exists int
		if err := queryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, table).Scan(&exists); err != nil || exists != 1 {
			t.Fatalf("adaptive surface %s missing: %d %v", table, exists, err)
		}
	}
}
