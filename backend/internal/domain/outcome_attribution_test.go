package domain_test

import (
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func attributionQuery() domain.OutcomeAttributionQuery {
	now := time.Now().UTC()
	return domain.OutcomeAttributionQuery{ProjectID: "project", From: now.Add(-time.Hour), To: now.Add(time.Hour)}
}

func routingOutcome(requestID, state string, accepted bool) domain.ManagerRoutingOutcome {
	outcome := domain.ManagerRoutingOutcome{
		DecisionID:   "decision-" + requestID,
		RequestID:    requestID,
		TaskID:       "task-" + requestID,
		TaskTitle:    "Slice " + requestID,
		Revision:     1,
		Optimization: "balanced",
		State:        state,
		Reason:       "Derived",
		Attempts:     1,
		DecidedAt:    time.Now().UTC(),
	}
	if accepted {
		outcome.Outcome = "accepted"
		selected := domain.WorkerDefinitionRef{ID: "type-a", Version: 2, Name: "Type A"}
		outcome.AgentType = &selected
	} else {
		outcome.Outcome = "rejected"
	}
	return outcome
}

func TestSummarizeManagerRoutingCountsDecisionsAndTypeGroups(t *testing.T) {
	query := attributionQuery()
	summary, err := domain.SummarizeManagerRouting(query, []domain.ManagerRoutingOutcome{
		routingOutcome("one", "completed", true),
		routingOutcome("two", "failed", true),
		routingOutcome("three", "working", true),
		routingOutcome("four", "pending", false),
		func() domain.ManagerRoutingOutcome {
			item := routingOutcome("five", "cancelled", true)
			selected := domain.WorkerDefinitionRef{ID: "type-b", Version: 1, Name: "Type B"}
			item.AgentType = &selected
			return item
		}(),
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Totals.Decisions != 5 || summary.Totals.Accepted != 4 || summary.Totals.Rejected != 1 {
		t.Fatalf("decision totals: %+v", summary.Totals)
	}
	if summary.Totals.RoutedTaskStates.Completed != 1 || summary.Totals.RoutedTaskStates.Failed != 1 || summary.Totals.RoutedTaskStates.Working != 1 || summary.Totals.RoutedTaskStates.Cancelled != 1 || summary.Totals.RoutedTaskAttempts != 4 {
		t.Fatalf("routed work must exclude the rejection: %+v", summary.Totals)
	}
	if len(summary.Types) != 2 {
		t.Fatalf("type groups: %+v", summary.Types)
	}
	first := summary.Types[0]
	if first.AgentTypeID != "type-a" || first.Version != 2 || first.Routed != 3 || first.Completed != 1 || first.Failed != 1 || first.Open != 1 || first.Cancelled != 0 {
		t.Fatalf("type-a group: %+v", first)
	}
	second := summary.Types[1]
	if second.AgentTypeID != "type-b" || second.Routed != 1 || second.Cancelled != 1 {
		t.Fatalf("type-b group: %+v", second)
	}
	if !summary.From.Equal(query.From) || !summary.To.Equal(query.To) || summary.ObservedAt.IsZero() {
		t.Fatalf("summary window: %+v", summary)
	}
}

func TestSummarizeManagerRoutingRefusesBrokenCohorts(t *testing.T) {
	query := attributionQuery()
	duplicate := routingOutcome("one", "pending", true)
	if _, err := domain.SummarizeManagerRouting(query, []domain.ManagerRoutingOutcome{duplicate, duplicate}, time.Now().UTC()); err == nil {
		t.Fatal("duplicate request attribution accepted")
	}
	untyped := routingOutcome("two", "pending", true)
	untyped.AgentType = nil
	if _, err := domain.SummarizeManagerRouting(query, []domain.ManagerRoutingOutcome{untyped}, time.Now().UTC()); err == nil {
		t.Fatal("accepted routing without its selected type accepted")
	}
	unknownState := routingOutcome("three", "vanished", true)
	if _, err := domain.SummarizeManagerRouting(query, []domain.ManagerRoutingOutcome{unknownState}, time.Now().UTC()); err == nil {
		t.Fatal("unknown task state accepted")
	}
	unknownOutcome := routingOutcome("four", "pending", false)
	unknownOutcome.Outcome = "deferred"
	if _, err := domain.SummarizeManagerRouting(query, []domain.ManagerRoutingOutcome{unknownOutcome}, time.Now().UTC()); err == nil {
		t.Fatal("unknown decision outcome accepted")
	}
	anonymous := routingOutcome("five", "pending", true)
	anonymous.DecisionID = ""
	if _, err := domain.SummarizeManagerRouting(query, []domain.ManagerRoutingOutcome{anonymous}, time.Now().UTC()); err == nil {
		t.Fatal("attribution without decision identity accepted")
	}
	if _, err := domain.SummarizeManagerRouting(query, make([]domain.ManagerRoutingOutcome, domain.OutcomeAttributionLimit+1), time.Now().UTC()); err == nil {
		t.Fatal("oversized cohort accepted")
	}
	if _, err := domain.SummarizeManagerRouting(domain.OutcomeAttributionQuery{ProjectID: "project", From: query.To, To: query.From}, nil, time.Now().UTC()); err == nil {
		t.Fatal("inverted window accepted")
	}
}

func TestSummarizeOrchestratorPlanningCountsActionsOnce(t *testing.T) {
	query := attributionQuery()
	created := func(taskID string) domain.OrchestratorPlanningOutcome {
		return domain.OrchestratorPlanningOutcome{ReceiptID: "receipt-" + taskID, Action: "create_task", TaskID: taskID, TaskTitle: "Slice " + taskID, Revision: 1, State: "completed", Reason: "Verified", Attempts: 2, CreatedAt: time.Now().UTC()}
	}
	summary, err := domain.SummarizeOrchestratorPlanning(query, []domain.OrchestratorPlanningOutcome{
		created("one"),
		created("two"),
		{ReceiptID: "receipt-revise", Action: "revise_task", TaskID: "one", Revision: 2, State: "completed", CreatedAt: time.Now().UTC()},
		{ReceiptID: "receipt-freeze", Action: "freeze_criteria", TaskID: "one", Revision: 2, State: "completed", CreatedAt: time.Now().UTC()},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Totals.Receipts != 4 || summary.Totals.CreateTask != 2 || summary.Totals.ReviseTask != 1 || summary.Totals.FreezeCriteria != 1 {
		t.Fatalf("action totals: %+v", summary.Totals)
	}
	if summary.Totals.PlannedTaskStates.Completed != 2 || summary.Totals.PlannedTaskAttempts != 4 {
		t.Fatalf("created tasks must count once regardless of later receipts: %+v", summary.Totals)
	}
}

func TestSummarizeOrchestratorPlanningRefusesBrokenCohorts(t *testing.T) {
	query := attributionQuery()
	duplicate := domain.OrchestratorPlanningOutcome{ReceiptID: "receipt-one", Action: "create_task", TaskID: "one", State: "pending", CreatedAt: time.Now().UTC()}
	again := duplicate
	again.ReceiptID = "receipt-one-again"
	if _, err := domain.SummarizeOrchestratorPlanning(query, []domain.OrchestratorPlanningOutcome{duplicate, again}, time.Now().UTC()); err == nil {
		t.Fatal("duplicate created-task attribution accepted")
	}
	unknown := domain.OrchestratorPlanningOutcome{ReceiptID: "receipt-unknown", Action: "recreate_task", TaskID: "one", State: "pending", CreatedAt: time.Now().UTC()}
	if _, err := domain.SummarizeOrchestratorPlanning(query, []domain.OrchestratorPlanningOutcome{unknown}, time.Now().UTC()); err == nil {
		t.Fatal("unknown planning action accepted")
	}
	badState := domain.OrchestratorPlanningOutcome{ReceiptID: "receipt-state", Action: "create_task", TaskID: "two", State: "dreaming", CreatedAt: time.Now().UTC()}
	if _, err := domain.SummarizeOrchestratorPlanning(query, []domain.OrchestratorPlanningOutcome{badState}, time.Now().UTC()); err == nil {
		t.Fatal("unknown planned task state accepted")
	}
	anonymous := domain.OrchestratorPlanningOutcome{Action: "create_task", TaskID: "three", State: "pending", CreatedAt: time.Now().UTC()}
	if _, err := domain.SummarizeOrchestratorPlanning(query, []domain.OrchestratorPlanningOutcome{anonymous}, time.Now().UTC()); err == nil {
		t.Fatal("attribution without receipt identity accepted")
	}
	if _, err := domain.SummarizeOrchestratorPlanning(query, make([]domain.OrchestratorPlanningOutcome, domain.OutcomeAttributionLimit+1), time.Now().UTC()); err == nil {
		t.Fatal("oversized cohort accepted")
	}
	if _, err := domain.SummarizeOrchestratorPlanning(domain.OutcomeAttributionQuery{ProjectID: "", From: query.From, To: query.To}, nil, time.Now().UTC()); err == nil {
		t.Fatal("missing project accepted")
	}
}
