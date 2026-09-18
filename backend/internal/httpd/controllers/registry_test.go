package controllers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func registryRouter(t *testing.T) (http.Handler, *registrysvc.Manager) {
	t.Helper()
	svc := registrysvc.New(sqlitetest.MustOpen(t))
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1", (&controllers.RegistryController{Svc: svc}).Register)
	return r, svc
}

func registryRequest(t *testing.T, router http.Handler, method, path string, body any, status int) *httptest.ResponseRecorder {
	t.Helper()
	var content []byte
	var err error
	if raw, ok := body.(string); ok {
		content = []byte(raw)
	} else if body != nil {
		content, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(content))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-Id", "registry-test-request")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	return w
}

func registryCreateInput() registrysvc.CreateInput {
	return registrysvc.CreateInput{Metadata: domain.RegistryMetadata{Name: "Coder", Enabled: true, Policy: domain.RegistryPolicy{ManagerCanSelect: true}},
		Definition: domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, MaxParallelWorkers: 2}}, Reason: "User creates coder"}
}

func TestRegistryHTTPAuthoringLifecycle(t *testing.T) {
	r, _ := registryRouter(t)
	response := registryRequest(t, r, http.MethodPost, "/agent-types", registryCreateInput(), http.StatusCreated)
	var created controllers.RegistryViewResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Entry.Origin != "USER" || created.Entry.ActiveVersion != 1 || created.Version.Definition.AgentType.Skills == nil {
		t.Fatalf("creation or wire arrays: %+v", created)
	}
	path := "/agent-types/" + created.Entry.ID
	registryRequest(t, r, http.MethodGet, path, nil, http.StatusOK)
	registryRequest(t, r, http.MethodGet, "/skills/"+created.Entry.ID, nil, http.StatusNotFound)
	definition := created.Version.Definition
	definition.AgentType.Instructions = "New immutable instructions"
	registryRequest(t, r, http.MethodPost, path+"/versions", registrysvc.VersionInput{Definition: definition, ExpectedRevision: 1, Reason: "Improve tests"}, http.StatusCreated)
	conflict := registryRequest(t, r, http.MethodPost, path+"/versions", registrysvc.VersionInput{Definition: definition, ExpectedRevision: 1, Reason: "Stale edit"}, http.StatusConflict)
	if !strings.Contains(conflict.Body.String(), "REGISTRY_REVISION_CONFLICT") || !strings.Contains(conflict.Body.String(), "registry-test-request") {
		t.Fatalf("lost error envelope: %s", conflict.Body.String())
	}
	registryRequest(t, r, http.MethodPost, path+"/activate", registrysvc.ActivateInput{Version: 2, ExpectedRevision: 2, Reason: "Activate"}, http.StatusOK)
	registryRequest(t, r, http.MethodPost, path+"/activate", registrysvc.ActivateInput{Version: 1, ExpectedRevision: 3, Reason: "Rollback"}, http.StatusOK)
	registryRequest(t, r, http.MethodGet, path+"/versions/1", nil, http.StatusOK)
	registryRequest(t, r, http.MethodGet, path+"/versions/2", nil, http.StatusOK)
	registryRequest(t, r, http.MethodGet, path+"/versions?limit=1", nil, http.StatusOK)
	registryRequest(t, r, http.MethodGet, path+"/audit", nil, http.StatusOK)
	registryRequest(t, r, http.MethodPost, path+"/clone", registrysvc.CloneInput{Version: 2, Name: "Cloned", Reason: "Specialize"}, http.StatusCreated)
	metadata := created.Entry.Metadata
	metadata.Enabled = false
	registryRequest(t, r, http.MethodPatch, path, registrysvc.MetadataInput{Metadata: metadata, ExpectedRevision: 4, Reason: "Disable"}, http.StatusOK)
	registryRequest(t, r, http.MethodGet, "/agent-types?limit=1", nil, http.StatusOK)
}

func TestRegistryHTTPRejectsSpoofedActorsAndInvalidBodies(t *testing.T) {
	r, _ := registryRouter(t)
	for _, body := range []string{`null`, `[]`, `{}`, `{} {}`, `{"origin":"AGENT_MANAGER"}`, `{"metadata":{"name":"x","origin":"USER"}}`, `{"definition":{"agentType":{"credentials":"secret"}}}`, strings.Repeat(" ", 2<<20) + `{}`} {
		registryRequest(t, r, http.MethodPost, "/agent-types", body, http.StatusBadRequest)
	}
	for _, path := range []string{"/agent-types?limit=0", "/skills?limit=201", "/agent-types?limit=bad", "/agent-types/x/versions?cursor=-1", "/skills/x/audit?cursor=no", "/agent-types/x/versions/0"} {
		registryRequest(t, r, http.MethodGet, path, nil, http.StatusBadRequest)
	}
}

func TestRegistryManagerAndUserShareVisibleDefinitions(t *testing.T) {
	r, svc := registryRouter(t)
	manager := domain.RegistryActor{Origin: domain.RegistryManager, ID: "manager-session"}
	created, err := svc.Create(context.Background(), manager, domain.RegistryAgentType, registryCreateInput())
	if err != nil {
		t.Fatal(err)
	}
	response := registryRequest(t, r, http.MethodGet, "/agent-types/"+created.Entry.ID, nil, http.StatusOK)
	if !strings.Contains(response.Body.String(), "AGENT_MANAGER") || !strings.Contains(response.Body.String(), "manager-session") {
		t.Fatal("manager creation not visible through human registry")
	}
	_, err = svc.Append(context.Background(), manager, domain.RegistryAgentType, created.Entry.ID,
		registrysvc.VersionInput{Definition: created.Version.Definition, ExpectedRevision: 1, Reason: "Not authorized"})
	if err == nil {
		t.Fatal("manager bypassed version ownership via service")
	}
}

func TestRegistryHTTPUnavailableService(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1", (&controllers.RegistryController{}).Register)
	registryRequest(t, r, http.MethodGet, "/agent-types", nil, http.StatusNotImplemented)
}
