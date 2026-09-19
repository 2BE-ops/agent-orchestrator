package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	controlsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/control"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func controlAPIFixture(t *testing.T) (*sqlite.Store, http.Handler) {
	t.Helper()
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "manual", Requirement: "Human checks", EvidenceKind: "manual"}}}
	if _, err := s.CreateAdaptiveTask(ctx, "task-1", "project", domain.TaskDefinition{Title: "Bounded work", Brief: "Verify controls", MaxAttempts: 2}, &criteria, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Plan"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdaptiveTask(ctx, "task-2", "project", domain.TaskDefinition{Title: "Free work", Brief: "Unleased", MaxAttempts: 2}, nil, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Plan"}); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1", (&controllers.ControlController{Svc: controlsvc.New(s)}).Register)
	return s, r
}

func TestControlAPIPauseDrainResumeAndCancel(t *testing.T) {
	_, router := controlAPIFixture(t)
	w := registryRequest(t, router, http.MethodGet, "/projects/project/control", nil, http.StatusOK)
	var view controllers.ProjectControlResponse
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.View.Control.State != domain.ProjectRunning || view.View.EffectiveState != domain.ProjectRunning {
		t.Fatalf("default control: %s (%v)", w.Body.String(), err)
	}
	w = registryRequest(t, router, http.MethodPost, "/projects/project/control", controlsvc.StateInput{State: "paused", Reason: "Evening maintenance"}, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.View.Control.State != domain.ProjectPaused || view.View.Control.Reason != "Evening maintenance" {
		t.Fatalf("pause: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodPost, "/projects/project/control", controlsvc.StateInput{State: "draining", Reason: "Skip pause"}, http.StatusConflict)
	registryRequest(t, router, http.MethodPost, "/projects/project/control", controlsvc.StateInput{State: "hibernating", Reason: "Bad state"}, http.StatusBadRequest)
	registryRequest(t, router, http.MethodPost, "/projects/ghost/control", controlsvc.StateInput{State: "paused", Reason: "No project"}, http.StatusNotFound)

	w = registryRequest(t, router, http.MethodPost, "/projects/project/control/cancel-work", controlsvc.CancelInput{Scope: "pending", Reason: "Wrong direction"}, http.StatusOK)
	var cancelled controllers.ProjectCancelWorkResponse
	if err := json.Unmarshal(w.Body.Bytes(), &cancelled); err != nil || len(cancelled.Cancel.Result.Cancelled) != 2 || cancelled.Cancel.KillService {
		t.Fatalf("cancel pending: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodPost, "/projects/project/control/cancel-work", controlsvc.CancelInput{Scope: "some", Reason: "Bad scope"}, http.StatusBadRequest)

	w = registryRequest(t, router, http.MethodPost, "/projects/project/control", controlsvc.StateInput{State: "running", Reason: "Resumed"}, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.View.Control.State != domain.ProjectRunning {
		t.Fatalf("resume: %s (%v)", w.Body.String(), err)
	}
}

func TestControlAPINeedsHumanLifecycle(t *testing.T) {
	_, router := controlAPIFixture(t)
	w := registryRequest(t, router, http.MethodPost, "/tasks/task-1/needs-human", controlsvc.NeedsHumanInput{ReasonCode: "credential_missing", Detail: "The provider credential expired"}, http.StatusCreated)
	var raised controllers.TaskNeedsHumanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &raised); err != nil || raised.NeedsHuman.TaskID != "task-1" || raised.NeedsHuman.Resolution != nil {
		t.Fatalf("raise: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodPost, "/tasks/task-1/needs-human", controlsvc.NeedsHumanInput{ReasonCode: "approval_required", Detail: "Second"}, http.StatusConflict)
	registryRequest(t, router, http.MethodPost, "/tasks/ghost/needs-human", controlsvc.NeedsHumanInput{ReasonCode: "credential_missing", Detail: "No task"}, http.StatusNotFound)
	registryRequest(t, router, http.MethodPost, "/tasks/task-1/needs-human", controlsvc.NeedsHumanInput{ReasonCode: "vibes", Detail: "Bad code"}, http.StatusBadRequest)

	w = registryRequest(t, router, http.MethodGet, "/projects/project/needs-human?limit=20", nil, http.StatusOK)
	var listed controllers.ProjectNeedsHumanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed.Items) != 1 || listed.Items[0].TaskID != "task-1" {
		t.Fatalf("list: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodGet, "/projects/project/needs-human?limit=0", nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, "/projects/ghost/needs-human?limit=20", nil, http.StatusOK)

	w = registryRequest(t, router, http.MethodPost, "/tasks/task-1/needs-human/resolve", controlsvc.ResolveInput{Resolution: "Rotated the credential"}, http.StatusOK)
	var resolved controllers.TaskNeedsHumanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resolved); err != nil || resolved.NeedsHuman.Resolution == nil || resolved.NeedsHuman.Resolution.Resolution != "Rotated the credential" {
		t.Fatalf("resolve: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, router, http.MethodPost, "/tasks/task-1/needs-human/resolve", controlsvc.ResolveInput{Resolution: "Again"}, http.StatusNotFound)
	registryRequest(t, router, http.MethodPost, "/tasks/task-1/needs-human/resolve", controlsvc.ResolveInput{}, http.StatusBadRequest)
}

func TestControlAPIDryRunIsReadOnly(t *testing.T) {
	s, router := controlAPIFixture(t)
	request := domain.DryRunRequest{Actions: []domain.OrchestratorPlanAction{
		{Action: "create_task", Definition: &domain.TaskDefinition{Title: "New work", Brief: "Rehearsed", Category: "chore", MaxAttempts: 2}, Reason: "Add work"},
		{Action: "revise_task", TaskID: "task-1", ExpectedRevision: 1, Definition: &domain.TaskDefinition{Title: "Narrower", Brief: "Rehearsed", Category: "chore", MaxAttempts: 2}, Reason: "Narrow"},
	}}
	before, err := s.ListAdaptiveTasks(context.Background(), "project", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	w := registryRequest(t, router, http.MethodPost, "/projects/project/dry-run", request, http.StatusOK)
	var verdict controllers.DryRunResponse
	if err := json.Unmarshal(w.Body.Bytes(), &verdict); err != nil || !verdict.Verdict.GraphValid || len(verdict.Verdict.Actions) != 2 || verdict.Verdict.CostEstimate != domain.DryRunCostUnknown {
		t.Fatalf("dry run: %s (%v)", w.Body.String(), err)
	}
	after, err := s.ListAdaptiveTasks(context.Background(), "project", "", 100)
	if err != nil || len(after) != len(before) {
		t.Fatalf("dry run mutated tasks through the API: %d -> %d %v", len(before), len(after), err)
	}
	registryRequest(t, router, http.MethodPost, "/projects/project/dry-run", domain.DryRunRequest{}, http.StatusBadRequest)
	registryRequest(t, router, http.MethodPost, "/projects/ghost/dry-run", request, http.StatusNotFound)
}
