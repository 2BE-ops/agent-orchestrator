package controllers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

func (c *SessionsController) workerConfiguration(w http.ResponseWriter, r *http.Request) {
	service, ok := c.Svc.(interface {
		WorkerConfiguration(context.Context, domain.SessionID) (*domain.WorkerConfiguration, error)
	})
	if !ok {
		apispec.NotImplemented(w, r, "GET", "/api/v1/sessions/{sessionId}/worker-configuration")
		return
	}
	snapshot, err := service.WorkerConfiguration(r.Context(), sessionID(r))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, WorkerConfigurationResponse{Configuration: snapshot})
}

func (c *SessionsController) workerExecutions(w http.ResponseWriter, r *http.Request) {
	service, ok := c.Svc.(interface {
		WorkerExecutions(context.Context, domain.SessionID, int64, int) (sessionsvc.WorkerExecutionPage, error)
	})
	if !ok {
		apispec.NotImplemented(w, r, "GET", "/api/v1/sessions/{sessionId}/worker-executions")
		return
	}
	cursor, limit := int64(0), 20
	var err error
	if value := r.URL.Query().Get("cursor"); value != "" {
		cursor, err = strconv.ParseInt(value, 10, 64)
	}
	if value := r.URL.Query().Get("limit"); err == nil && value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err != nil || cursor < 0 || limit < 1 || limit > 100 {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_WORKER_HISTORY_PAGE", "Cursor must be non-negative and limit between 1 and 100", nil))
		return
	}
	page, err := service.WorkerExecutions(r.Context(), sessionID(r), cursor, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, WorkerExecutionHistoryResponse(page))
}

func (c *SessionsController) workerExecution(w http.ResponseWriter, r *http.Request) {
	service, ok := c.Svc.(interface {
		WorkerExecution(context.Context, domain.SessionID, string) (domain.WorkerExecution, error)
	})
	if !ok {
		apispec.NotImplemented(w, r, "GET", "/api/v1/sessions/{sessionId}/worker-executions/{executionId}")
		return
	}
	execution, err := service.WorkerExecution(r.Context(), sessionID(r), chi.URLParam(r, "executionId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, WorkerExecutionResponse{Execution: execution})
}
