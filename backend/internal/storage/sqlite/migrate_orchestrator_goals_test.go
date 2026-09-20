package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestOrchestratorGoalMigrationRoundTripAndGuards(t *testing.T) {
	db := openMigratedDatabaseCopy(t, 140)
	upTo(t, db, 179)
	ctx := context.Background()
	s := sqlitestore.NewStore(db, db)
	if _, err := db.Exec(`INSERT INTO projects(id,path,registered_at) VALUES('project','/repo',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 180)
	downTo(t, db, 179)
	upTo(t, db, 180)
	if err := db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after); err != nil || before != after {
		t.Fatalf("migration emitted CDC: %d %d %v", before, after, err)
	}
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	goal, err := s.SetProjectGoal(ctx, "project", "Ship the milestone", "Record intent", actor)
	if err != nil {
		t.Fatal(err)
	}
	orch, err := s.CreateSession(ctx, domain.SessionRecord{ProjectID: "project", Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	definition := domain.TaskDefinition{Title: "Planned work", Brief: "Decomposed by the orchestrator", MaxAttempts: 2}
	submission := domain.OrchestratorPlanSubmission{ReceiptID: "receipt", ProjectID: "project", SessionID: orch.ID, IdempotencyKey: "plan", Action: domain.OrchestratorPlanAction{Action: "create_task", Definition: &definition, Reason: "Decompose"}, NewTaskID: "task", Now: time.Now().UTC()}
	if _, _, err := s.ApplyOrchestratorPlan(ctx, submission); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetProjectGoal(ctx, "project")
	if err != nil || current.Number != goal.Number || current.Goal != "Ship the milestone" {
		t.Fatalf("goal rewritten across round trip: %+v %v", current, err)
	}
	receipts, err := s.ListOrchestratorPlanReceipts(ctx, "project", "", 100)
	if err != nil || len(receipts) != 1 || receipts[0].Outcome.TaskID != "task" {
		t.Fatalf("plan receipts lost: %+v %v", receipts, err)
	}
	// Completion evidence carrying blockers must be refused by the schema guard
	// even when every other field matches the current goal version.
	_, err = db.Exec(`INSERT INTO adaptive_project_goal_completions(id,project_id,goal_version,summary,evidence,actor,reason,created_at) VALUES('bad','project',1,'s',?,'{"kind":"USER","id":"human"}','r',CURRENT_TIMESTAMP)`,
		`{"projectId":"project","goalVersion":1,"goalHash":"`+goal.ContentHash+`","verifiedTasks":[],"blockers":[{"taskId":"task"}]}`)
	if err == nil {
		t.Fatal("blocker-bearing completion evidence accepted")
	}
	gooseMu.Lock()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	err = goose.DownTo(db, "migrations", 179)
	gooseMu.Unlock()
	if err == nil {
		t.Fatal("downgrade erased goal and planning history")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM adaptive_orchestrator_plan_receipts`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("receipt lost: %d %v", count, err)
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %s %v", integrity, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign keys: %d %v", count, err)
	}
}
