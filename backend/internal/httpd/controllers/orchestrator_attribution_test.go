package controllers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

func TestOrchestratorPlanningAttributionAPI(t *testing.T) {
	s, router := orchestratorGoalAPIFixture(t)
	if _, err := s.CreateSession(t.Context(), domain.SessionRecord{ProjectID: "project", Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: time.Now().UTC()}, Metadata: domain.SessionMetadata{RuntimeLaunchID: "gen-1"}, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	input := controllers.OrchestratorPlanRequest{SourceGeneration: "gen-1", IdempotencyKey: "plan-1", Action: domain.OrchestratorPlanAction{Action: "create_task", Definition: &domain.TaskDefinition{Title: "Attributed slice", Brief: "Implement the slice", MaxAttempts: 3, Dependencies: []string{}, RequiredCapabilities: []string{}}, Reason: "Decompose"}}
	w := registryRequest(t, router, http.MethodPost, "/projects/project/orchestrator/plan", input, http.StatusOK)
	var planned controllers.OrchestratorPlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &planned); err != nil || !planned.Created {
		t.Fatalf("plan: %s (%v)", w.Body.String(), err)
	}
	w = registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/planning-outcomes?limit=20", nil, http.StatusOK)
	var outcomes controllers.OrchestratorPlanningOutcomesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &outcomes); err != nil || len(outcomes.Items) != 1 {
		t.Fatalf("planning outcomes: %s (%v)", w.Body.String(), err)
	}
	if item := outcomes.Items[0]; item.ReceiptID != planned.Receipt.Outcome.ReceiptID || item.TaskID != planned.Receipt.Outcome.TaskID || item.State != "pending" || item.Action != "create_task" {
		t.Fatalf("planning outcome row: %+v", item)
	}
	from := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	to := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	w = registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/planning-summary?from="+from+"&to="+to, nil, http.StatusOK)
	var summary controllers.OrchestratorPlanningSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil || summary.Summary.Totals.Receipts != 1 || summary.Summary.Totals.CreateTask != 1 || summary.Summary.Totals.PlannedTaskStates.Pending != 1 {
		t.Fatalf("planning summary: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/planning-outcomes?limit=0", nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/planning-outcomes?afterId=r&limit=1&extra=1", nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/planning-summary?from="+to+"&to="+from, nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/planning-summary?from="+from, nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, "/projects/other/orchestrator/planning-summary?from="+from+"&to="+to, nil, http.StatusNotFound)
}
