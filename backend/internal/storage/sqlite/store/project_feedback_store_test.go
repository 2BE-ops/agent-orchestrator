package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestProjectFeedbackDerivesTerminalFacts(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpenAt(t, t.TempDir())
	seedProject(t, s, "project")
	setGoal(t, s, "project", "Drive the loop")

	_, result := taskEvaluationFixture(t, s, evaluationCriteria())
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	if _, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "passed", 0)); err != nil {
		t.Fatal(err)
	}
	createTask(t, s, "exhausted", func() domain.TaskDefinition {
		definition := taskDefinition()
		definition.MaxAttempts = 1
		return definition
	}())
	_, exhaustedLease := reserveTask(t, s, "exhausted-attempt", "exhausted")
	if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: exhaustedLease.TaskLeaseToken, Reason: "worker exited", Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	createTask(t, s, "cancelled", taskDefinition())
	if _, err := s.ChangeTaskIntent(ctx, "cancelled", domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)}); err != nil {
		t.Fatal(err)
	}
	createTask(t, s, "open", taskDefinition())

	items, err := s.ListProjectFeedback(ctx, "project", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]domain.ProjectFeedbackItem{}
	for _, item := range items {
		states[item.TaskID] = item
	}
	if len(items) != 4 {
		t.Fatalf("feedback items: %+v", items)
	}
	if work := states["work"]; work.State != "completed" || work.ResultID != result.ID || work.Title == "" {
		t.Fatalf("completed feedback: %+v", work)
	}
	if exhausted := states["exhausted"]; exhausted.State != "failed" || exhausted.Reason != "Task attempt limit is exhausted" {
		t.Fatalf("exhausted feedback: %+v", exhausted)
	}
	if cancelled := states["cancelled"]; cancelled.State != "cancelled" {
		t.Fatalf("cancelled feedback: %+v", cancelled)
	}
	if open := states["open"]; open.State != "pending" || open.Revision != 1 {
		t.Fatalf("open feedback: %+v", open)
	}
	if _, err := s.ListProjectFeedback(ctx, "project", "", 0); err == nil {
		t.Fatal("invalid page accepted")
	}
	page, err := s.ListProjectFeedback(ctx, "project", "cancelled", 100)
	if err != nil || len(page) != 3 || page[0].TaskID != "exhausted" {
		t.Fatalf("keyset page: %+v %v", page, err)
	}
}

func TestProjectFeedbackRestartStable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	before, err := s.ListProjectFeedback(ctx, "project", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	after, err := reopened.ListProjectFeedback(ctx, "project", "", 100)
	if err != nil || len(after) != len(before) || after[0] != before[0] {
		t.Fatalf("derived feedback changed across restart: %+v vs %+v (%v)", before, after, err)
	}
}
