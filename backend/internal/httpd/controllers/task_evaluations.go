package controllers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

func (c *AdaptiveTasksController) evaluate(w http.ResponseWriter, r *http.Request) {
	var input TaskEvaluateRequest
	if !decodeTaskOutput(w, r, &input, 8<<10, "INVALID_EVALUATION_JSON", "evaluation request") {
		return
	}
	receipt, err := c.Svc.Evaluate(r.Context(), taskHumanActor(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"), tasksvc.EvaluationInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, TaskEvaluateResponse(receipt))
}

func (c *AdaptiveTasksController) evaluations(w http.ResponseWriter, r *http.Request) {
	after, limit, err := taskPage(r, true)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Evaluations(r.Context(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := TaskEvaluationsResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Number, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AdaptiveTasksController) evaluation(w http.ResponseWriter, r *http.Request) {
	item, err := c.Svc.Evaluation(r.Context(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"), chi.URLParam(r, "evaluationId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, item)
}
