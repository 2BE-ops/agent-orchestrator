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

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	knowledgesvc "github.com/aoagents/agent-orchestrator/backend/internal/service/knowledge"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func knowledgeRouter(t *testing.T) (http.Handler, *knowledgesvc.Manager) {
	t.Helper()
	s := sqlitetest.MustOpen(t)
	for _, id := range []string{"project", "other"} {
		if err := s.UpsertProject(context.Background(), domain.ProjectRecord{ID: id, Path: "/repo/" + id, RegisteredAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	svc := knowledgesvc.New(s)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1", (&controllers.ProjectKnowledgeController{Svc: svc}).Register)
	return r, svc
}

func knowledgeInput() controllers.KnowledgeCreateRequest {
	return controllers.KnowledgeCreateRequest{Definition: domain.KnowledgeDefinition{Title: "Service boundary", Kind: "architecture", Content: "Daemon services own durable state", Status: "candidate", Confidence: "medium", Sources: []domain.KnowledgeSource{{Kind: "user", Reference: "Reviewed architecture"}}}, Reason: "Record a reviewable claim"}
}

func createKnowledgeHTTP(t *testing.T, r http.Handler) controllers.KnowledgeResponse {
	t.Helper()
	w := registryRequest(t, r, http.MethodPost, "/projects/project/knowledge", knowledgeInput(), http.StatusCreated)
	var view controllers.KnowledgeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func TestProjectKnowledgeAPIAuthoringReviewSearchAndRetainedHistory(t *testing.T) {
	r, _ := knowledgeRouter(t)
	view := createKnowledgeHTTP(t, r)
	if view.Version.Actor.Kind != "USER" || view.Version.Actor.ID != "local-user" || view.Version.Number != 1 {
		t.Fatalf("authority: %+v", view)
	}
	path := "/knowledge/" + view.Knowledge.ID
	d := view.Version.Definition
	d.Status, d.Pinned = "accepted", true
	change := controllers.KnowledgeReviseRequest{Definition: d, ExpectedVersion: 1, Reason: "Reviewed against repository evidence"}
	registryRequest(t, r, http.MethodPost, path+"/versions", change, http.StatusCreated)
	registryRequest(t, r, http.MethodPost, path+"/versions", change, http.StatusConflict)
	w := registryRequest(t, r, http.MethodGet, "/projects/project/knowledge?status=accepted&kind=architecture&search=DAEMON", nil, http.StatusOK)
	var list controllers.KnowledgeListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 || list.Items[0].Version.Number != 2 {
		t.Fatalf("search: %+v %v", list, err)
	}
	d.Status, d.Pinned = "deleted", false
	registryRequest(t, r, http.MethodPost, path+"/versions", controllers.KnowledgeReviseRequest{Definition: d, ExpectedVersion: 2, Reason: "Withdraw future context"}, http.StatusCreated)
	w = registryRequest(t, r, http.MethodGet, "/projects/project/knowledge", nil, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 0 {
		t.Fatalf("default selection retained deletion: %+v %v", list, err)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/versions/2", nil, http.StatusOK)
	var version domain.KnowledgeVersion
	if err := json.Unmarshal(w.Body.Bytes(), &version); err != nil || version.Definition.Status != "accepted" || !version.Definition.Pinned {
		t.Fatalf("past evidence changed: %+v %v", version, err)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/versions?cursor=1&limit=1", nil, http.StatusOK)
	var versions controllers.KnowledgeVersionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &versions); err != nil || len(versions.Items) != 1 || versions.NextCursor != "2" {
		t.Fatalf("history page: %+v %v", versions, err)
	}
	registryRequest(t, r, http.MethodGet, path, nil, http.StatusOK)
	w = registryRequest(t, r, http.MethodGet, "/projects/other/knowledge", nil, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 0 {
		t.Fatalf("cross-project list: %+v %v", list, err)
	}
}

func TestProjectKnowledgeAPIRejectsForgedAuthorityMalformedPayloadAndInvalidQueries(t *testing.T) {
	r, _ := knowledgeRouter(t)
	for _, body := range []string{`{"actor":{"kind":"SYSTEM"}}`, `{} {}`, `[]`, `{"definition":{"content":"` + strings.Repeat("x", 129<<10) + `"}}`} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/projects/project/knowledge", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("malformed body accepted: %d", w.Code)
		}
	}
	for _, path := range []string{"/projects/project/knowledge?status=trusted", "/projects/project/knowledge?kind=unknown", "/projects/project/knowledge?limit=101", "/knowledge/fact/versions?cursor=-1", "/knowledge/fact/versions/0"} {
		registryRequest(t, r, http.MethodGet, path, nil, http.StatusBadRequest)
	}
	for _, path := range []string{"/projects/missing/knowledge", "/knowledge/missing", "/knowledge/missing/versions", "/knowledge/missing/versions/1"} {
		registryRequest(t, r, http.MethodGet, path, nil, http.StatusNotFound)
	}
}

func TestProjectKnowledgeAPIMountedAndUnavailable(t *testing.T) {
	_, svc := knowledgeRouter(t)
	r := chi.NewRouter()
	httpd.NewAPI(config.Config{}, httpd.APIDeps{ProjectKnowledge: svc}).Register(r)
	view := createKnowledgeHTTP(t, r)
	registryRequest(t, r, http.MethodGet, "/knowledge/"+view.Knowledge.ID, nil, http.StatusOK)
	empty := chi.NewRouter()
	empty.Route("/api/v1", (&controllers.ProjectKnowledgeController{}).Register)
	registryRequest(t, empty, http.MethodGet, "/projects/project/knowledge", nil, http.StatusNotImplemented)
}
