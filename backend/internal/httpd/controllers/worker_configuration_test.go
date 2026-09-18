package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

func (f *fakeSessionService) WorkerConfiguration(_ context.Context, id domain.SessionID) (*domain.WorkerConfiguration, error) {
	if id == "legacy" {
		return nil, nil
	}
	if id == "retained" {
		return &domain.WorkerConfiguration{AgentType: domain.WorkerDefinitionRef{ID: "disabled-type", Version: 3}, SystemPrompt: "Retained instructions"}, nil
	}
	return nil, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
}

func TestSessionsAPIWorkerConfigurationHistory(t *testing.T) {
	srv := newSessionTestServer(t, newFakeSessionService())
	for _, id := range []string{"legacy", "retained", "missing"} {
		t.Run(id, func(t *testing.T) {
			body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/sessions/"+id+"/worker-configuration", "")
			if id == "missing" {
				assertErrorCode(t, body, status, http.StatusNotFound, "SESSION_NOT_FOUND")
				var envelope struct {
					RequestID string `json:"requestId"`
				}
				if err := json.Unmarshal(body, &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.RequestID == "" {
					t.Fatal("missing request ID")
				}
				return
			}
			if status != http.StatusOK {
				t.Fatalf("status %d: %s", status, body)
			}
			var response controllers.WorkerConfigurationResponse
			if err := json.Unmarshal(body, &response); err != nil {
				t.Fatal(err)
			}
			if id == "legacy" && response.Configuration != nil {
				t.Fatal("invented legacy snapshot")
			}
			if id == "retained" && (response.Configuration == nil || response.Configuration.AgentType.Version != 3 || response.Configuration.SystemPrompt != "Retained instructions") {
				t.Fatalf("lost history: %s", body)
			}
		})
	}
}

func TestSessionsAPISpawnPassesWorkerSelectionWithTrustedActor(t *testing.T) {
	svc := newFakeSessionService()
	srv := newSessionTestServer(t, svc)
	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/sessions", `{"kind":"worker","prompt":"review","workerSelection":{"agentTypeId":"type-1","version":2,"overrides":{"instructions":"","skills":[]}}}`)
	if status != http.StatusCreated {
		t.Fatalf("status %d: %s", status, body)
	}
	selection := svc.lastSpawn.WorkerSelection
	if selection == nil || selection.AgentTypeID != "type-1" || selection.Version != 2 || selection.Overrides.Instructions == nil || *selection.Overrides.Instructions != "" || selection.Overrides.Skills == nil || len(*selection.Overrides.Skills) != 0 {
		t.Fatalf("selection lost: %+v", selection)
	}
	if svc.lastSpawn.WorkerActor.Origin != domain.RegistryUser || svc.lastSpawn.WorkerActor.ID == "" {
		t.Fatalf("actor: %+v", svc.lastSpawn.WorkerActor)
	}
}
