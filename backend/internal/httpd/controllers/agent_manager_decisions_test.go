package controllers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	managersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agentmanager"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
)

func TestAgentManagerHTTPDecisionFeedbackHistoryAndNoPublicVerdict(t *testing.T) {
	r, s, rec := managerProposalAPIFixture(t)
	path := "/sessions/" + string(rec.ID) + "/agent-manager/requests/native-request/proposals"
	input := controllers.AgentManagerProposalRequest{SourceGeneration: rec.Metadata.RuntimeLaunchID, IdempotencyKey: "assess-on-retry", Raw: `{"schemaVersion":1,"action":"select_existing","agentTypeId":"controller","agentTypeVersion":1,"rationale":"Inspect protected selection","candidates":[]}`}
	w := registryRequest(t, r, http.MethodPost, path, input, http.StatusOK)
	var receipt controllers.AgentManagerProposalResponse
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	historyPath := "/projects/project/agent-manager/requests/native-request/decisions"
	w = registryRequest(t, r, http.MethodGet, historyPath+"/"+receipt.Proposal.ID, nil, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != `{"decision":null}` {
		t.Fatalf("pending response: %s", w.Body.String())
	}
	svc := managersvc.New(s)
	svc.SetCandidateAssessor(registrysvc.New(s))
	configured := chi.NewRouter()
	configured.Use(middleware.RequestID)
	configured.Route("/api/v1", (&controllers.AgentManagersController{Svc: svc}).Register)
	w = registryRequest(t, configured, http.MethodPost, path, input, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.Created || receipt.Decision == nil || receipt.Decision.Outcome != "rejected" || receipt.Decision.Candidates[0].Issues[0].Code != "MANAGER_SELECTION_FORBIDDEN" {
		t.Fatalf("lost automatic rejection: %s %v", w.Body.String(), err)
	}
	w = registryRequest(t, configured, http.MethodGet, historyPath, nil, http.StatusOK)
	var history controllers.AgentManagerDecisionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil || len(history.Items) != 1 || history.Items[0].ContentHash != receipt.Decision.ContentHash {
		t.Fatalf("history: %s %v", w.Body.String(), err)
	}
	w = registryRequest(t, configured, http.MethodGet, historyPath+"/"+receipt.Proposal.ID, nil, http.StatusOK)
	var exact controllers.AgentManagerDecisionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &exact); err != nil || exact.Decision == nil || exact.Decision.Validate() != nil {
		t.Fatalf("exact history: %s %v", w.Body.String(), err)
	}
	registryRequest(t, configured, http.MethodGet, "/projects/other/agent-manager/requests/native-request/decisions", nil, http.StatusNotFound)
	registryRequest(t, configured, http.MethodGet, historyPath+"/missing", nil, http.StatusNotFound)
	registryRequest(t, configured, http.MethodPost, path, map[string]any{"sourceGeneration": input.SourceGeneration, "idempotencyKey": "forged", "raw": input.Raw, "decision": map[string]any{"outcome": "accepted"}}, http.StatusBadRequest)
	input.IdempotencyKey = "escalate"
	input.Raw = `{"schemaVersion":1,"action":"needs_human","rationale":"No permitted configuration","candidates":[]}`
	w = registryRequest(t, configured, http.MethodPost, path, input, http.StatusOK)
	receipt = controllers.AgentManagerProposalResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.Decision != nil || receipt.RoutingOutcome != "needs_human" {
		t.Fatalf("terminal feedback: %s %v", w.Body.String(), err)
	}
}
