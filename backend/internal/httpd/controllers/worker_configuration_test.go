package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
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

func (f *fakeSessionService) WorkerExecutions(ctx context.Context, id domain.SessionID, after int64, limit int) (sessionsvc.WorkerExecutionPage, error) {
	configuration, err := f.WorkerConfiguration(ctx, id)
	return sessionsvc.WorkerExecutionPage{Current: configuration, CurrentSequence: after, Events: []sessionsvc.WorkerExecutionSummary{}}, err
}

func (f *fakeSessionService) WorkerExecution(_ context.Context, id domain.SessionID, executionID string) (domain.WorkerExecution, error) {
	if id != "retained" || executionID != "change" {
		return domain.WorkerExecution{}, apierr.NotFound("WORKER_EXECUTION_NOT_FOUND", "Unknown worker execution")
	}
	return domain.WorkerExecution{ID: executionID, SessionID: id, Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}}, nil
}

func TestSessionsAPIWorkerExecutionPaginationAndIsolation(t *testing.T) {
	srv := newSessionTestServer(t, newFakeSessionService())
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=no", "?cursor=-1", "?cursor=wrong", "?cursor=999999999999999999999999"} {
		body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/sessions/retained/worker-executions"+query, "")
		assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_WORKER_HISTORY_PAGE")
	}
	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/sessions/retained/worker-executions?cursor=2&limit=1", "")
	var page controllers.WorkerExecutionHistoryResponse
	if err := json.Unmarshal(body, &page); err != nil || status != http.StatusOK || page.CurrentSequence != 2 || page.Current == nil {
		t.Fatalf("page: %s %v", body, err)
	}
	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/sessions/retained/worker-executions/change", "")
	var detail controllers.WorkerExecutionResponse
	if err := json.Unmarshal(body, &detail); err != nil || status != http.StatusOK || detail.Execution.Actor.ID != "human" {
		t.Fatalf("detail: %s %v", body, err)
	}
	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/sessions/legacy/worker-executions/change", "")
	assertErrorCode(t, body, status, http.StatusNotFound, "WORKER_EXECUTION_NOT_FOUND")
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
