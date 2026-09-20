package sqlite

import "testing"

func TestManagerDecisionCursorMigrationIsIndependentAndResettable(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 176)
	upTo(t, db, 177)
	if _, err := db.Exec(`UPDATE adaptive_agent_manager_decision_cursor SET after_proposal_id='retained-position' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	downTo(t, db, 176)
	upTo(t, db, 177)
	var cursor string
	if err := db.QueryRow(`SELECT after_proposal_id FROM adaptive_agent_manager_decision_cursor WHERE id=1`).Scan(&cursor); err != nil || cursor != "" {
		t.Fatalf("recovery checkpoint: %s %v", cursor, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign keys: %d %v", count, err)
	}
}
