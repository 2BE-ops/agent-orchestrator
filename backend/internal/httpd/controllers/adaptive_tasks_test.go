package controllers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func adaptiveTaskRouter(t *testing.T) (http.Handler, *sqlite.Store, *tasksvc.Manager) {
	t.Helper()
	s := sqlitetest.MustOpen(t)
	for _, id := range []string{"project", "other"} {
		if err := s.UpsertProject(context.Background(), domain.ProjectRecord{ID: id, Path: "/repo/" + id, RegisteredAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	svc := tasksvc.New(s)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1", (&controllers.AdaptiveTasksController{Svc: svc}).Register)
	return r, s, svc
}

func adaptiveTaskInput() controllers.AdaptiveTaskCreateRequest {
	return controllers.AdaptiveTaskCreateRequest{Definition: domain.TaskDefinition{Title: "Add task history", Brief: "Retain exact acceptance criteria", MaxAttempts: 3}, Criteria: &domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "test", Requirement: "Regression tests pass", EvidenceKind: "test", Command: []string{"go", "test", "./..."}}}}, Reason: "Plan requested work"}
}

func createAdaptiveTaskHTTP(t *testing.T, router http.Handler) controllers.AdaptiveTaskResponse {
	t.Helper()
	w := registryRequest(t, router, http.MethodPost, "/projects/project/tasks", adaptiveTaskInput(), http.StatusCreated)
	var task controllers.AdaptiveTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	return task
}

type contextReadStore struct {
	tasksvc.Store
	snapshot domain.TaskContextSnapshot
	found    bool
	err      error
	reads    int
}

func (s *contextReadStore) GetTaskContext(context.Context, string) (domain.TaskContextSnapshot, bool, error) {
	s.reads++
	return s.snapshot, s.found, s.err
}

func TestAdaptiveTaskContextReadScopesAttemptAndPreservesErrors(t *testing.T) {
	r, s, _ := adaptiveTaskRouter(t)
	task := createAdaptiveTaskHTTP(t, r)
	ctx := context.Background()
	_, _, err := s.ReserveTask(ctx, domain.TaskReservation{ID: "attempt", TaskID: task.Task.ID, LaunchIntentID: "launch", HolderID: "scheduler", Mutation: domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Prepare task", ExpectedRevision: 1}, Now: time.Now().UTC(), TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	read := &contextReadStore{Store: s, found: true, snapshot: domain.TaskContextSnapshot{AttemptID: "attempt", SessionID: "worker", Prompt: "Exact retained context", ContentHash: strings.Repeat("a", 64)}}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Route("/api/v1", (&controllers.AdaptiveTasksController{Svc: tasksvc.New(read)}).Register)
	path := "/tasks/" + task.Task.ID + "/attempts/attempt/context"
	w := registryRequest(t, router, http.MethodGet, path, nil, http.StatusOK)
	var snapshot domain.TaskContextSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil || snapshot.ContentHash != read.snapshot.ContentHash || snapshot.Prompt != read.snapshot.Prompt {
		t.Fatalf("context changed across HTTP: %+v %v", snapshot, err)
	}
	registryRequest(t, router, http.MethodGet, "/tasks/other/attempts/attempt/context", nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, "/tasks/"+task.Task.ID+"/attempts/missing/context", nil, http.StatusNotFound)
	if read.reads != 1 {
		t.Fatal("unrelated task could read attempt context")
	}
	read.found = false
	w = registryRequest(t, router, http.MethodGet, path, nil, http.StatusNotFound)
	var missing envelope.APIError
	if err := json.Unmarshal(w.Body.Bytes(), &missing); err != nil || missing.Code != "TASK_CONTEXT_NOT_FOUND" || missing.RequestID == "" {
		t.Fatalf("missing context envelope: %s %v", w.Body.String(), err)
	}
	read.err = errors.New("context database unavailable")
	registryRequest(t, router, http.MethodGet, path, nil, http.StatusInternalServerError)
}

func TestAdaptiveTasksAuthoringCriteriaAndHistory(t *testing.T) {
	r, _, _ := adaptiveTaskRouter(t)
	task := createAdaptiveTaskHTTP(t, r)
	if task.Task.ID == "" || task.Task.CreatedBy.Kind != "USER" || task.Task.CreatedBy.ID != "local-user" || task.Revision.CriteriaVersion != 1 {
		t.Fatalf("created task: %+v", task)
	}
	path := "/tasks/" + task.Task.ID
	input := adaptiveTaskInput()
	input.Definition.Title = "Refine future work"
	registryRequest(t, r, http.MethodPost, path+"/revisions", controllers.AdaptiveTaskReviseRequest{Definition: input.Definition, ExpectedRevision: 1, Reason: "Refine intent"}, http.StatusCreated)
	criteria := *input.Criteria
	criteria.Criteria[0].Requirement = "Tests and added regression pass"
	change := controllers.AdaptiveTaskCriteriaRequest{Criteria: criteria, ExpectedRevision: 2, Reason: "Add regression requirement"}
	registryRequest(t, r, http.MethodPost, path+"/criteria", change, http.StatusCreated)
	registryRequest(t, r, http.MethodPost, path+"/criteria", change, http.StatusConflict)
	w := registryRequest(t, r, http.MethodGet, path, nil, http.StatusOK)
	var view controllers.AdaptiveTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Task.Revision != 3 || view.Revision.Definition.Title != input.Definition.Title || view.Criteria.Number != 2 {
		t.Fatalf("current view: %+v", view)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/criteria/1", nil, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Regression tests pass") {
		t.Fatalf("old criteria changed: %s", w.Body.String())
	}
	w = registryRequest(t, r, http.MethodGet, path+"/revisions/1", nil, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Add task history") {
		t.Fatalf("old planning changed: %s", w.Body.String())
	}
	w = registryRequest(t, r, http.MethodGet, path+"/revisions?cursor=1&limit=1", nil, http.StatusOK)
	var revisions controllers.AdaptiveTaskRevisionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &revisions); err != nil {
		t.Fatal(err)
	}
	if len(revisions.Items) != 1 || revisions.Items[0].Number != 2 || revisions.NextCursor != "2" {
		t.Fatalf("revision page: %+v", revisions)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/audit?limit=1", nil, http.StatusOK)
	var audit controllers.AdaptiveTaskAuditResponse
	if err := json.Unmarshal(w.Body.Bytes(), &audit); err != nil {
		t.Fatal(err)
	}
	if len(audit.Items) != 1 || audit.Items[0].Action != "created" || audit.NextCursor == "" {
		t.Fatalf("audit page: %+v", audit)
	}
	registryRequest(t, r, http.MethodGet, path+"/audit?cursor="+audit.NextCursor, nil, http.StatusOK)
}

func TestAdaptiveTaskIntentsAreFencedAuditedAndServerAttributed(t *testing.T) {
	r, _, _ := adaptiveTaskRouter(t)
	task := createAdaptiveTaskHTTP(t, r)
	path := "/tasks/" + task.Task.ID
	change := controllers.AdaptiveTaskIntentRequest{Intent: "cancel", ExpectedRevision: 1, Reason: "Cancel future work"}
	w := registryRequest(t, r, http.MethodPost, path+"/intents", change, http.StatusOK)
	var intent domain.TaskIntent
	if err := json.Unmarshal(w.Body.Bytes(), &intent); err != nil || intent.Version != 1 || intent.Actor.ID != "local-user" {
		t.Fatalf("intent attribution: %+v %v", intent, err)
	}
	registryRequest(t, r, http.MethodPost, path+"/intents", change, http.StatusConflict)
	w = registryRequest(t, r, http.MethodGet, path, nil, http.StatusOK)
	var view controllers.AdaptiveTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.State.Phase != "cancelled" {
		t.Fatalf("derived cancellation: %+v %v", view.State, err)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/intents?limit=1", nil, http.StatusOK)
	var history controllers.AdaptiveTaskIntentsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil || len(history.Items) != 1 || history.NextCursor != "1" {
		t.Fatalf("intent history: %+v %v", history, err)
	}
	registryRequest(t, r, http.MethodPost, path+"/intents", map[string]any{"intent": "run", "actor": map[string]string{"kind": "USER"}}, http.StatusBadRequest)
	change.Intent, change.ExpectedVersion = "completed", 1
	registryRequest(t, r, http.MethodPost, path+"/intents", change, http.StatusBadRequest)
	registryRequest(t, r, http.MethodGet, "/tasks/missing/intents", nil, http.StatusNotFound)
	registryRequest(t, r, http.MethodGet, path+"/intents?cursor=-1", nil, http.StatusBadRequest)
}

func TestAdaptiveTasksProjectPagesAndMissingResources(t *testing.T) {
	r, _, _ := adaptiveTaskRouter(t)
	createAdaptiveTaskHTTP(t, r)
	createAdaptiveTaskHTTP(t, r)
	w := registryRequest(t, r, http.MethodGet, "/projects/project/tasks?limit=1", nil, http.StatusOK)
	var first controllers.AdaptiveTaskListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first page: %+v", first)
	}
	w = registryRequest(t, r, http.MethodGet, "/projects/project/tasks?cursor="+first.NextCursor, nil, http.StatusOK)
	var next controllers.AdaptiveTaskListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].Task.ID == first.Items[0].Task.ID {
		t.Fatalf("second page: %+v", next)
	}
	w = registryRequest(t, r, http.MethodGet, "/projects/other/tasks", nil, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != `{"items":[]}` {
		t.Fatalf("project scope: %s", w.Body.String())
	}
	for _, path := range []string{"/projects/missing/tasks", "/tasks/missing", "/tasks/missing/revisions", "/tasks/missing/audit", "/tasks/missing/attempts", "/tasks/missing/revisions/1", "/tasks/missing/criteria/1"} {
		registryRequest(t, r, http.MethodGet, path, nil, http.StatusNotFound)
	}
}

func TestAdaptiveTasksRejectMalformedAndSpoofedAuthoring(t *testing.T) {
	r, _, _ := adaptiveTaskRouter(t)
	for _, body := range []string{`{`, `null`, `{}`, `{"actor":{"kind":"SYSTEM"}}`, `{"definition":{"title":"work","brief":"work","maxAttempts":2,"hidden":true},"reason":"plan"}`, `{} {}`, strings.Repeat("x", (256<<10)+1)} {
		w := registryRequest(t, r, http.MethodPost, "/projects/project/tasks", body, http.StatusBadRequest)
		var problem envelope.APIError
		if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(w.Body.String(), "registry-test-request") {
			t.Fatalf("lost request ID: %s", w.Body.String())
		}
	}
	task := createAdaptiveTaskHTTP(t, r)
	for _, path := range []string{"/projects/project/tasks?limit=0", "/projects/project/tasks?limit=101", "/tasks/" + task.Task.ID + "/audit?cursor=-1", "/tasks/" + task.Task.ID + "/attempts?cursor=wrong", "/tasks/" + task.Task.ID + "/revisions/0", "/tasks/" + task.Task.ID + "/criteria/wrong"} {
		registryRequest(t, r, http.MethodGet, path, nil, http.StatusBadRequest)
	}
	input := adaptiveTaskInput()
	input.Definition.Dependencies = []string{task.Task.ID}
	registryRequest(t, r, http.MethodPost, "/projects/other/tasks", input, http.StatusBadRequest)
}

func TestAdaptiveTasksExposeFrozenAttemptsWithoutHolderToken(t *testing.T) {
	r, s, _ := adaptiveTaskRouter(t)
	task := createAdaptiveTaskHTTP(t, r)
	_, _, err := s.ReserveTask(context.Background(), domain.TaskReservation{ID: "attempt", TaskID: task.Task.ID, LaunchIntentID: "intent", HolderID: "private-scheduler-incarnation", Mutation: domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "scheduler"}, Reason: "Dispatch", ExpectedRevision: 1}, Now: time.Now().UTC(), TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/tasks/" + task.Task.ID, "/tasks/" + task.Task.ID + "/attempts"} {
		w := registryRequest(t, r, http.MethodGet, path, nil, http.StatusOK)
		if strings.Contains(w.Body.String(), "private-scheduler-incarnation") || !strings.Contains(w.Body.String(), `"criteriaVersion":1`) {
			t.Fatalf("unsafe/missing execution facts: %s", w.Body.String())
		}
	}
}

func TestAdaptiveTasksTrustedToolsDenyWorkerCriteriaChanges(t *testing.T) {
	r, _, svc := adaptiveTaskRouter(t)
	task := createAdaptiveTaskHTTP(t, r)
	for _, kind := range []string{"WORKER", "AGENT_MANAGER"} {
		_, err := svc.ReviseCriteria(context.Background(), domain.AdaptiveActor{Kind: kind, ID: "controller"}, task.Task.ID, tasksvc.CriteriaInput{Criteria: *adaptiveTaskInput().Criteria, ExpectedRevision: 1, Reason: "Make my result pass"})
		if err == nil {
			t.Fatalf("%s changed success criteria", kind)
		}
	}
	view, err := svc.Get(context.Background(), task.Task.ID)
	if err != nil || view.Task.Revision != 1 {
		t.Fatalf("criteria changed: %+v %v", view, err)
	}
}

func TestAdaptiveTasksUnavailableService(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1", (&controllers.AdaptiveTasksController{}).Register)
	registryRequest(t, r, http.MethodGet, "/projects/project/tasks", nil, http.StatusNotImplemented)
}

func TestAdaptiveTasksMountedWithProjectRoutes(t *testing.T) {
	_, _, svc := adaptiveTaskRouter(t)
	r := chi.NewRouter()
	httpd.NewAPI(config.Config{}, httpd.APIDeps{AdaptiveTasks: svc}).Register(r)
	created := createAdaptiveTaskHTTP(t, r)
	registryRequest(t, r, http.MethodGet, "/projects/project/tasks", nil, http.StatusOK)
	registryRequest(t, r, http.MethodGet, "/tasks/"+created.Task.ID, nil, http.StatusOK)
}
