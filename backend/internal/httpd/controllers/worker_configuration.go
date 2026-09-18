package controllers

import (
	"context"
	"net/http"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
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
