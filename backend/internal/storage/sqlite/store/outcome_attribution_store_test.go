package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func exhaustTaskAttempts(t *testing.T, s *sqlite.Store, taskID string, attempts int, revision int64) {
	t.Helper()
	for i := range attempts {
		reservation := taskReservation(taskID+"-attempt-"+string(rune('1'+i)), taskID)
		reservation.Mutation = taskMutation(revision)
		_, lease, err := s.ReserveTask(context.Background(), reservation)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReleaseTaskLease(context.Background(), domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, Reason: "worker exited", Now: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagerRoutingOutcomesAttributeTaskFate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, assessment := managerDecisionFixture(t, s, domain.ContextTechnical)
	decision, created, err := s.RecordAgentManagerDecision(ctx, assessment)
	if err != nil || !created || decision.Outcome != "accepted" {
		t.Fatalf("accepted decision: %+v %v", decision, err)
	}
	outcomes, err := s.ListManagerRoutingOutcomes(ctx, "project", 0, 100)
	if err != nil || len(outcomes) != 1 {
		t.Fatalf("routing outcomes: %+v %v", outcomes, err)
	}
	routed := outcomes[0]
	if routed.DecisionID != input.ID || routed.RequestID != input.RequestID || routed.TaskID != "decision-task" {
		t.Fatalf("routing identity: %+v", routed)
	}
	if routed.Outcome != "accepted" || routed.AgentType == nil || routed.AgentType.ID != "unvalidated-type" || routed.AgentType.Version != 1 {
		t.Fatalf("accepted routing must retain its selected type: %+v", routed)
	}
	if routed.State != "pending" || routed.Attempts != 0 || routed.Revision != 1 || routed.TaskTitle == "" {
		t.Fatalf("pending routed task: %+v", routed)
	}
	exhaustTaskAttempts(t, s, "decision-task", 3, 1)
	outcomes, err = s.ListManagerRoutingOutcomes(ctx, "project", 0, 100)
	if err != nil || len(outcomes) != 1 || outcomes[0].State != "failed" || outcomes[0].Attempts != 3 {
		t.Fatalf("exhausted routing outcome: %+v %v", outcomes, err)
	}
	now := time.Now().UTC()
	summary, err := s.ManagerRoutingSummary(ctx, domain.OutcomeAttributionQuery{ProjectID: "project", From: now.Add(-time.Hour), To: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Totals.Decisions != 1 || summary.Totals.Accepted != 1 || summary.Totals.Rejected != 0 {
		t.Fatalf("routing totals: %+v", summary.Totals)
	}
	if summary.Totals.RoutedTaskStates.Failed != 1 || summary.Totals.RoutedTaskStates.Pending != 0 || summary.Totals.RoutedTaskAttempts != 3 {
		t.Fatalf("routed task states: %+v", summary.Totals)
	}
	if len(summary.Types) != 1 || summary.Types[0].AgentTypeID != "unvalidated-type" || summary.Types[0].Routed != 1 || summary.Types[0].Failed != 1 || summary.Types[0].Completed != 0 {
		t.Fatalf("type attribution: %+v", summary.Types)
	}
	excluded, err := s.ManagerRoutingSummary(ctx, domain.OutcomeAttributionQuery{ProjectID: "project", From: now.Add(-2 * time.Hour), To: now.Add(-time.Hour)})
	if err != nil || excluded.Totals.Decisions != 0 || len(excluded.Types) != 0 {
		t.Fatalf("window must exclude the decision: %+v %v", excluded, err)
	}
	request, err := s.GetAgentManagerRequest(ctx, "project", input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	tail, err := s.ListManagerRoutingOutcomes(ctx, "project", request.Sequence, 100)
	if err != nil || len(tail) != 0 {
		t.Fatalf("keyset page after the request: %+v %v", tail, err)
	}
	if _, err := s.ListManagerRoutingOutcomes(ctx, "project", 0, 0); !errors.Is(err, ports.ErrOutcomePageInvalid) {
		t.Fatalf("invalid page accepted: %v", err)
	}
	if _, err := s.ManagerRoutingSummary(ctx, domain.OutcomeAttributionQuery{ProjectID: "project", From: now.Add(time.Hour), To: now}); !errors.Is(err, ports.ErrOutcomePageInvalid) {
		t.Fatalf("inverted window accepted: %v", err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	after, err := reopened.ListManagerRoutingOutcomes(ctx, "project", 0, 100)
	if err != nil || len(after) != 1 || after[0].DecisionID != outcomes[0].DecisionID || after[0].State != outcomes[0].State || after[0].Attempts != outcomes[0].Attempts || after[0].AgentType.ID != outcomes[0].AgentType.ID || after[0].AgentType.Version != outcomes[0].AgentType.Version {
		t.Fatalf("routing attribution changed across restart: %+v vs %+v (%v)", after, outcomes, err)
	}
}

func TestManagerRoutingOutcomesCountRejectionsWithoutTaskInflation(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpenAt(t, t.TempDir())
	input, assessment := managerDecisionFixture(t, s, domain.ContextTechnical)
	assessment.Candidates[0].Eligible = false
	assessment.Candidates[0].Issues = []domain.AgentManagerCandidateIssue{{Code: "missing_capability", State: "unavailable"}}
	decision, created, err := s.RecordAgentManagerDecision(ctx, assessment)
	if err != nil || !created || decision.Outcome != "rejected" {
		t.Fatalf("rejected decision: %+v %v", decision, err)
	}
	outcomes, err := s.ListManagerRoutingOutcomes(ctx, "project", 0, 100)
	if err != nil || len(outcomes) != 1 {
		t.Fatalf("routing outcomes: %+v %v", outcomes, err)
	}
	rejected := outcomes[0]
	if rejected.Outcome != "rejected" || rejected.AgentType != nil || rejected.State != "pending" {
		t.Fatalf("rejected routing must not claim a selection: %+v", rejected)
	}
	now := time.Now().UTC()
	summary, err := s.ManagerRoutingSummary(ctx, domain.OutcomeAttributionQuery{ProjectID: "project", From: now.Add(-time.Hour), To: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Totals.Decisions != 1 || summary.Totals.Rejected != 1 || summary.Totals.Accepted != 0 {
		t.Fatalf("rejection totals: %+v", summary.Totals)
	}
	if summary.Totals.RoutedTaskStates != (domain.TaskOutcomeTotals{}) || summary.Totals.RoutedTaskAttempts != 0 || len(summary.Types) != 0 {
		t.Fatalf("rejection must not inflate routed work: %+v", summary.Totals)
	}
	if rejected.DecisionID != input.ID {
		t.Fatalf("rejected decision identity: %+v", rejected)
	}
}

func TestOrchestratorPlanningOutcomesAttributeCreatedTasks(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	orch := orchestratorSession(t, s, "project")
	definition := taskDefinition()
	if _, _, err := s.ApplyOrchestratorPlan(ctx, planSubmission(orch.ID, "plan-1", domain.OrchestratorPlanAction{Action: "create_task", Definition: &definition, Reason: "Cancelled slice"})); err != nil {
		t.Fatal(err)
	}
	failable := definition
	failable.MaxAttempts = 2
	if _, _, err := s.ApplyOrchestratorPlan(ctx, planSubmission(orch.ID, "plan-2", domain.OrchestratorPlanAction{Action: "create_task", Definition: &failable, Reason: "Failed slice"})); err != nil {
		t.Fatal(err)
	}
	frozen := taskCriteria()
	if _, _, err := s.ApplyOrchestratorPlan(ctx, planSubmission(orch.ID, "plan-2b", domain.OrchestratorPlanAction{Action: "freeze_criteria", TaskID: "task-plan-2", ExpectedRevision: 1, Criteria: &frozen, Reason: "Freeze before dispatch"})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ApplyOrchestratorPlan(ctx, planSubmission(orch.ID, "plan-3", domain.OrchestratorPlanAction{Action: "create_task", Definition: &definition, Reason: "Open slice"})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeTaskIntent(ctx, "task-plan-1", domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)}); err != nil {
		t.Fatal(err)
	}
	exhaustTaskAttempts(t, s, "task-plan-2", 2, 2)
	if _, _, err := s.ApplyOrchestratorPlan(ctx, planSubmission(orch.ID, "plan-4", domain.OrchestratorPlanAction{Action: "revise_task", TaskID: "task-plan-3", ExpectedRevision: 1, Definition: &definition, Reason: "Refine the open slice"})); err != nil {
		t.Fatal(err)
	}
	criteria := taskCriteria()
	if _, _, err := s.ApplyOrchestratorPlan(ctx, planSubmission(orch.ID, "plan-5", domain.OrchestratorPlanAction{Action: "freeze_criteria", TaskID: "task-plan-3", ExpectedRevision: 2, Criteria: &criteria, Reason: "Freeze acceptance"})); err != nil {
		t.Fatal(err)
	}
	outcomes, err := s.ListOrchestratorPlanningOutcomes(ctx, "project", "", 100)
	if err != nil || len(outcomes) != 6 {
		t.Fatalf("planning outcomes: %+v %v", outcomes, err)
	}
	states := map[string]domain.OrchestratorPlanningOutcome{}
	for _, item := range outcomes {
		states[item.TaskID] = item
	}
	if item := states["task-plan-1"]; item.State != "cancelled" || item.Action != "create_task" || item.TaskTitle == "" {
		t.Fatalf("cancelled planned task: %+v", item)
	}
	if item := states["task-plan-2"]; item.State != "failed" || item.Attempts != 2 {
		t.Fatalf("failed planned task: %+v", item)
	}
	if item := states["task-plan-3"]; item.State != "pending" || item.Revision != 3 {
		t.Fatalf("planned task revisions collapse to current state: %+v", item)
	}
	if outcomes[0].ReceiptID != "receipt-plan-1" {
		t.Fatalf("planning outcome order: %+v", outcomes[0])
	}
	tail, err := s.ListOrchestratorPlanningOutcomes(ctx, "project", "receipt-plan-3", 100)
	if err != nil || len(tail) != 2 || tail[0].ReceiptID != "receipt-plan-4" {
		t.Fatalf("planning keyset page: %+v %v", tail, err)
	}
	now := time.Now().UTC()
	summary, err := s.OrchestratorPlanningSummary(ctx, domain.OutcomeAttributionQuery{ProjectID: "project", From: now.Add(-time.Hour), To: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Totals.Receipts != 6 || summary.Totals.CreateTask != 3 || summary.Totals.ReviseTask != 1 || summary.Totals.FreezeCriteria != 2 {
		t.Fatalf("planning action totals: %+v", summary.Totals)
	}
	if summary.Totals.PlannedTaskStates.Cancelled != 1 || summary.Totals.PlannedTaskStates.Failed != 1 || summary.Totals.PlannedTaskStates.Pending != 1 {
		t.Fatalf("planned task states count each created task once: %+v", summary.Totals)
	}
	if summary.Totals.PlannedTaskAttempts != 2 {
		t.Fatalf("planned attempts: %+v", summary.Totals)
	}
	excluded, err := s.OrchestratorPlanningSummary(ctx, domain.OutcomeAttributionQuery{ProjectID: "project", From: now.Add(-2 * time.Hour), To: now.Add(-time.Hour)})
	if err != nil || excluded.Totals.Receipts != 0 {
		t.Fatalf("window must exclude receipts: %+v %v", excluded, err)
	}
	if _, err := s.ListOrchestratorPlanningOutcomes(ctx, "project", "", 0); !errors.Is(err, ports.ErrOutcomePageInvalid) {
		t.Fatalf("invalid page accepted: %v", err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	after, err := reopened.ListOrchestratorPlanningOutcomes(ctx, "project", "", 100)
	if err != nil || len(after) != 6 || after[0] != outcomes[0] {
		t.Fatalf("planning attribution changed across restart: %+v vs %+v (%v)", after, outcomes, err)
	}
}
