package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	orchestratorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/orchestrator"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func jsonContains(w *httptest.ResponseRecorder, substr string) bool {
	return strings.Contains(w.Body.String(), substr)
}

func orchestratorGoalAPIFixture(t *testing.T) (*sqlite.Store, http.Handler) {
	t.Helper()
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1", (&controllers.OrchestratorController{Svc: orchestratorsvc.New(s)}).Register)
	return s, r
}

func TestOrchestratorGoalAPIFullLoop(t *testing.T) {
	s, router := orchestratorGoalAPIFixture(t)
	ctx := context.Background()

	w := registryRequest(t, router, http.MethodGet, "/projects/project/goal", nil, http.StatusNotFound)
	if !jsonContains(w, "GOAL_NOT_FOUND") {
		t.Fatalf("missing goal envelope: %s", w.Body.String())
	}
	w = registryRequest(t, router, http.MethodPost, "/projects/project/goal", controllers.SetProjectGoalRequest{Goal: "Ship the verified loop", Reason: "Kick off"}, http.StatusCreated)
	var goal controllers.ProjectGoalResponse
	if err := json.Unmarshal(w.Body.Bytes(), &goal); err != nil || goal.Goal.Number != 1 || goal.Goal.ContentHash == "" {
		t.Fatalf("set goal: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodPost, "/projects/project/goal", controllers.SetProjectGoalRequest{Goal: "", Reason: "Empty"}, http.StatusBadRequest)

	registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/goal", nil, http.StatusNotFound)
	rec, err := s.CreateSession(ctx, domain.SessionRecord{ProjectID: "project", Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: time.Now().UTC()}, Metadata: domain.SessionMetadata{RuntimeLaunchID: "gen-1"}, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	w = registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/goal", nil, http.StatusOK)
	var native controllers.NativeOrchestratorGoalResponse
	if err := json.Unmarshal(w.Body.Bytes(), &native); err != nil || native.SourceGeneration != "gen-1" || native.SessionID != rec.ID || native.Goal.Number != 1 {
		t.Fatalf("native goal: %s (%v)", w.Body.String(), err)
	}

	input := controllers.OrchestratorPlanRequest{SourceGeneration: "gen-1", IdempotencyKey: "plan-1", Action: domain.OrchestratorPlanAction{Action: "create_task", Definition: &domain.TaskDefinition{Title: "First slice", Brief: "Implement the slice", Priority: 5, MaxAttempts: 3, Dependencies: []string{}, RequiredCapabilities: []string{}}, Reason: "Decompose"}}
	w = registryRequest(t, router, http.MethodPost, "/projects/project/orchestrator/plan", input, http.StatusOK)
	var planned controllers.OrchestratorPlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &planned); err != nil || !planned.Created || planned.Receipt.Outcome.TaskID == "" || planned.Receipt.Outcome.Revision != 1 {
		t.Fatalf("plan: %s (%v)", w.Body.String(), err)
	}
	taskID := planned.Receipt.Outcome.TaskID
	stale := input
	stale.SourceGeneration = "gen-2"
	registryRequest(t, router, http.MethodPost, "/projects/project/orchestrator/plan", stale, http.StatusConflict)
	w = registryRequest(t, router, http.MethodPost, "/projects/project/orchestrator/plan", input, http.StatusOK)
	var replay controllers.OrchestratorPlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil || replay.Created || replay.Receipt.Outcome.ReceiptID != planned.Receipt.Outcome.ReceiptID {
		t.Fatalf("plan replay: %s (%v)", w.Body.String(), err)
	}

	complete := controllers.OrchestratorCompleteRequest{SourceGeneration: "gen-1", GoalVersion: 1, Summary: "Delivered", Reason: "Terminal"}
	w = registryRequest(t, router, http.MethodPost, "/projects/project/orchestrator/complete", complete, http.StatusConflict)
	if !jsonContains(w, "GOAL_INCOMPLETE") || !jsonContains(w, "blockers") {
		t.Fatalf("typed blocker envelope lost: %s", w.Body.String())
	}

	w = registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/feedback?limit=20", nil, http.StatusOK)
	var feedback controllers.ProjectFeedbackResponse
	if err := json.Unmarshal(w.Body.Bytes(), &feedback); err != nil || len(feedback.Items) != 1 || feedback.Items[0].TaskID != taskID || feedback.Items[0].State != "pending" || feedback.Items[0].Title != "First slice" {
		t.Fatalf("feedback: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/feedback?limit=0", nil, http.StatusBadRequest)

	w = registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/receipts?limit=20", nil, http.StatusOK)
	var receipts controllers.OrchestratorPlanReceiptsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &receipts); err != nil || len(receipts.Items) != 1 {
		t.Fatalf("receipts: %s (%v)", w.Body.String(), err)
	}
	receiptID := receipts.Items[0].Outcome.ReceiptID
	w = registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/receipts/"+receiptID, nil, http.StatusOK)
	var one controllers.OrchestratorPlanReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &one); err != nil || one.Receipt.Outcome.TaskID != taskID {
		t.Fatalf("receipt read: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodGet, "/projects/project/orchestrator/receipts/missing", nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, "/projects/missing/goal", nil, http.StatusNotFound)

	w = registryRequest(t, router, http.MethodGet, "/projects/project/goal/versions?after=0&limit=20", nil, http.StatusOK)
	var versions controllers.ProjectGoalVersionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &versions); err != nil || len(versions.Items) != 1 {
		t.Fatalf("goal versions: %s (%v)", w.Body.String(), err)
	}
}
