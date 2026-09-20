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

func TestRegistryPortableRoundTrip(t *testing.T) {
	r, svc := registryRouter(t)
	ctx := context.Background()
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "author"}
	skill, err := svc.Create(ctx, actor, domain.RegistrySkill, registrysvc.CreateInput{Metadata: domain.RegistryMetadata{Name: "Review", Enabled: true}, Definition: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Review exact commit", RequiredTools: []string{"git"}, Resources: []domain.SkillResource{{Path: "references/checklist.md", Content: "Retain test evidence"}}}}, Reason: "Author review"})
	if err != nil {
		t.Fatal(err)
	}
	input := registryCreateInput()
	input.Definition.AgentType.ProviderBindingID = "private-account-reference-never-export"
	input.Definition.AgentType.Skills = []domain.SkillVersionRef{{ID: skill.Entry.ID, Version: 1}}
	agent, err := svc.Create(ctx, actor, domain.RegistryAgentType, input)
	if err != nil {
		t.Fatal(err)
	}
	changed := skill.Version.Definition
	changed.Skill.Instructions = "Newer content must not replace the pin"
	if _, err := svc.Append(ctx, actor, domain.RegistrySkill, skill.Entry.ID, registrysvc.VersionInput{Definition: changed, ExpectedRevision: 1, Reason: "Evolve skill"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Activate(ctx, actor, domain.RegistrySkill, skill.Entry.ID, registrysvc.ActivateInput{Version: 2, ExpectedRevision: 2, Reason: "Activate skill"}); err != nil {
		t.Fatal(err)
	}
	w := registryRequest(t, r, http.MethodGet, "/agent-types/"+agent.Entry.ID+"/versions/1/export", nil, http.StatusOK)
	if strings.Contains(w.Body.String(), input.Definition.AgentType.ProviderBindingID) || strings.Contains(w.Body.String(), skill.Entry.ID) || strings.Contains(w.Body.String(), "Newer content") {
		t.Fatalf("export leaked local references or drifted: %s", w.Body.String())
	}
	var bundle registrysvc.PortableBundle
	if err := json.Unmarshal(w.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if !bundle.AgentType.RequiresProviderBinding || bundle.AgentType.Skills[0].Definition.Instructions != "Review exact commit" {
		t.Fatal("lost export requirements or exact pin")
	}
	w = registryRequest(t, r, http.MethodPost, "/agent-types/import", registrysvc.ImportInput{Bundle: bundle, Reason: "Import reviewed bundle"}, http.StatusCreated)
	var imported controllers.RegistryImportResponse
	if err := json.Unmarshal(w.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.Root.Entry.Metadata.Enabled || imported.Root.Entry.Metadata.Policy != (domain.RegistryPolicy{}) || imported.Root.Entry.ID == agent.Entry.ID || len(imported.ImportedSkills) != 1 || imported.ImportedSkills[0].Metadata.Enabled || imported.ImportedSkills[0].ID == skill.Entry.ID {
		t.Fatalf("unsafe import: %+v", imported)
	}
	definition := imported.Root.Version.Definition.AgentType
	if definition.ProviderBindingID != "" || !definition.ProviderBindingRequired || definition.Skills[0].ID != imported.ImportedSkills[0].ID {
		t.Fatal("import retained binding or wrong pins")
	}
	w = registryRequest(t, r, http.MethodGet, "/agent-types/"+imported.Root.Entry.ID+"/versions/1/export", nil, http.StatusOK)
	expected, _ := json.Marshal(bundle)
	if strings.TrimSpace(w.Body.String()) != string(expected) {
		t.Fatalf("round trip changed content: %s", w.Body.String())
	}
	registryRequest(t, r, http.MethodGet, "/skills/"+skill.Entry.ID+"/versions/1/export", nil, http.StatusOK)
}

func TestRegistryPortableRejectsUntrustedBundlesWithoutPartialWrites(t *testing.T) {
	r, svc := registryRouter(t)
	valid := `{"bundle":{"schemaVersion":1,"kind":"skill","name":"Review","description":"","skill":{"instructions":"Check","capabilities":[],"requiredTools":[],"requiredMcpServers":[],"resources":[]}},"reason":"Import"}`
	for _, body := range []string{
		strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":99`, 1),
		strings.Replace(valid, `"name":"Review"`, `"name":"Review","origin":"USER"`, 1),
		strings.Replace(valid, `"resources":[]`, `"resources":[{"path":"../escape","content":"bad"}]`, 1),
		strings.Replace(valid, `"instructions":"Check"`, `"instructions":"Check","apiKey":"secret"`, 1),
	} {
		registryRequest(t, r, http.MethodPost, "/skills/import", body, http.StatusBadRequest)
	}
	entries, err := svc.List(context.Background(), domain.RegistrySkill, "", 100)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid bundle wrote records: %v %v", entries, err)
	}
	registryRequest(t, r, http.MethodPost, "/skills/import", valid, http.StatusCreated)
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
