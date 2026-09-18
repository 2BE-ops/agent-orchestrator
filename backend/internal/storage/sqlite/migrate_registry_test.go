package sqlite

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestRegistryUpgradePreservesSessionsAndCDC(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 147)
	if _, err := db.Exec(`
INSERT INTO projects (id, path, registered_at) VALUES ('registry-upgrade', '/repo', CURRENT_TIMESTAMP);
INSERT INTO sessions (id, project_id, num, activity_last_at, created_at, updated_at, latest_user_prompt)
VALUES ('registry-upgrade-1', 'registry-upgrade', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'retain this prompt');
`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	var prompt string
	if err := db.QueryRow(`SELECT latest_user_prompt FROM sessions WHERE id = 'registry-upgrade-1'`).Scan(&prompt); err != nil || prompt != "retain this prompt" {
		t.Fatalf("legacy session: %q %v", prompt, err)
	}
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || before != after {
		t.Fatalf("CDC changed during upgrade: %d -> %d %v", before, after, err)
	}
	if _, err := db.Exec(`UPDATE sessions SET activity_state = 'active' WHERE id = 'registry-upgrade-1'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || after <= before {
		t.Fatal("existing CDC triggers were lost")
	}
	if _, err := db.Exec(`INSERT INTO change_log (event_type, payload) VALUES ('registry_changed', '{"id":"x","kind":"skill","revision":1}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO change_log (event_type, payload) VALUES ('unknown_event', '{}')`); err == nil {
		t.Fatal("CDC vocabulary constraint was removed")
	}
	if err := migrate(db); err != nil {
		t.Fatalf("reopening migrated schema: %v", err)
	}
	var writable int
	if err := db.QueryRow(`PRAGMA writable_schema`).Scan(&writable); err != nil || writable != 0 {
		t.Fatal("schema editing left enabled")
	}
}

func TestRegistryDatabaseProtectsHistoryAndSkillKinds(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON;
BEGIN;
INSERT INTO adaptive_registry (id, kind, name, description, origin, created_by, enabled, manager_can_select, manager_can_modify, manager_can_version, revision, active_version, created_at, updated_at)
VALUES ('type', 'agent_type', 'Type', '', 'USER', 'local', 1, 1, 0, 0, 1, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO adaptive_registry_versions (entry_id, number, kind, definition, content_hash, actor_origin, actor_id, reason, created_at)
VALUES ('type', 1, 'agent_type', '{}', printf('%064d', 0), 'USER', 'local', 'create', CURRENT_TIMESTAMP);
INSERT INTO adaptive_registry_audit (entry_id, revision, action, version_number, actor_origin, actor_id, reason, created_at)
VALUES ('type', 1, 'created', 1, 'USER', 'local', 'create', CURRENT_TIMESTAMP);
COMMIT;`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE adaptive_registry_versions SET definition = '{"changed":true}' WHERE entry_id = 'type'`,
		`DELETE FROM adaptive_registry_versions WHERE entry_id = 'type'`,
		`UPDATE adaptive_registry SET origin = 'AGENT_MANAGER', revision = revision + 1 WHERE id = 'type'`,
		`UPDATE adaptive_registry SET revision = revision + 2 WHERE id = 'type'`,
		`UPDATE adaptive_registry SET active_version = 99, revision = revision + 1 WHERE id = 'type'`,
		`INSERT INTO adaptive_registry_skill_pins (entry_id, version, position, skill_id, skill_version) VALUES ('type', 1, 0, 'type', 1)`,
		`UPDATE adaptive_registry_audit SET reason = 'rewritten'`,
		`DELETE FROM adaptive_registry_audit`,
	} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("historical mutation succeeded: %s", statement)
		}
	}
	// Even an unsealed version cannot pin an Agent Type as if it were a Skill.
	if _, err := db.Exec(`INSERT INTO adaptive_registry_skill_pins (entry_id, version, position, skill_id, skill_version) VALUES ('type', 2, 0, 'type', 1)`); err == nil {
		t.Fatal("cross-kind skill reference accepted")
	}
}

func TestRegistrySurvivesStoreRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, createErr := s.CreateRegistryEntry(ctx, "restart-skill", domain.RegistrySkill,
		domain.RegistryMetadata{Name: "Restart", Enabled: true},
		domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Retain history"}},
		domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "local"}, Reason: "create"})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if createErr != nil {
		t.Fatal(createErr)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	version, err := s.GetRegistryVersion(ctx, "restart-skill", 1)
	if err != nil || version.Definition.Skill.Instructions != "Retain history" {
		t.Fatalf("restart lost history: %+v %v", version, err)
	}
	audit, err := s.ListRegistryAudit(ctx, "restart-skill", 0, 10)
	if err != nil || len(audit) != 1 {
		t.Fatal("restart lost audit")
	}
}

func TestRegistryAuditFailureRollsBackBusinessMutation(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER registry_audit_failure BEFORE INSERT ON adaptive_registry_audit BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END;`); err != nil {
		t.Fatal(err)
	}
	s := sqlitestore.NewStore(db, db)
	_, err := s.CreateRegistryEntry(context.Background(), "rollback", domain.RegistrySkill,
		domain.RegistryMetadata{Name: "Rollback", Enabled: true},
		domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Test atomicity"}},
		domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "local"}, Reason: "create"})
	if err == nil {
		t.Fatal("injected audit failure ignored")
	}
	for _, query := range []string{
		`SELECT count(*) FROM adaptive_registry`,
		`SELECT count(*) FROM adaptive_registry_versions`,
		`SELECT count(*) FROM adaptive_registry_audit`,
		`SELECT count(*) FROM change_log WHERE event_type = 'registry_changed'`,
	} {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial transaction retained: %s = %d %v", query, count, err)
		}
	}
}
