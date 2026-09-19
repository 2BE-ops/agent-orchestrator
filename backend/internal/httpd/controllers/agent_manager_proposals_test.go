package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func managerProposalAPIFixture(t *testing.T) (http.Handler, *sqlite.Store, domain.SessionRecord) {
	t.Helper()
	ctx := context.Background()
	router, s, configurationInput := agentManagerRouter(t)
	registryRequest(t, router, http.MethodPut, "/projects/project/agent-manager", configurationInput, http.StatusOK)
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
	_, err = s.CreateAdaptiveTask(ctx, "native-task", "project", domain.TaskDefinition{Title: "Native routing request", Brief: "Retain exact intent", MaxAttempts: 1}, &criteria, domain.TaskMutation{Actor: configuration.Actor, Reason: "Plan exact work"})
	if err != nil {
		t.Fatal(err)
	}
	registryRequest(t, router, http.MethodPost, "/projects/project/agent-manager/requests", controllers.AgentManagerEnqueueRequest{ID: "native-request", TaskID: "native-task", TaskRevision: 1, ConfigurationVersion: 1, Reason: "Route this task"}, http.StatusOK)
	return router, s, rec
}

func TestAgentManagerHTTPNativeProposalReceiptsAndHistory(t *testing.T) {
	router, s, rec := managerProposalAPIFixture(t)
	path := "/sessions/" + string(rec.ID) + "/agent-manager/requests/native-request/proposals"
	input := controllers.AgentManagerProposalRequest{SourceGeneration: "native-generation", IdempotencyKey: "empty-output", Raw: ""}
	w := registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	var first controllers.AgentManagerProposalResponse
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || !first.Created || first.Proposal.ValidationError == "" || first.Proposal.SessionID != rec.ID || first.Proposal.NativeGeneration != "native-generation" || first.Proposal.Definition != nil {
		t.Fatalf("native rejection receipt: %+v %v", first, err)
	}
	w = registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	var replay controllers.AgentManagerProposalResponse
	if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil || replay.Created || replay.Proposal.ContentHash != first.Proposal.ContentHash {
		t.Fatalf("native retry: %+v %v", replay, err)
	}
	input.IdempotencyKey = "selected-output"
	input.Raw = `{"schemaVersion":1,"action":"select_existing","agentTypeId":"candidate","agentTypeVersion":1,"rationale":"Candidate explanation remains unvalidated","candidates":[]}`
	w = registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	var selected controllers.AgentManagerProposalResponse
	if err := json.Unmarshal(w.Body.Bytes(), &selected); err != nil || !selected.Created || selected.Proposal.Definition == nil || selected.Proposal.Number != 2 {
		t.Fatalf("parsed proposal: %+v %v", selected, err)
	}
	base := "/projects/project/agent-manager/requests/native-request/proposals"
	w = registryRequest(t, router, http.MethodGet, base, nil, http.StatusOK)
	var history controllers.AgentManagerProposalsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil || len(history.Items) != 2 || history.Items[0].Raw != "" || history.Items[1].Raw != input.Raw {
		t.Fatalf("exact native history: %+v %v", history, err)
	}
	registryRequest(t, router, http.MethodGet, base+"/"+first.Proposal.ID, nil, http.StatusOK)
	registryRequest(t, router, http.MethodGet, base+"?limit=1", nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, "/projects/other/agent-manager/requests/native-request/proposals/"+first.Proposal.ID, nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, "/projects/project/agent-manager/requests/other/proposals/"+first.Proposal.ID, nil, http.StatusNotFound)
	input.SourceGeneration = "stale"
	w = registryRequest(t, router, http.MethodPost, path, input, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "AGENT_MANAGER_OWNER_CHANGED") || !strings.Contains(w.Body.String(), "requestId") {
		t.Fatalf("native fence envelope: %s", w.Body.String())
	}
	input.SourceGeneration, input.IdempotencyKey = "native-generation", "replacement-output"
	registryRequest(t, router, http.MethodPost, path, input, http.StatusConflict)
	if _, found, err := s.GetAgentManagerRequestResolution(context.Background(), "project", "native-request"); err != nil || found {
		t.Fatalf("proposal applied routing intent: %v %v", found, err)
	}
	rows, err := s.ListAllSessions(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatalf("proposal launched worker: %+v %v", rows, err)
	}
	rec.IsTerminated = true
	if err := s.UpdateSession(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	registryRequest(t, router, http.MethodGet, base, nil, http.StatusOK)
	registryRequest(t, router, http.MethodPost, path, input, http.StatusConflict)
}

func TestAgentManagerHTTPNativeProposalRejectsAuthorityAndOversize(t *testing.T) {
	router, s, rec := managerProposalAPIFixture(t)
	path := "/sessions/" + string(rec.ID) + "/agent-manager/requests/native-request/proposals"
	for _, body := range []string{`{"sourceGeneration":"native-generation","idempotencyKey":"key","raw":"","actor":{"kind":"USER","id":"forged"}}`, `{"sourceGeneration":"native-generation","idempotencyKey":"key","raw":"","sourceOwner":{}}`, `{} {}`, `null`, `[]`, `{}`, `{"sourceGeneration":"native-generation","idempotencyKey":"key","raw":"` + strings.Repeat("x", (64<<10)+1) + `"}`, `{"raw":"` + strings.Repeat("x", 512<<10) + `"}`} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1"+path, strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("forged native body: %d %s", w.Code, w.Body.String())
		}
	}
	items, err := s.ListAgentManagerProposals(context.Background(), "project", "native-request")
	if err != nil || len(items) != 0 {
		t.Fatalf("invalid envelope created proposal: %+v %v", items, err)
	}
}
