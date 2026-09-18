package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type bindingNative struct{}

func (bindingNative) Configuration(context.Context, string, domain.SessionMode) (agentsvc.Configuration, error) {
	return agentsvc.Configuration{Fields: []agentsvc.ConfigurationField{{Key: "model"}, {Key: "permissions", Options: []string{"auto"}}}, CapabilityState: "supported"}, nil
}
func (bindingNative) Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error) {
	return ports.AgentModelCatalog{Models: []ports.AgentModelInfo{{ID: "native/model", Provider: "native"}}}, nil
}
func (bindingNative) EnsureAgentReadiness(context.Context, string, domain.AgentReadinessPurpose) (domain.AgentReadinessSnapshot, error) {
	return domain.AgentReadinessSnapshot{EffectiveReadiness: domain.AgentReadinessReady}, nil
}

func TestProviderBindingsAPIAndValidation(t *testing.T) {
	svc := registrysvc.NewWithNative(sqlitetest.MustOpen(t), bindingNative{})
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1", (&controllers.RegistryController{Svc: svc}).Register)
	input := registrysvc.BindingCreateInput{Name: "Native account", Harness: domain.HarnessCodex, Provider: "native", Reason: "Reuse native provider"}
	w := registryRequest(t, r, http.MethodPost, "/provider-bindings", input, http.StatusCreated)
	var binding controllers.ProviderBindingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &binding); err != nil {
		t.Fatal(err)
	}
	if binding.ID == "" || binding.Revision != 1 || !binding.Enabled {
		t.Fatalf("bad binding: %+v", binding)
	}
	registryRequest(t, r, http.MethodGet, "/provider-bindings/"+binding.ID, nil, http.StatusOK)
	w = registryRequest(t, r, http.MethodGet, "/provider-bindings?limit=1", nil, http.StatusOK)
	if !strings.Contains(w.Body.String(), binding.ID) {
		t.Fatal("binding missing from list")
	}
	bad := input
	bad.Provider = "unconfigured"
	registryRequest(t, r, http.MethodPost, "/provider-bindings", bad, http.StatusBadRequest)
	registryRequest(t, r, http.MethodPost, "/provider-bindings", `{"name":"Injected","harness":"codex","provider":"native","reason":"test","apiKey":"must-not-be-accepted"}`, http.StatusBadRequest)
	create := registryCreateInput()
	create.Definition.AgentType.Config.Model = "native/model"
	create.Definition.AgentType.ProviderBindingID = binding.ID
	entry, err := svc.Create(context.Background(), domain.RegistryActor{Origin: domain.RegistryUser, ID: "author"}, domain.RegistryAgentType, create)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		w := registryRequest(t, r, http.MethodPost, "/agent-types/"+entry.Entry.ID+"/validate", registrysvc.CheckInput{Version: 1}, http.StatusOK)
		var result registrysvc.ConfigurationCheck
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Ready != want {
			t.Fatalf("ready=%v, want %v: %s", result.Ready, want, w.Body.String())
		}
	}
	check(true)
	update := registrysvc.BindingUpdateInput{Name: binding.Name, Enabled: false, ExpectedRevision: 1, Reason: "Disable unavailable account"}
	registryRequest(t, r, http.MethodPatch, "/provider-bindings/"+binding.ID, update, http.StatusOK)
	w = registryRequest(t, r, http.MethodPatch, "/provider-bindings/"+binding.ID, update, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "requestId") {
		t.Fatal("conflict lost error envelope request ID")
	}
	check(false)
	w = registryRequest(t, r, http.MethodGet, "/provider-bindings/"+binding.ID+"/audit", nil, http.StatusOK)
	if !strings.Contains(w.Body.String(), "local-user") || !strings.Contains(w.Body.String(), update.Reason) {
		t.Fatalf("missing human attribution: %s", w.Body.String())
	}
	registryRequest(t, r, http.MethodGet, "/provider-bindings/missing", nil, http.StatusNotFound)
}

func TestProviderBindingNativeServiceUnavailable(t *testing.T) {
	r, _ := registryRouter(t)
	registryRequest(t, r, http.MethodPost, "/provider-bindings", registrysvc.BindingCreateInput{Name: "Native", Harness: domain.HarnessCodex, Reason: "Create reference"}, http.StatusNotImplemented)
}
