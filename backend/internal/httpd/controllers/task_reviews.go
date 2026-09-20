package controllers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

func (c *AdaptiveTasksController) requestReview(w http.ResponseWriter, r *http.Request) {
	var input TaskReviewRequest
	if !decodeTaskOutput(w, r, &input, 8<<10, "INVALID_REVIEW_JSON", "review request") {
		return
	}
	receipt, err := c.Svc.RequestReview(r.Context(), taskHumanActor(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"), tasksvc.ReviewInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, receipt)
}

func (c *AdaptiveTasksController) taskReviews(w http.ResponseWriter, r *http.Request) {
	runs, err := c.Svc.Reviews(r.Context(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"), chi.URLParam(r, "resultId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, TaskReviewsResponse{Items: runs})
}

func (c *AdaptiveTasksController) taskReview(w http.ResponseWriter, r *http.Request) {
	view, err := c.Svc.Review(r.Context(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"), chi.URLParam(r, "runId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, TaskReviewResponse(view))
}
