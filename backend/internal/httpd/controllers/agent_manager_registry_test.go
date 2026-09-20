package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	managersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agentmanager"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// managerRegistryAPIFixture mirrors the native proposal fixture with a
// governance policy that permits Manager authoring, and mounts the normal
// registry surface beside the Manager API.
func managerRegistryAPIFixture(t *testing.T) (http.Handler, *sqlite.Store, domain.SessionRecord) {
	t.Helper()
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	for _, id := range []string{"project", "other"} {
		if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: id, Path: "/repo/" + id, RegisteredAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateRegistryEntry(ctx, "controller", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Manager", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, Instructions: "Route bounded work", MaxParallelWorkers: 1}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Create native controller"}); err != nil {
		t.Fatal(err)
	}
	policy := domain.DefaultAgentManagerPolicy()
	policy.AllowCreateTypes, policy.AllowCreateSkills, policy.AllowCreateVersions = true, true, true
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1", func(r chi.Router) {
		(&controllers.AgentManagersController{Svc: managersvc.New(s)}).Register(r)
		(&controllers.RegistryController{Svc: registrysvc.New(s)}).Register(r)
	})
	configurationInput := controllers.AgentManagerConfigureRequest{Definition: domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: "controller", AgentTypeVersion: 1, Policy: policy}, Reason: "Set project governance"}
	registryRequest(t, r, http.MethodPut, "/projects/project/agent-manager", configurationInput, http.StatusOK)
	configuration, err := s.GetAgentManager(ctx, "project")
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.GetRegistryVersion(ctx, "controller", 1)
	if err != nil {
		t.Fatal(err)
	}
	effective := *version.Definition.AgentType
	effective.SessionMode = domain.SessionModeTUI
	snapshot := domain.WorkerConfiguration{SchemaVersion: 1, AgentType: configuration.ControllerType, Selection: domain.WorkerSelection{AgentTypeID: "controller", Version: 1}, Effective: effective, Origin: domain.RegistryUser, ActorID: configuration.Actor.ID, SystemPrompt: "Route bounded work", CreatedAt: time.Now().UTC()}
	snapshot.ContentHash = snapshot.Hash()
	controller, _, err := s.ReserveAgentManagerController(ctx, domain.AgentManagerControllerReservation{AgentManagerControllerToken: domain.AgentManagerControllerToken{ID: "native-controller", ProjectID: "project", ConfigurationVersion: 1}, Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "daemon"}, Reason: "Start configured Manager", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := s.CreateAgentManagerSession(ctx, controller.AgentManagerControllerToken, domain.SessionRecord{ProjectID: "project", Kind: domain.KindAgentManager, Harness: effective.Harness, Mode: effective.SessionMode, Metadata: domain.SessionMetadata{Permissions: effective.Config.Permissions}, CreatedAt: time.Now().UTC()}, snapshot, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	op := domain.AgentManagerExecutionOperation{ID: "native-generation", ControllerID: controller.ID, SessionID: rec.ID, SourceOwner: rec.ControllerOwner(), Kind: "dispatch", CreatedAt: time.Now().UTC()}
	if _, err := s.BeginAgentManagerExecution(ctx, op); err != nil {
		t.Fatal(err)
	}
	rec.Metadata.RuntimeLaunchID = op.ID
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveAgentManagerExecution(ctx, domain.AgentManagerExecutionResolution{OperationID: op.ID, ObservedOwner: rec.ControllerOwner(), Outcome: "connected", Reason: "Observed connected native source", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "checked", Requirement: "Independent validation", EvidenceKind: "manual"}}}
	if _, err := s.CreateAdaptiveTask(ctx, "native-task", "project", domain.TaskDefinition{Title: "Native routing request", Brief: "Retain exact intent", MaxAttempts: 1}, &criteria, domain.TaskMutation{Actor: configuration.Actor, Reason: "Plan exact work"}); err != nil {
		t.Fatal(err)
	}
	registryRequest(t, r, http.MethodPost, "/projects/project/agent-manager/requests", controllers.AgentManagerEnqueueRequest{ID: "native-request", TaskID: "native-task", TaskRevision: 1, ConfigurationVersion: 1, Reason: "Route this task"}, http.StatusOK)
	delivery, _, err := s.BeginAgentManagerDelivery(ctx, domain.AgentManagerContextSeal{ID: "native-context", ProjectID: "project", RequestID: "native-request", SessionID: rec.ID, SourceOwner: rec.ControllerOwner(), Now: time.Now().UTC()}, "native-delivery")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveAgentManagerDelivery(ctx, domain.AgentManagerDeliveryResolution{ID: delivery.ID, State: "handed_off", Reason: "Observed native input acceptance"}); err != nil {
		t.Fatal(err)
	}
	return r, s, rec
}

func managerRegistryActionInput() controllers.AgentManagerRegistryRequest {
	return controllers.AgentManagerRegistryRequest{SourceGeneration: "native-generation", IdempotencyKey: "author-key", Action: domain.AgentManagerRegistryAction{Action: "create", Kind: domain.RegistrySkill, Name: "Binary Format Tests", Definition: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Verify decoded boundaries", Capabilities: []string{"binary-tests"}}}, Reason: "Existing Skills lack binary format assertions"}}
}

func TestAgentManagerHTTPNativeRegistryAuthoringAndHistory(t *testing.T) {
	router, _, rec := managerRegistryAPIFixture(t)
	path := "/sessions/" + string(rec.ID) + "/agent-manager/requests/native-request/registry-actions"
	input := managerRegistryActionInput()
	w := registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	var first controllers.AgentManagerRegistryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || !first.Created || first.Receipt.Target.ID == "" || first.Receipt.Target.Version != 1 || first.Receipt.Classification != domain.ContextTechnical || first.Receipt.NativeGeneration != "native-generation" || first.Receipt.SessionID != rec.ID {
		t.Fatalf("native authoring receipt: %+v %v", first, err)
	}
	w = registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	var replay controllers.AgentManagerRegistryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil || replay.Created || replay.Receipt.ContentHash != first.Receipt.ContentHash {
		t.Fatalf("native authoring replay: %+v %v", replay, err)
	}
	changed := input
	changed.Action.Name = "Renamed Skill"
	w = registryRequest(t, router, http.MethodPost, path, changed, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "AGENT_MANAGER_REGISTRY_CONFLICT") {
		t.Fatalf("changed replay envelope: %s", w.Body.String())
	}
	versioned := input
	versioned.IdempotencyKey = "version-key"
	versioned.Action.Action, versioned.Action.Name = "append_version", ""
	versioned.Action.EntryID, versioned.Action.ExpectedRevision = first.Receipt.Target.ID, first.Receipt.MetadataRevision
	versioned.Action.Definition.Skill.Instructions = "Verify decoded boundaries and framing"
	w = registryRequest(t, router, http.MethodPost, path, versioned, http.StatusOK)
	var appended controllers.AgentManagerRegistryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &appended); err != nil || !appended.Created || appended.Receipt.Target.Version != 2 || appended.Receipt.MetadataRevision != first.Receipt.MetadataRevision+1 {
		t.Fatalf("appended version: %+v %v", appended, err)
	}
	base := "/projects/project/agent-manager/requests/native-request/registry-receipts"
	w = registryRequest(t, router, http.MethodGet, base+"?limit=100", nil, http.StatusOK)
	var history controllers.AgentManagerRegistryReceiptsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil || len(history.Items) != 2 {
		t.Fatalf("receipt history: %+v %v", history, err)
	}
	// Receipt pages keyset by unique receipt ID, not creation order.
	actions := map[string]int{}
	for _, item := range history.Items {
		actions[item.Action.Action]++
		if item.RequestID != "native-request" || item.SessionID != rec.ID {
			t.Fatalf("unsealed receipt attribution: %+v", item)
		}
	}
	if actions["create"] != 1 || actions["append_version"] != 1 {
		t.Fatalf("unexpected retained actions: %+v", actions)
	}
	registryRequest(t, router, http.MethodGet, base+"/"+first.Receipt.ID, nil, http.StatusOK)
	registryRequest(t, router, http.MethodGet, base+"?limit=0", nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, base+"?limit=101", nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, base+"?afterId=bad%0Acursor&limit=5", nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, base+"?unknown=1", nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, "/projects/other/agent-manager/requests/native-request/registry-receipts/"+first.Receipt.ID, nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, "/projects/project/agent-manager/requests/other/registry-receipts/"+first.Receipt.ID, nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, "/projects/project/agent-manager/requests/other/registry-receipts?limit=5", nil, http.StatusNotFound)
}

// Manager-authored definitions must be visible through the normal registry
// surface the desktop UI reads, with manager origin and retained identity.
func TestAgentManagerHTTPAuthoredDefinitionsAppearInNormalRegistry(t *testing.T) {
	router, s, rec := managerRegistryAPIFixture(t)
	path := "/sessions/" + string(rec.ID) + "/agent-manager/requests/native-request/registry-actions"
	w := registryRequest(t, router, http.MethodPost, path, managerRegistryActionInput(), http.StatusOK)
	var first controllers.AgentManagerRegistryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || !first.Created {
		t.Fatalf("native authoring receipt: %+v %v", first, err)
	}
	w = registryRequest(t, router, http.MethodGet, "/skills?limit=100", nil, http.StatusOK)
	var skills struct {
		Items []registrysvc.View `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &skills); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, view := range skills.Items {
		if view.Entry.ID == first.Receipt.Target.ID {
			found = true
			if view.Entry.Origin != domain.RegistryManager || view.Entry.Metadata.Name != "Binary Format Tests" || view.Version.Number != 1 {
				t.Fatalf("manager origin or identity lost: %+v", view.Entry)
			}
		}
	}
	if !found {
		t.Fatal("manager-authored skill missing from normal registry listing")
	}
	entry, err := s.GetRegistryEntry(context.Background(), first.Receipt.Target.ID)
	if err != nil || entry.Origin != domain.RegistryManager || entry.CreatedBy == "human" {
		t.Fatalf("manager-owned entry: %+v %v", entry, err)
	}
}

func TestAgentManagerHTTPNativeRegistryRejectsForgedAndOversize(t *testing.T) {
	router, s, rec := managerRegistryAPIFixture(t)
	path := "/sessions/" + string(rec.ID) + "/agent-manager/requests/native-request/registry-actions"
	for _, body := range []string{
		`{"sourceGeneration":"native-generation","idempotencyKey":"key","action":{"action":"create","kind":"skill","name":"S","definition":{}},"actor":{"kind":"USER","id":"forged"}}`,
		`{"sourceGeneration":"native-generation","idempotencyKey":"key","action":{"action":"create","kind":"skill","name":"S","definition":{}},"createdEntryId":"chosen-id"}`,
		`{}`,
	} {
		registryRequest(t, router, http.MethodPost, path, json.RawMessage(body), http.StatusBadRequest)
	}
	registryRequest(t, router, http.MethodPost, path, json.RawMessage(`{"sourceGeneration":"stale","idempotencyKey":"key","action":{"action":"create","kind":"skill","name":"S","reason":"x","definition":{"skill":{"instructions":"x"}}}}`), http.StatusConflict)
	registryRequest(t, router, http.MethodPost, path, json.RawMessage(`{"sourceGeneration":"native-generation","idempotencyKey":"key","action":{"action":"create","kind":"skill","name":"S","reason":"x","definition":{"skill":{"instructions":"`+strings.Repeat("x", 288<<10)+`"}}}}`), http.StatusBadRequest)
	items, err := s.ListAgentManagerRegistryReceipts(context.Background(), "project", "native-request", "", 100)
	if err != nil || len(items) != 0 {
		t.Fatalf("invalid envelope authored registry content: %+v %v", items, err)
	}
}
