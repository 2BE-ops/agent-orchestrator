package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

func TestAgentManagerHTTPInboxAndResolution(t *testing.T) {
	r, s, configuration := agentManagerRouter(t)
	const path = "/projects/project/agent-manager"
	registryRequest(t, r, http.MethodPut, path, configuration, http.StatusOK)
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "checked", Requirement: "Independent verification", EvidenceKind: "manual"}}}
	_, err := s.CreateAdaptiveTask(context.Background(), "route-task", "project", domain.TaskDefinition{Title: "Exact routing intent", Brief: "This brief is not embedded in the inbox", MaxAttempts: 1}, &criteria, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Plan work"})
	if err != nil {
		t.Fatal(err)
	}
	input := controllers.AgentManagerEnqueueRequest{ID: "request-one", TaskID: "route-task", TaskRevision: 1, ConfigurationVersion: 1, Reason: "Route current task"}
	w := registryRequest(t, r, http.MethodPost, path+"/requests", input, http.StatusOK)
	var request domain.AgentManagerRequest
	if err := json.Unmarshal(w.Body.Bytes(), &request); err != nil || request.Validate() != nil || request.Actor.ID != "local-user" || request.TaskRevision != 1 || request.CriteriaVersion != 1 {
		t.Fatalf("sealed request: %+v %v", request, err)
	}
	firstJSON := w.Body.String()
	w = registryRequest(t, r, http.MethodPost, path+"/requests", input, http.StatusOK)
	if w.Body.String() != firstJSON {
		t.Fatal("retry changed durable request")
	}
	w = registryRequest(t, r, http.MethodGet, path+"/requests/request-one", nil, http.StatusOK)
	if w.Body.String() != firstJSON {
		t.Fatal("read changed sealed request")
	}
	w = registryRequest(t, r, http.MethodGet, path+"/inbox?limit=1", nil, http.StatusOK)
	var page controllers.AgentManagerRequestsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("pending page: %+v %v", page, err)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/requests/request-one/resolution", nil, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != `{"resolution":null}` {
		t.Fatalf("pending receipt: %s", w.Body.String())
	}
	bad := input
	bad.Reason = "Changed under same ID"
	w = registryRequest(t, r, http.MethodPost, path+"/requests", bad, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "AGENT_MANAGER_REQUEST_CONFLICT") || !strings.Contains(w.Body.String(), "requestId") {
		t.Fatalf("lost conflict envelope: %s", w.Body.String())
	}
	resolve := controllers.AgentManagerResolveRequest{Outcome: "needs_human", Reason: "No suitable configuration"}
	w = registryRequest(t, r, http.MethodPost, path+"/requests/request-one/resolution", resolve, http.StatusOK)
	firstResolution := w.Body.String()
	w = registryRequest(t, r, http.MethodPost, path+"/requests/request-one/resolution", resolve, http.StatusOK)
	if w.Body.String() != firstResolution {
		t.Fatal("resolution retry replaced proof")
	}
	w = registryRequest(t, r, http.MethodGet, path+"/inbox", nil, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 0 {
		t.Fatalf("resolved inbox: %+v %v", page, err)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/requests", nil, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("history lost: %+v %v", page, err)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/requests/request-one/resolution", nil, http.StatusOK)
	var terminal controllers.AgentManagerResolutionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &terminal); err != nil || terminal.Resolution == nil || terminal.Resolution.Outcome != "needs_human" {
		t.Fatalf("terminal receipt: %+v %v", terminal, err)
	}
	for _, suffix := range []string{"/requests/request-one", "/requests/request-one/resolution"} {
		registryRequest(t, r, http.MethodGet, "/projects/other/agent-manager"+suffix, nil, http.StatusNotFound)
	}
	registryRequest(t, r, http.MethodPost, "/projects/other/agent-manager/requests/request-one/resolution", resolve, http.StatusNotFound)
	configuration.ExpectedRevision = 1
	configuration.Definition.Enabled = false
	registryRequest(t, r, http.MethodPut, path, configuration, http.StatusOK)
	input.ID = "after-disable"
	w = registryRequest(t, r, http.MethodPost, path+"/requests", input, http.StatusConflict)
	if !strings.Contains(w.Body.String(), "AGENT_MANAGER_WORK_FENCED") {
		t.Fatalf("disabled inbox: %s", w.Body.String())
	}
	rows, err := s.ListAllSessions(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatalf("inbox launched sessions: %+v %v", rows, err)
	}
	intent, err := s.GetTaskIntent(context.Background(), "route-task")
	if err != nil || intent.Intent != "run" {
		t.Fatalf("resolution changed task intent: %+v %v", intent, err)
	}
}

func TestAgentManagerHTTPInboxStrictPayloadsAndPages(t *testing.T) {
	r, _, configuration := agentManagerRouter(t)
	const path = "/projects/project/agent-manager"
	registryRequest(t, r, http.MethodPut, path, configuration, http.StatusOK)
	for _, suffix := range []string{"/requests", "/requests/work/resolution"} {
		for _, body := range []string{`{"actor":{"kind":"USER","id":"forged"}}`, `{"sessionId":"borrowed"}`, `{} {}`, `null`, `[]`, `{}`, `{"reason":"` + strings.Repeat("x", 16<<10) + `"}`} {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1"+path+suffix, strings.NewReader(body)))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid inbox body %s: %d %s", suffix, w.Code, w.Body.String())
			}
		}
	}
	for _, suffix := range []string{"/inbox?cursor=-1", "/requests?limit=101", "/inbox?limit=1&limit=2", "/requests?limit=", "/inbox?cursor=9223372036854775808", "/requests?state=pending"} {
		registryRequest(t, r, http.MethodGet, path+suffix, nil, http.StatusBadRequest)
	}
}
