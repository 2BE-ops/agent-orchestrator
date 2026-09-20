package controllers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

func (c *AdaptiveTasksController) delegations(w http.ResponseWriter, r *http.Request) {
	after, limit, err := taskPage(r, true)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Delegations(r.Context(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := TaskDelegationsResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Number, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AdaptiveTasksController) delegation(w http.ResponseWriter, r *http.Request) {
	number, err := tasksvc.ParseTaskDelegationNumber(chi.URLParam(r, "number"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	item, err := c.Svc.Delegation(r.Context(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"), number)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, item)
}
