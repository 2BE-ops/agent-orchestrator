package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/orchestrator"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestOrchestratorPlanningAttributionReads(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	m := orchestrator.New(store)
	if _, err := m.PlanningOutcomes(ctx, "missing", "", 20); apiCode(t, err) != "PROJECT_NOT_FOUND" {
		t.Fatal("unknown project accepted")
	}
	session := seedOrchestrator(t, store, "project")
	definition := domain.TaskDefinition{Title: "Planned slice", Brief: "Deliver the slice", MaxAttempts: 2}
	submission := domain.OrchestratorPlanSubmission{ReceiptID: "receipt-1", ProjectID: "project", SessionID: session.ID, IdempotencyKey: "plan-1", Action: domain.OrchestratorPlanAction{Action: "create_task", Definition: &definition, Reason: "Decompose"}, NewTaskID: "planned-1", Now: time.Now().UTC()}
	if _, _, err := store.ApplyOrchestratorPlan(ctx, submission); err != nil {
		t.Fatal(err)
	}
	outcomes, err := m.PlanningOutcomes(ctx, "project", "", 20)
	if err != nil || len(outcomes) != 1 {
		t.Fatalf("planning outcomes: %+v %v", outcomes, err)
	}
	if outcomes[0].TaskID != "planned-1" || outcomes[0].Action != "create_task" || outcomes[0].State != "pending" || outcomes[0].ReceiptID != "receipt-1" {
		t.Fatalf("planning outcome row: %+v", outcomes[0])
	}
	now := time.Now().UTC()
	summary, err := m.PlanningSummary(ctx, "project", domain.OutcomeAttributionQuery{ProjectID: "project", From: now.Add(-time.Hour), To: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Totals.Receipts != 1 || summary.Totals.CreateTask != 1 || summary.Totals.PlannedTaskStates.Pending != 1 || summary.Totals.PlannedTaskStates.Completed != 0 {
		t.Fatalf("planning summary: %+v", summary.Totals)
	}
	if _, err := m.PlanningOutcomes(ctx, "project", "", 0); apiCode(t, err) != "INVALID_PLANNING_PAGE" {
		t.Fatalf("invalid page accepted: %v", err)
	}
	if _, err := m.PlanningOutcomes(ctx, "project", "", 101); apiCode(t, err) != "INVALID_PLANNING_PAGE" {
		t.Fatalf("oversized page accepted: %v", err)
	}
	if _, err := m.PlanningSummary(ctx, "project", domain.OutcomeAttributionQuery{ProjectID: "project", From: now, To: now.Add(-time.Hour)}); apiCode(t, err) != "INVALID_ATTRIBUTION_WINDOW" {
		t.Fatalf("inverted window accepted: %v", err)
	}
	if _, err := m.PlanningSummary(ctx, "project", domain.OutcomeAttributionQuery{ProjectID: "project", From: now.Add(-367 * 24 * time.Hour), To: now}); apiCode(t, err) != "INVALID_ATTRIBUTION_WINDOW" {
		t.Fatalf("oversized window accepted: %v", err)
	}
}
