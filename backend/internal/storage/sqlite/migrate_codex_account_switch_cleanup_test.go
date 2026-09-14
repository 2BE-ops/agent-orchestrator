package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigration0142RemovesRetiredCodexAccountSwitchState(t *testing.T) {
	rows := []struct {
		id, phase, failureCode, wantPhase, wantCode string
		terminal                                    bool
	}{
		{id: "pre-credential", phase: "stopping_sessions", wantPhase: "failed", wantCode: "legacy_session_switch_retired", terminal: true},
		{id: "post-credential", phase: "restarting_sessions", wantPhase: "recovery_required", wantCode: "legacy_switch_recovery"},
		{id: "verifying-target", phase: "verifying_target", wantPhase: "recovery_required", wantCode: "legacy_switch_recovery"},
		{id: "rollback-required", phase: "rollback_required", wantPhase: "recovery_required", wantCode: "legacy_switch_recovery"},
		{id: "stop-unconfirmed", phase: "recovery_required", failureCode: "stop_unconfirmed", wantPhase: "failed", wantCode: "legacy_session_switch_retired", terminal: true},
	}

	// Building a database through the full migration history is deliberately
	// expensive under the race detector. Build that history once, then give each
	// legacy state an isolated copy so the one-active-switch invariant is kept.
	templatePath := filepath.Join(t.TempDir(), "ao.db")
	templateDB, err := sql.Open("sqlite", "file:"+templatePath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open template database: %v", err)
	}
	upTo(t, templateDB, 141)
	if err := templateDB.Close(); err != nil {
		t.Fatalf("close template database: %v", err)
	}
	template, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read template database: %v", err)
	}

	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "ao.db")
			if err := os.WriteFile(databasePath, template, 0o600); err != nil {
				t.Fatalf("copy template database: %v", err)
			}
			db, err := sql.Open("sqlite", "file:"+databasePath+"?_pragma=busy_timeout(5000)")
			if err != nil {
				t.Fatalf("open copied database: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			now := time.Now().UTC().Truncate(time.Second)
			if _, err := db.Exec(`INSERT INTO codex_account_switches (
			id, source_account_id, target_account_id, idempotency_key,
			request_fingerprint, expected_account_revision, phase, failure_code,
			created_at, updated_at, source_kind
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				row.id, "source", "target", row.id+"-request", "v1:"+row.id,
				1, row.phase, row.failureCode, now, now, "managed"); err != nil {
				t.Fatalf("seed %s: %v", row.id, err)
			}

			upTo(t, db, 142)

			for _, name := range []string{"codex_account_switch_sessions", "restart_running_sessions"} {
				var count int
				query := `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`
				if name == "restart_running_sessions" {
					query = `SELECT COUNT(*) FROM pragma_table_info('codex_account_switches') WHERE name = ?`
				}
				if err := db.QueryRow(query, name).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("retired schema %s still exists", name)
				}
			}

			var phase, code string
			var completedAt any
			if err := db.QueryRow(`SELECT phase, failure_code, completed_at FROM codex_account_switches WHERE id = ?`, row.id).
				Scan(&phase, &code, &completedAt); err != nil {
				t.Fatal(err)
			}
			if phase != row.wantPhase || code != row.wantCode || (completedAt != nil) != row.terminal {
				t.Fatalf("switch %s = (%s,%s,%v), want (%s,%s,%v)", row.id, phase, code, completedAt != nil, row.wantPhase, row.wantCode, row.terminal)
			}
		})
	}
}
