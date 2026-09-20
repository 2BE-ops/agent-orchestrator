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
	managersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agentmanager"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
)

func TestAgentManagerHTTPCandidateChecksAndBoundedErrors(t *testing.T) {
	_, s, configuration := agentManagerRouter(t)
	svc := managersvc.New(s)
	svc.SetCandidateAssessor(registrysvc.New(s))
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1", (&controllers.AgentManagersController{Svc: svc}).Register)
	const path = "/projects/project/agent-manager"
	registryRequest(t, r, http.MethodPut, path, configuration, http.StatusOK)
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "checked", Requirement: "Verified output", EvidenceKind: "manual"}}}
	if _, err := s.CreateAdaptiveTask(context.Background(), "task", "project", domain.TaskDefinition{Title: "Route work", Brief: "Exact intent", MaxAttempts: 1}, &criteria, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Plan"}); err != nil {
		t.Fatal(err)
	}
	registryRequest(t, r, http.MethodPost, path+"/requests", controllers.AgentManagerEnqueueRequest{ID: "request", TaskID: "task", TaskRevision: 1, ConfigurationVersion: 1, Reason: "Route"}, http.StatusOK)
	w := registryRequest(t, r, http.MethodGet, path+"/requests/request/candidates?limit=1", nil, http.StatusOK)
	var page controllers.AgentManagerCandidatesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.Items[0].Eligible || len(page.Items[0].Issues) != 1 || page.Items[0].Issues[0].Code != "MANAGER_SELECTION_FORBIDDEN" || page.NextCursor != "" {
		t.Fatalf("lost exclusion: %s %v", w.Body.String(), err)
	}
	w = registryRequest(t, r, http.MethodGet, path+"/requests/request/candidates/controller?version=1", nil, http.StatusOK)
	var exact domain.AgentManagerCandidate
	if err := json.Unmarshal(w.Body.Bytes(), &exact); err != nil || exact.AgentType.Version != 1 || exact.AgentType.ID != "controller" {
		t.Fatalf("exact candidate: %s %v", w.Body.String(), err)
	}
	for _, suffix := range []string{"?limit=21", "?limit=0", "?limit=bad", "/controller", "/controller?version=0", "/controller?version=invalid"} {
		w = registryRequest(t, r, http.MethodGet, path+"/requests/request/candidates"+suffix, nil, http.StatusBadRequest)
		if !strings.Contains(w.Body.String(), "requestId") {
			t.Fatal("error envelope lost request ID")
		}
	}
	registryRequest(t, r, http.MethodGet, "/projects/other/agent-manager/requests/request/candidates", nil, http.StatusNotFound)
	registryRequest(t, r, http.MethodGet, path+"/requests/request/candidates?cursor=controller", nil, http.StatusOK)
}
