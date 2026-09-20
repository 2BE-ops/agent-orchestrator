package sqlite

import (
	"context"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestProjectControlsMigrationSealsControlAndNeedsHuman(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 182)
	// The round trip must precede any rows: retained history refuses downgrade.
	var before, after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	downTo(t, db, 181)
	upTo(t, db, 182)
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || before != after {
		t.Fatalf("migration emitted CDC: %d %d %v", before, after, err)
	}
	ctx := context.Background()
	s := sqlitestore.NewStore(db, db)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('project','/repo',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdaptiveTask(ctx, "task-1", "project", domain.TaskDefinition{Title: "Check the release", Brief: "Verify the release checklist", Category: "chore", MaxAttempts: 3}, nil, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Seed work", ExpectedRevision: 0}); err != nil {
		t.Fatal(err)
	}

	controlActor := `{"kind":"USER","id":"local-user"}`
	if _, err := db.Exec(`INSERT INTO adaptive_project_controls(project_id,state,actor,reason,updated_at)
VALUES('project','paused',?,'Evening maintenance',CURRENT_TIMESTAMP)`, controlActor); err != nil {
		t.Fatal(err)
	}
	// Unknown projects and actor-less rows are refused.
	if _, err := db.Exec(`INSERT INTO adaptive_project_controls(project_id,state,actor,reason,updated_at)
VALUES('ghost','paused',?,'No such project',CURRENT_TIMESTAMP)`, controlActor); err == nil {
		t.Fatal("control scope not enforced")
	}
	if _, err := db.Exec(`INSERT INTO adaptive_project_controls(project_id,state,actor,reason,updated_at)
VALUES('project','paused','{}','Missing actor',CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("control actor not enforced")
	}
	// The state machine is enforced at the database layer.
	if _, err := db.Exec(`UPDATE adaptive_project_controls SET state='draining' WHERE project_id='project'`); err == nil {
		t.Fatal("paused -> draining transition not refused")
	}
	if _, err := db.Exec(`UPDATE adaptive_project_controls SET project_id='ghost' WHERE project_id='project'`); err == nil {
		t.Fatal("control project rewrite not refused")
	}
	if _, err := db.Exec(`UPDATE adaptive_project_controls SET state='running',reason='Resumed',updated_at=CURRENT_TIMESTAMP WHERE project_id='project'`); err != nil {
		t.Fatalf("legal resume refused: %v", err)
	}
	// Same-state update without another field changing is the no-op the store
	// skips, so the database keeps requiring a real transition.
	if _, err := db.Exec(`UPDATE adaptive_project_controls SET state='running' WHERE project_id='project'`); err == nil {
		t.Fatal("same-state rewrite not refused")
	}
	if _, err := db.Exec(`DELETE FROM adaptive_project_controls WHERE project_id='project'`); err == nil {
		t.Fatal("control deletion not refused")
	}

	needsSnapshot := `{"id":"nh-1","taskId":"task-1","projectId":"project","reasonCode":"credential_missing","detail":"Expired provider credential","actor":{"kind":"WORKER","id":"worker-1"},"createdAt":"2026-09-19T12:00:00Z","resolution":""}`
	needsActor := `{"kind":"WORKER","id":"worker-1"}`
	if _, err := db.Exec(`INSERT INTO adaptive_task_needs_human(id,task_id,project_id,reason_code,detail,actor,snapshot,content_hash,created_at)
VALUES('nh-1','task-1','project','credential_missing','Expired provider credential',?,?,printf('%064d',1),CURRENT_TIMESTAMP)`, needsActor, needsSnapshot); err != nil {
		t.Fatal(err)
	}
	// Scope and echo mismatches are refused.
	if _, err := db.Exec(`INSERT INTO adaptive_task_needs_human(id,task_id,project_id,reason_code,detail,actor,snapshot,content_hash,created_at)
VALUES('nh-2','ghost','project','credential_missing','Wrong task',?,?,printf('%064d',2),CURRENT_TIMESTAMP)`, needsActor, needsSnapshot); err == nil {
		t.Fatal("needs human scope not enforced")
	}
	crossProject := `{"id":"nh-3","taskId":"task-1","projectId":"elsewhere","reasonCode":"credential_missing","detail":"Wrong project","actor":{"kind":"WORKER","id":"worker-1"},"createdAt":"2026-09-19T12:00:00Z","resolution":""}`
	if _, err := db.Exec(`INSERT INTO adaptive_task_needs_human(id,task_id,project_id,reason_code,detail,actor,snapshot,content_hash,created_at)
VALUES('nh-3','task-1','elsewhere','credential_missing','Wrong project',?,?,printf('%064d',3),CURRENT_TIMESTAMP)`, needsActor, crossProject); err == nil {
		t.Fatal("cross-project needs human not enforced")
	}
	tampered := `{"id":"other","taskId":"task-1","projectId":"project","reasonCode":"credential_missing","detail":"Expired provider credential","actor":{"kind":"WORKER","id":"worker-1"},"createdAt":"2026-09-19T12:00:00Z","resolution":""}`
	if _, err := db.Exec(`INSERT INTO adaptive_task_needs_human(id,task_id,project_id,reason_code,detail,actor,snapshot,content_hash,created_at)
VALUES('nh-4','task-1','project','credential_missing','Echo mismatch',?,?,printf('%064d',4),CURRENT_TIMESTAMP)`, needsActor, tampered); err == nil {
		t.Fatal("snapshot echo not enforced")
	}
	// Exactly one pending row per task.
	if _, err := db.Exec(`INSERT INTO adaptive_task_needs_human(id,task_id,project_id,reason_code,detail,actor,snapshot,content_hash,created_at)
VALUES('nh-5','task-1','project','approval_required','Second request',?,?,printf('%064d',5),CURRENT_TIMESTAMP)`, needsActor, needsSnapshot); err == nil {
		t.Fatal("duplicate pending needs human not enforced")
	}

	// The one allowed mutation is the single pending -> resolved seal.
	resolution := `{"resolution":"Rotated the credential","actor":{"kind":"USER","id":"local-user"},"resolvedAt":"2026-09-19T13:00:00Z"}`
	resolvedBy := `{"kind":"USER","id":"local-user"}`
	if _, err := db.Exec(`UPDATE adaptive_task_needs_human SET resolved_at=CURRENT_TIMESTAMP,resolution=?,resolved_by=? WHERE task_id='task-1'`, resolution, resolvedBy); err != nil {
		t.Fatalf("legal resolve refused: %v", err)
	}
	if _, err := db.Exec(`UPDATE adaptive_task_needs_human SET detail='Rewritten history' WHERE id='nh-1'`); err == nil {
		t.Fatal("needs human rewrite not refused")
	}
	if _, err := db.Exec(`DELETE FROM adaptive_task_needs_human WHERE id='nh-1'`); err == nil {
		t.Fatal("needs human deletion not refused")
	}
	// A resolved task may raise a new request.
	secondSnapshot := `{"id":"nh-6","taskId":"task-1","projectId":"project","reasonCode":"approval_required","detail":"Needs a release sign-off","actor":{"kind":"USER","id":"local-user"},"createdAt":"2026-09-19T14:00:00Z","resolution":""}`
	if _, err := db.Exec(`INSERT INTO adaptive_task_needs_human(id,task_id,project_id,reason_code,detail,actor,snapshot,content_hash,created_at)
VALUES('nh-6','task-1','project','approval_required','Needs a release sign-off',?,?,printf('%064d',6),CURRENT_TIMESTAMP)`, resolvedBy, secondSnapshot); err != nil {
		t.Fatalf("re-raise after resolve refused: %v", err)
	}

	// Retained history refuses the downgrade.
	if err := goose.DownTo(db, "migrations", 181); err == nil {
		t.Fatal("downgrade with retained control history must fail")
	}
}
