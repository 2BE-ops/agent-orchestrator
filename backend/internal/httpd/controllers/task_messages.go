package controllers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

func (c *AdaptiveTasksController) submitMessage(w http.ResponseWriter, r *http.Request) {
	var input TaskMessageSubmitRequest
	if !decodeTaskOutput(w, r, &input, 64<<10, "INVALID_MESSAGE_JSON", "message") {
		return
	}
	receipt, err := c.Svc.SubmitMessage(r.Context(), sessionID(r), tasksvc.MessageInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, TaskMessageSubmitResponse(receipt))
}

func (c *AdaptiveTasksController) messages(w http.ResponseWriter, r *http.Request) {
	after, limit, err := taskPage(r, true)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Messages(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), r.URL.Query().Get("taskId"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := TaskMessagesResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Sequence, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AdaptiveTasksController) message(w http.ResponseWriter, r *http.Request) {
	view, err := c.Svc.Message(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), chi.URLParam(r, "messageId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, TaskMessageResponse(view))
}
