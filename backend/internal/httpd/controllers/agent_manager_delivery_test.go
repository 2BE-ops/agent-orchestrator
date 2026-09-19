package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

func TestAgentManagerHTTPInspectExactInputAndTransportWithoutOwnerInternals(t *testing.T) {
	router, s, _ := managerProposalAPIFixture(t)
	prefix := "/projects/project/agent-manager/requests/native-request"
	w := registryRequest(t, router, http.MethodGet, prefix+"/contexts", nil, http.StatusOK)
	var contexts controllers.AgentManagerContextsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &contexts); err != nil || len(contexts.Items) != 1 {
		t.Fatalf("context history: %s %v", w.Body.String(), err)
	}
	sealed := contexts.Items[0]
	w = registryRequest(t, router, http.MethodGet, prefix+"/contexts/"+sealed.ID, nil, http.StatusOK)
	var exact domain.AgentManagerContext
	if err := json.Unmarshal(w.Body.Bytes(), &exact); err != nil || exact.ContentHash != sealed.ContentHash || exact.Prompt != sealed.Prompt || exact.ProjectID != "project" || exact.Sources[0].Classification != domain.ContextTechnical {
		t.Fatalf("input provenance: %s %v", w.Body.String(), err)
	}
	w = registryRequest(t, router, http.MethodGet, prefix+"/deliveries", nil, http.StatusOK)
	var deliveries controllers.AgentManagerDeliveriesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &deliveries); err != nil || len(deliveries.Items) != 1 || deliveries.Items[0].ContextID != sealed.ID || deliveries.Items[0].State != "handed_off" {
		t.Fatalf("transport history: %s %v", w.Body.String(), err)
	}
	if strings.Contains(w.Body.String(), `"owner"`) || strings.Contains(w.Body.String(), `"RuntimeLaunchID"`) {
		t.Fatal("internal native owner entered wire response")
	}
	configuration, err := s.GetAgentManager(context.Background(), "project")
	if err != nil {
		t.Fatal(err)
	}
	configuration.Definition.Enabled = false
	if _, err := s.ConfigureAgentManager(context.Background(), "project", configuration.Definition, domain.TaskMutation{Actor: configuration.Actor, Reason: "Disable after receipt", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	registryRequest(t, router, http.MethodGet, prefix+"/contexts/"+sealed.ID, nil, http.StatusOK)
	for _, suffix := range []string{"contexts", "contexts/" + sealed.ID, "deliveries"} {
		registryRequest(t, router, http.MethodGet, "/projects/foreign/agent-manager/requests/native-request/"+suffix, nil, http.StatusNotFound)
	}
	registryRequest(t, router, http.MethodGet, prefix+"/contexts/missing", nil, http.StatusNotFound)
}
