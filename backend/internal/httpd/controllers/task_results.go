package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

func (c *AdaptiveTasksController) submitResult(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input TaskResultSubmitRequest
	if err := decoder.Decode(&input); err != nil {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_RESULT_JSON", "Expected supported worker-result fields (maximum request 512 KiB)", nil))
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_RESULT_JSON", "Expected exactly one worker-result object", nil))
		return
	}
	receipt, err := c.Svc.SubmitResult(r.Context(), sessionID(r), tasksvc.ResultInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, TaskResultSubmitResponse(receipt))
}

func (c *AdaptiveTasksController) results(w http.ResponseWriter, r *http.Request) {
	after, limit, err := taskPage(r, true)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Results(r.Context(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := TaskResultsResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Number, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AdaptiveTasksController) result(w http.ResponseWriter, r *http.Request) {
	result, err := c.Svc.Result(r.Context(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"), chi.URLParam(r, "resultId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}
