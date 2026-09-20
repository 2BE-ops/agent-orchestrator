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
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	managersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agentmanager"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type apiManagerRuntime struct {
	store *sqlite.Store
	calls int
}

func (n *apiManagerRuntime) Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	n.calls++
	version, err := n.store.GetRegistryVersion(ctx, cfg.WorkerSelection.AgentTypeID, cfg.WorkerSelection.Version)
	if err != nil {
		return domain.SessionRecord{}, 0, 0, err
	}
	effective := *version.Definition.AgentType
	effective.SessionMode = domain.SessionModeTUI
	snapshot := domain.WorkerConfiguration{SchemaVersion: 1, AgentType: domain.WorkerDefinitionRef{ID: version.EntryID, Version: version.Number, ContentHash: version.ContentHash}, Selection: *cfg.WorkerSelection, Effective: effective, Origin: cfg.WorkerActor.Origin, ActorID: cfg.WorkerActor.ID, SystemPrompt: "Native Manager instructions", CreatedAt: time.Now().UTC()}
	snapshot.ContentHash = snapshot.Hash()
	rec, _, err := n.store.CreateAgentManagerSession(ctx, *cfg.ManagerController, domain.SessionRecord{ProjectID: cfg.ProjectID, Kind: cfg.Kind, Harness: effective.Harness, Mode: effective.SessionMode, Metadata: domain.SessionMetadata{Permissions: effective.Config.Permissions}, CreatedAt: time.Now().UTC()}, snapshot, time.Now().UTC())
	return rec, 0, 0, err
}

func managerControllerAPIFixture(t *testing.T) (http.Handler, *sqlite.Store, *apiManagerRuntime, controllers.AgentManagerConfigureRequest) {
	t.Helper()
	r, s, input := agentManagerRouter(t)
	registryRequest(t, r, http.MethodPut, "/projects/project/agent-manager", input, http.StatusOK)
	native := &apiManagerRuntime{store: s}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Route("/api/v1", (&controllers.AgentManagersController{Svc: managersvc.NewWithRuntime(s, native)}).Register)
	return router, s, native, input
}

func TestManagerControllerHTTPStartRetryAndHistoricalScope(t *testing.T) {
	r, s, native, configuration := managerControllerAPIFixture(t)
	const path = "/projects/project/agent-manager"
	w := registryRequest(t, r, http.MethodGet, path+"/controller", nil, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != `{"state":null}` {
		t.Fatalf("idle ownership: %s", w.Body.String())
	}
	input := controllers.AgentManagerStartRequest{ID: "start-one", ConfigurationVersion: 1, Reason: "Start configured Manager"}
	w = registryRequest(t, r, http.MethodPost, path+"/controllers", input, http.StatusOK)
	var receipt controllers.AgentManagerStartResponse
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || !receipt.Created || receipt.State.Dispatch == nil || receipt.State.Controller.Actor.Kind != "USER" || receipt.State.Controller.Actor.ID != "local-user" {
		t.Fatalf("start receipt: %+v %v (%s)", receipt, err, w.Body.String())
	}
	if native.calls != 1 {
		t.Fatalf("native calls: %d", native.calls)
	}
	registryRequest(t, r, http.MethodGet, path+"/controllers/start-one", nil, http.StatusOK)
	registryRequest(t, r, http.MethodGet, "/projects/other/agent-manager/controllers/start-one", nil, http.StatusNotFound)
	configuration.ExpectedRevision = 1
	configuration.Definition.Enabled = false
	registryRequest(t, r, http.MethodPut, path, configuration, http.StatusOK)
	w = registryRequest(t, r, http.MethodPost, path+"/controllers", input, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.Created || native.calls != 1 {
		t.Fatalf("retry launched: %+v %v", receipt, err)
	}
	input.ID = "second"
	w = registryRequest(t, r, http.MethodPost, path+"/controllers", input, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "AGENT_MANAGER_CONTROLLER_FENCED") || !strings.Contains(w.Body.String(), "requestId") {
		t.Fatalf("lost error envelope: %s", w.Body.String())
	}
	rows, err := s.ListAllSessions(context.Background())
	if err != nil || len(rows) != 1 || rows[0].Kind != domain.KindAgentManager {
		t.Fatalf("wrong native population: %+v %v", rows, err)
	}
}

func TestManagerControllerHTTPRejectsAuthorityAndOversizedBodies(t *testing.T) {
	r, s, native, _ := managerControllerAPIFixture(t)
	const base = `{"id":"start","configurationVersion":1,"reason":"Start configured Manager"}`
	for _, body := range []string{base + ` {}`, `null`, `[]`, `{}`, base[:len(base)-1] + `,"actor":{"kind":"SYSTEM","id":"daemon"}}`, base[:len(base)-1] + `,"workerSelection":{"agentTypeId":"forged"}}`, `{"reason":"` + strings.Repeat("x", 16<<10) + `"}`} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/projects/project/agent-manager/controllers", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "requestId") {
			t.Fatalf("invalid body: %d %s", w.Code, w.Body.String())
		}
	}
	if native.calls != 0 {
		t.Fatal("invalid body invoked native engine")
	}
	if _, found, err := s.ActiveAgentManagerController(context.Background(), "project"); err != nil || found {
		t.Fatalf("invalid body reserved ownership: %v %v", found, err)
	}
	// Governance-only builds reject launch before taking an admission.
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Route("/api/v1", (&controllers.AgentManagersController{Svc: managersvc.New(s)}).Register)
	registryRequest(t, router, http.MethodPost, "/projects/project/agent-manager/controllers", controllers.AgentManagerStartRequest{ID: "unavailable", ConfigurationVersion: 1, Reason: "Start"}, http.StatusNotImplemented)
}
