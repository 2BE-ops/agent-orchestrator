package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

func (c *AdaptiveTasksController) submitResult(w http.ResponseWriter, r *http.Request) {
	var input TaskResultSubmitRequest
	if !decodeTaskOutput(w, r, &input, 512<<10, "INVALID_RESULT_JSON", "worker-result") {
		return
	}
	receipt, err := c.Svc.SubmitResult(r.Context(), sessionID(r), tasksvc.ResultInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, TaskResultSubmitResponse(receipt))
}

// decodeTaskOutput shares strict single-object decoding while preserving each
// worker protocol's body budget and public error code.
func decodeTaskOutput(w http.ResponseWriter, r *http.Request, target any, limit int64, code, kind string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		envelope.WriteError(w, r, apierr.Invalid(code, fmt.Sprintf("Expected supported %s fields (maximum request %d KiB)", kind, limit>>10), nil))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		envelope.WriteError(w, r, apierr.Invalid(code, "Expected exactly one "+kind+" object", nil))
		return false
	}
	return true
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
