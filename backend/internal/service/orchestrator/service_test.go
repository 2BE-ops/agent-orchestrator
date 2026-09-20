package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/orchestrator"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func seedOrchestrator(t *testing.T, s *sqlite.Store, project string) domain.SessionRecord {
	t.Helper()
	rec := domain.SessionRecord{ProjectID: domain.ProjectID(project), Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: time.Now().UTC()}, Metadata: domain.SessionMetadata{RuntimeLaunchID: "launch-native"}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	created, err := s.CreateSession(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func apiCode(t *testing.T, err error) string {
	t.Helper()
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected typed api error, got %v", err)
	}
	return apiErr.Code
}

func TestOrchestratorServiceGoalLifecycle(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	m := orchestrator.New(store)
	if _, err := m.Goal(ctx, "missing"); apiCode(t, err) != "PROJECT_NOT_FOUND" {
		t.Fatal("unknown project accepted")
	}
	actor := domain.AdaptiveActor{Kind: "USER", ID: "local-user"}
	first, err := m.SetGoal(ctx, actor, "project", orchestrator.GoalInput{Goal: "Ship the reporting milestone", Reason: "Record the goal"})
	if err != nil || first.Number != 1 {
		t.Fatalf("set goal: %+v %v", first, err)
	}
	second, err := m.SetGoal(ctx, actor, "project", orchestrator.GoalInput{Goal: "Ship it with tests", Reason: "Refine"})
	if err != nil || second.Number != 2 {
		t.Fatalf("refined goal: %+v %v", second, err)
	}
	current, err := m.Goal(ctx, "project")
	if err != nil || current.Number != 2 {
		t.Fatalf("current goal: %+v %v", current, err)
	}
	versions, err := m.GoalVersions(ctx, "project", 0, 100)
	if err != nil || len(versions) != 2 {
		t.Fatalf("goal history: %+v %v", versions, err)
	}
}

func TestOrchestratorServiceNativePlanningFencesLiveGeneration(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	m := orchestrator.New(store)
	actor := domain.AdaptiveActor{Kind: "USER", ID: "local-user"}
	if _, err := m.SetGoal(ctx, actor, "project", orchestrator.GoalInput{Goal: "Plan the pipeline", Reason: "Record"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.NativeGoal(ctx, "project"); apiCode(t, err) != "ORCHESTRATOR_NOT_FOUND" {
		t.Fatalf("native goal without an orchestrator: %v", err)
	}
	orch := seedOrchestrator(t, store, "project")
	goal, err := m.NativeGoal(ctx, "project")
	if err != nil || goal.SourceGeneration != "launch-native" || goal.SessionID != orch.ID {
		t.Fatalf("native goal: %+v %v", goal, err)
	}
	definition := domain.TaskDefinition{Title: "Planned decomposition", Brief: "First planned slice", MaxAttempts: 2}
	input := orchestrator.PlanInput{SourceGeneration: "stale-generation", IdempotencyKey: "plan-1", Action: domain.OrchestratorPlanAction{Action: "create_task", Definition: &definition, Reason: "Decompose"}}
	if _, err := m.Plan(ctx, "project", input); apiCode(t, err) != "ORCHESTRATOR_OWNER_CHANGED" {
		t.Fatalf("stale generation accepted: %v", err)
	}
	input.SourceGeneration = "launch-native"
	receipt, err := m.Plan(ctx, "project", input)
	if err != nil || !receipt.Created || receipt.Receipt.Outcome.TaskID == "" {
		t.Fatalf("planning refused: %+v %v", receipt, err)
	}
	replay, err := m.Plan(ctx, "project", input)
	if err != nil || replay.Created || replay.Receipt.Outcome.ReceiptID != receipt.Receipt.Outcome.ReceiptID {
		t.Fatalf("replay not idempotent: %+v %v", replay, err)
	}
	history, err := m.Receipts(ctx, "project", "", 100)
	if err != nil || len(history) != 1 {
		t.Fatalf("receipt history: %+v %v", history, err)
	}
	dead := orch
	dead.IsTerminated = true
	if err := store.UpdateSession(ctx, dead); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Plan(ctx, "project", input); apiCode(t, err) != "ORCHESTRATOR_NOT_FOUND" {
		t.Fatalf("terminated orchestrator planned: %v", err)
	}
}

func TestOrchestratorServiceCompleteIsDeterministic(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	m := orchestrator.New(store)
	actor := domain.AdaptiveActor{Kind: "USER", ID: "local-user"}
	goal, err := m.SetGoal(ctx, actor, "project", orchestrator.GoalInput{Goal: "Verified delivery", Reason: "Record"})
	if err != nil {
		t.Fatal(err)
	}
	seedOrchestrator(t, store, "project")
	input := orchestrator.CompleteInput{SourceGeneration: "launch-native", GoalVersion: goal.Number, Summary: "Delivered", Reason: "All work terminal"}
	if _, _, err := m.Complete(ctx, "project", input); apiCode(t, err) != "GOAL_INCOMPLETE" {
		t.Fatalf("unverified project completed: %v", err)
	}
	definition := domain.TaskDefinition{Title: "Planned work", Brief: "Slice", MaxAttempts: 1}
	if _, err := m.Plan(ctx, "project", orchestrator.PlanInput{SourceGeneration: "launch-native", IdempotencyKey: "cancel-1", Action: domain.OrchestratorPlanAction{Action: "create_task", Definition: &definition, Reason: "Plan"}}); err != nil {
		t.Fatal(err)
	}
	receipts, err := m.Receipts(ctx, "project", "", 100)
	if err != nil || len(receipts) != 1 {
		t.Fatalf("receipts: %+v %v", receipts, err)
	}
	var apiErr *apierr.Error
	_, _, err = m.Complete(ctx, "project", input)
	if !errors.As(err, &apiErr) || len(apiErr.Details["blockers"].([]map[string]any)) != 2 || apiErr.Details["blockers"].([]map[string]any)[0]["state"] != "pending" {
		t.Fatalf("blocker details lost: %+v", apiErr)
	}
	stale := input
	stale.SourceGeneration = "replaced-generation"
	if _, _, err := m.Complete(ctx, "project", stale); apiCode(t, err) != "ORCHESTRATOR_OWNER_CHANGED" {
		t.Fatalf("stale completion source accepted: %v", err)
	}
}
