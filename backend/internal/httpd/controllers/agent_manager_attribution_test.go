package controllers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	managersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agentmanager"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
)

func TestAgentManagerRoutingAttributionAPI(t *testing.T) {
	_, s, rec := managerProposalAPIFixture(t)
	path := "/sessions/" + string(rec.ID) + "/agent-manager/requests/native-request/proposals"
	input := controllers.AgentManagerProposalRequest{SourceGeneration: rec.Metadata.RuntimeLaunchID, IdempotencyKey: "assess-on-retry", Raw: `{"schemaVersion":1,"action":"select_existing","agentTypeId":"controller","agentTypeVersion":1,"rationale":"Inspect protected selection","candidates":[]}`}
	svc := managersvc.New(s)
	svc.SetCandidateAssessor(registrysvc.New(s))
	configured := chi.NewRouter()
	configured.Use(middleware.RequestID)
	configured.Route("/api/v1", (&controllers.AgentManagersController{Svc: svc}).Register)
	w := registryRequest(t, configured, http.MethodPost, path, input, http.StatusOK)
	var receipt controllers.AgentManagerProposalResponse
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.Decision == nil || receipt.Decision.Outcome != "rejected" {
		t.Fatalf("automatic rejection: %s (%v)", w.Body.String(), err)
	}
	w = registryRequest(t, configured, http.MethodGet, "/projects/project/agent-manager/routing-outcomes?limit=20", nil, http.StatusOK)
	var outcomes controllers.ManagerRoutingOutcomesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &outcomes); err != nil || len(outcomes.Items) != 1 {
		t.Fatalf("routing outcomes: %s (%v)", w.Body.String(), err)
	}
	if item := outcomes.Items[0]; item.Outcome != "rejected" || item.AgentType != nil || item.State != "pending" || item.TaskID == "" || item.Sequence < 1 || item.DecisionID != receipt.Decision.ProposalID {
		t.Fatalf("routing outcome row: %+v", item)
	}
	from := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	to := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	w = registryRequest(t, configured, http.MethodGet, "/projects/project/agent-manager/routing-summary?from="+from+"&to="+to, nil, http.StatusOK)
	var summary controllers.ManagerRoutingSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil || summary.Summary.Totals.Decisions != 1 || summary.Summary.Totals.Rejected != 1 || summary.Summary.Totals.Accepted != 0 || len(summary.Summary.Types) != 0 {
		t.Fatalf("routing summary: %s (%v)", w.Body.String(), err)
	}
	w = registryRequest(t, configured, http.MethodGet, "/projects/other/agent-manager/routing-outcomes?limit=20", nil, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &outcomes); err != nil || len(outcomes.Items) != 0 {
		t.Fatalf("unknown project must return an empty page: %s (%v)", w.Body.String(), err)
	}
	registryRequest(t, configured, http.MethodGet, "/projects/project/agent-manager/routing-outcomes?limit=0", nil, http.StatusBadRequest)
	registryRequest(t, configured, http.MethodGet, "/projects/project/agent-manager/routing-outcomes?after=-1&limit=5", nil, http.StatusBadRequest)
	registryRequest(t, configured, http.MethodGet, "/projects/project/agent-manager/routing-summary?from="+to+"&to="+from, nil, http.StatusBadRequest)
	registryRequest(t, configured, http.MethodGet, "/projects/project/agent-manager/routing-summary?from=not-a-time&to="+to, nil, http.StatusBadRequest)
}
