package sqlite

import (
	"testing"

	"github.com/pressly/goose/v3"
)

func TestAgentManagerKindMigrationRetainsLegacyFactsAndDowngradesWithoutHistory(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 167)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('manager-upgrade','/repo',CURRENT_TIMESTAMP);
INSERT INTO sessions(id,project_id,num,kind,activity_last_at,created_at,updated_at)
VALUES('legacy-worker','manager-upgrade',1,'worker',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
('legacy-orchestrator','manager-upgrade',2,'orchestrator',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 168)
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after != before {
		t.Fatalf("migration changed legacy CDC: %d -> %d %v", before, after, err)
	}
	for id, want := range map[string]string{"legacy-worker": "worker", "legacy-orchestrator": "orchestrator"} {
		var kind string
		if err := db.QueryRow(`SELECT kind FROM sessions WHERE id=?`, id).Scan(&kind); err != nil || kind != want {
			t.Fatalf("legacy kind: %s %v", kind, err)
		}
	}
	downTo(t, db, 167)
	statement := `INSERT INTO sessions(id,project_id,num,kind,activity_last_at,created_at,updated_at) VALUES('manager','manager-upgrade',3,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`
	if _, err := db.Exec(statement, "agent_manager"); err == nil {
		t.Fatal("downgrade retained new kind")
	}
	upTo(t, db, 168)
	if _, err := db.Exec(statement, "agent_manager"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sessions SET activity_state='active' WHERE id='manager'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after <= before {
		t.Fatalf("new kind lost existing CDC: %d -> %d %v", before, after, err)
	}
	var check string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
		t.Fatalf("schema integrity: %s %v", check, err)
	}
}

func TestAgentManagerKindDowngradeCannotEraseOrRelabelRetainedController(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 168)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('manager-history','/repo',CURRENT_TIMESTAMP);
INSERT INTO sessions(id,project_id,num,kind,is_terminated,activity_last_at,created_at,updated_at)
VALUES('retained-manager','manager-history',1,'agent_manager',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	err := goose.DownTo(db, "migrations", 167)
	gooseMu.Unlock()
	if err == nil {
		t.Fatal("downgrade erased manager history")
	}
	var kind string
	if err := db.QueryRow(`SELECT kind FROM sessions WHERE id='retained-manager'`).Scan(&kind); err != nil || kind != "agent_manager" {
		t.Fatalf("failed downgrade changed history: %s %v", kind, err)
	}
	var guards int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name IN ('sessions_one_active_agent_manager','sessions_agent_manager_project_insert','sessions_agent_manager_identity_update')`).Scan(&guards); err != nil || guards != 3 {
		t.Fatalf("failed downgrade partially removed guards: %d %v", guards, err)
	}
	if _, err := db.Exec(`UPDATE sessions SET kind='worker' WHERE id='retained-manager'`); err == nil {
		t.Fatal("retained manager relabeled as worker")
	}
}
