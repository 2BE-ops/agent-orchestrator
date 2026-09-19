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
	managersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agentmanager"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func agentManagerRouter(t *testing.T) (http.Handler, *sqlite.Store, controllers.AgentManagerConfigureRequest) {
	t.Helper()
	s := sqlitetest.MustOpen(t)
	ctx := context.Background()
	for _, id := range []string{"project", "other"} {
		if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: id, Path: "/repo/" + id, RegisteredAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.CreateRegistryEntry(ctx, "controller", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Manager", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, Instructions: "Route bounded work", MaxParallelWorkers: 1}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Create native controller"})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1", (&controllers.AgentManagersController{Svc: managersvc.New(s)}).Register)
	input := controllers.AgentManagerConfigureRequest{Definition: domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: "controller", AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}, Reason: "Set project governance"}
	return r, s, input
}

func TestAgentManagerHTTPGovernanceAndHistory(t *testing.T) {
	r, s, input := agentManagerRouter(t)
	const path = "/projects/project/agent-manager"
	registryRequest(t, r, http.MethodGet, path, nil, http.StatusNotFound)
	w := registryRequest(t, r, http.MethodPut, path, input, http.StatusOK)
	var first domain.AgentManagerConfiguration
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || first.Validate() != nil || first.Number != 1 || first.Actor.Kind != "USER" || first.Actor.ID != "local-user" {
		t.Fatalf("configuration: %+v %v", first, err)
	}
	w = registryRequest(t, r, http.MethodPut, path, input, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "AGENT_MANAGER_REVISION_CONFLICT") || !strings.Contains(w.Body.String(), "requestId") {
		t.Fatalf("conflict envelope: %s", w.Body.String())
	}
	input.ExpectedRevision = 1
	input.Definition.Policy.Optimization = "quality"
	registryRequest(t, r, http.MethodPut, path, input, http.StatusOK)
	w = registryRequest(t, r, http.MethodGet, path+"/configurations/1", nil, http.StatusOK)
	var retained domain.AgentManagerConfiguration
	if err := json.Unmarshal(w.Body.Bytes(), &retained); err != nil || retained.ContentHash != first.ContentHash {
		t.Fatalf("history changed: %+v %v", retained, err)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/configurations?limit=1", nil, http.StatusOK)
	var versions controllers.AgentManagerConfigurationsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &versions); err != nil || len(versions.Items) != 1 || versions.NextCursor != "1" {
		t.Fatalf("page: %+v %v", versions, err)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/configurations?cursor=1&limit=2", nil, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &versions); err != nil || len(versions.Items) != 1 || versions.Items[0].Number != 2 {
		t.Fatalf("next page: %+v %v", versions, err)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/audit", nil, http.StatusOK)
	var audit controllers.AgentManagerAuditResponse
	if err := json.Unmarshal(w.Body.Bytes(), &audit); err != nil || len(audit.Items) != 2 || audit.Items[1].ConfigurationVersion != 2 || audit.Items[1].Actor != first.Actor {
		t.Fatalf("audit: %+v %v", audit, err)
	}
	registryRequest(t, r, http.MethodGet, "/projects/other/agent-manager/configurations/1", nil, http.StatusNotFound)
	registryRequest(t, r, http.MethodPut, "/projects/missing/agent-manager", input, http.StatusNotFound)
	sessions, err := s.ListAllSessions(context.Background())
	if err != nil || len(sessions) != 0 {
		t.Fatalf("governance launched native sessions: %+v %v", sessions, err)
	}
}

func TestAgentManagerHTTPRejectsMalformedAuthorityAndBounds(t *testing.T) {
	r, s, input := agentManagerRouter(t)
	const path = "/projects/project/agent-manager"
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		string(encoded[:len(encoded)-1]) + `,"actor":{"kind":"USER","id":"forged"}}`,
		string(encoded[:len(encoded)-1]) + `,"origin":"USER"}`,
		string(encoded) + ` {}`, `null`, `[]`, `{}`, `{"reason":"` + strings.Repeat("x", 64<<10) + `"}`,
	} {
		request := httptest.NewRequest(http.MethodPut, "/api/v1"+path, strings.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, request)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid body: status %d %s", w.Code, w.Body.String())
		}
	}
	for _, change := range []func(*controllers.AgentManagerConfigureRequest){
		func(i *controllers.AgentManagerConfigureRequest) { i.ExpectedRevision = -1 },
		func(i *controllers.AgentManagerConfigureRequest) { i.Definition.AgentTypeVersion = 0 },
		func(i *controllers.AgentManagerConfigureRequest) {
			i.Definition.Policy.AllowCreateSkills = true
			i.Definition.Policy.MaxCreatedSkills = 0
		},
		func(i *controllers.AgentManagerConfigureRequest) { i.Reason = "" },
	} {
		bad := input
		change(&bad)
		registryRequest(t, r, http.MethodPut, path, bad, http.StatusBadRequest)
	}
	registryRequest(t, r, http.MethodPut, path, input, http.StatusOK)
	for _, suffix := range []string{"/configurations?limit=0", "/audit?cursor=-1", "/audit?cursor=9223372036854775808", "/audit?limit=101", "/audit?limit=1&limit=2", "/audit?limit=", "/audit?extra=true", "/configurations/0", "/configurations/1001", "/configurations/no"} {
		registryRequest(t, r, http.MethodGet, path+suffix, nil, http.StatusBadRequest)
	}
	current, err := s.GetAgentManager(context.Background(), "project")
	if err != nil || current.Number != 1 {
		t.Fatalf("rejected request wrote governance: %+v %v", current, err)
	}
	unavailable := chi.NewRouter()
	unavailable.Route("/api/v1", (&controllers.AgentManagersController{}).Register)
	registryRequest(t, unavailable, http.MethodGet, path, nil, http.StatusNotImplemented)
}
