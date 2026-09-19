package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

// AdaptiveTaskService is shared by human authoring and trusted controller tools.
type AdaptiveTaskService interface {
	Create(context.Context, domain.AdaptiveActor, domain.ProjectID, tasksvc.CreateInput) (tasksvc.View, error)
	Get(context.Context, string) (tasksvc.View, error)
	List(context.Context, domain.ProjectID, string, int) ([]tasksvc.View, error)
	Revise(context.Context, domain.AdaptiveActor, string, tasksvc.RevisionInput) (domain.TaskRevision, error)
	ReviseCriteria(context.Context, domain.AdaptiveActor, string, tasksvc.CriteriaInput) (domain.TaskRevision, error)
	Revision(context.Context, string, int64) (domain.TaskRevision, error)
	Criteria(context.Context, string, int64) (domain.AcceptanceCriteriaVersion, error)
	Revisions(context.Context, string, int64, int) ([]domain.TaskRevision, error)
	Audit(context.Context, string, int64, int) ([]domain.TaskAudit, error)
	Attempts(context.Context, string, int64, int) ([]tasksvc.AttemptView, error)
	Context(context.Context, string, string) (domain.TaskContextSnapshot, error)
	SubmitResult(context.Context, domain.SessionID, tasksvc.ResultInput) (tasksvc.ResultReceipt, error)
	Results(context.Context, string, string, int64, int) ([]domain.TaskResult, error)
	Result(context.Context, string, string, string) (domain.TaskResult, error)
	Evaluate(context.Context, domain.AdaptiveActor, string, string, tasksvc.EvaluationInput) (tasksvc.EvaluationReceipt, error)
	Evaluations(context.Context, string, string, int64, int) ([]domain.TaskEvaluation, error)
	Evaluation(context.Context, string, string, string) (domain.TaskEvaluation, error)
	SubmitMessage(context.Context, domain.SessionID, tasksvc.MessageInput) (tasksvc.MessageReceipt, error)
	Messages(context.Context, domain.ProjectID, string, int64, int) ([]domain.TaskMessage, error)
	Message(context.Context, domain.ProjectID, string) (tasksvc.MessageView, error)
	ChangeIntent(context.Context, domain.AdaptiveActor, string, tasksvc.IntentInput) (domain.TaskIntent, error)
	Intents(context.Context, string, int64, int) ([]domain.TaskIntent, error)
}

// AdaptiveTasksController is the human task API; it grants no worker lease controls.
type AdaptiveTasksController struct{ Svc AdaptiveTaskService }

// Register mounts project authoring and task-scoped immutable history.
func (c *AdaptiveTasksController) Register(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(c.available)
		r.Get("/projects/{id}/tasks", c.list)
		r.Post("/projects/{id}/tasks", c.create)
		r.Get("/tasks/{taskId}", c.get)
		r.Get("/tasks/{taskId}/revisions", c.revisions)
		r.Post("/tasks/{taskId}/revisions", c.revise)
		r.Get("/tasks/{taskId}/revisions/{version}", c.revision)
		r.Post("/tasks/{taskId}/criteria", c.reviseCriteria)
		r.Get("/tasks/{taskId}/criteria/{version}", c.criteria)
		r.Get("/tasks/{taskId}/audit", c.audit)
		r.Get("/tasks/{taskId}/attempts", c.attempts)
		r.Get("/tasks/{taskId}/attempts/{attemptId}/context", c.context)
		r.Get("/tasks/{taskId}/attempts/{attemptId}/results", c.results)
		r.Get("/tasks/{taskId}/attempts/{attemptId}/results/{resultId}", c.result)
		r.Post("/tasks/{taskId}/attempts/{attemptId}/evaluations", c.evaluate)
		r.Get("/tasks/{taskId}/attempts/{attemptId}/evaluations", c.evaluations)
		r.Get("/tasks/{taskId}/attempts/{attemptId}/evaluations/{evaluationId}", c.evaluation)
		r.Post("/sessions/{sessionId}/task-results", c.submitResult)
		r.Post("/sessions/{sessionId}/task-messages", c.submitMessage)
		r.Get("/projects/{id}/task-messages", c.messages)
		r.Get("/projects/{id}/task-messages/{messageId}", c.message)
		r.Post("/tasks/{taskId}/intents", c.changeIntent)
		r.Get("/tasks/{taskId}/intents", c.intents)
	})
}

func (c *AdaptiveTasksController) available(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.Svc == nil {
			envelope.WriteError(w, r, apierr.NotImplemented("TASKS_UNAVAILABLE", "Task service is unavailable"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func taskHumanActor() domain.AdaptiveActor {
	return domain.AdaptiveActor{Kind: "USER", ID: "local-user"}
}

func (c *AdaptiveTasksController) create(w http.ResponseWriter, r *http.Request) {
	var input AdaptiveTaskCreateRequest
	if !decodeTaskBody(w, r, &input) {
		return
	}
	view, err := c.Svc.Create(r.Context(), taskHumanActor(), domain.ProjectID(chi.URLParam(r, "id")), tasksvc.CreateInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, AdaptiveTaskResponse(view))
}

func (c *AdaptiveTasksController) get(w http.ResponseWriter, r *http.Request) {
	view, err := c.Svc.Get(r.Context(), chi.URLParam(r, "taskId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AdaptiveTaskResponse(view))
}

func (c *AdaptiveTasksController) context(w http.ResponseWriter, r *http.Request) {
	snapshot, err := c.Svc.Context(r.Context(), chi.URLParam(r, "taskId"), chi.URLParam(r, "attemptId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, snapshot)
}

func (c *AdaptiveTasksController) list(w http.ResponseWriter, r *http.Request) {
	_, limit, err := taskPage(r, false)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.List(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := AdaptiveTaskListResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = items[len(items)-1].Task.ID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AdaptiveTasksController) revise(w http.ResponseWriter, r *http.Request) {
	var input AdaptiveTaskReviseRequest
	if !decodeTaskBody(w, r, &input) {
		return
	}
	revision, err := c.Svc.Revise(r.Context(), taskHumanActor(), chi.URLParam(r, "taskId"), tasksvc.RevisionInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, revision)
}

func (c *AdaptiveTasksController) reviseCriteria(w http.ResponseWriter, r *http.Request) {
	var input AdaptiveTaskCriteriaRequest
	if !decodeTaskBody(w, r, &input) {
		return
	}
	revision, err := c.Svc.ReviseCriteria(r.Context(), taskHumanActor(), chi.URLParam(r, "taskId"), tasksvc.CriteriaInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, revision)
}

func (c *AdaptiveTasksController) revision(w http.ResponseWriter, r *http.Request) {
	number, err := taskVersion(r)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	revision, err := c.Svc.Revision(r.Context(), chi.URLParam(r, "taskId"), number)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, revision)
}

func (c *AdaptiveTasksController) criteria(w http.ResponseWriter, r *http.Request) {
	number, err := taskVersion(r)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	criteria, err := c.Svc.Criteria(r.Context(), chi.URLParam(r, "taskId"), number)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, criteria)
}

func (c *AdaptiveTasksController) revisions(w http.ResponseWriter, r *http.Request) {
	after, limit, err := taskPage(r, true)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Revisions(r.Context(), chi.URLParam(r, "taskId"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := AdaptiveTaskRevisionsResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Number, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AdaptiveTasksController) audit(w http.ResponseWriter, r *http.Request) {
	after, limit, err := taskPage(r, true)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Audit(r.Context(), chi.URLParam(r, "taskId"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := AdaptiveTaskAuditResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Sequence, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AdaptiveTasksController) attempts(w http.ResponseWriter, r *http.Request) {
	after, limit, err := taskPage(r, true)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Attempts(r.Context(), chi.URLParam(r, "taskId"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := AdaptiveTaskAttemptsResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Attempt.Number, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AdaptiveTasksController) changeIntent(w http.ResponseWriter, r *http.Request) {
	var input AdaptiveTaskIntentRequest
	if !decodeTaskBody(w, r, &input) {
		return
	}
	intent, err := c.Svc.ChangeIntent(r.Context(), taskHumanActor(), chi.URLParam(r, "taskId"), tasksvc.IntentInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, intent)
}

func (c *AdaptiveTasksController) intents(w http.ResponseWriter, r *http.Request) {
	after, limit, err := taskPage(r, true)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Intents(r.Context(), chi.URLParam(r, "taskId"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := AdaptiveTaskIntentsResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Version, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func decodeTaskBody(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_TASK_JSON", "Expected supported task fields (maximum 256 KiB)", nil))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_TASK_JSON", "Expected exactly one JSON object", nil))
		return false
	}
	return true
}

func taskVersion(r *http.Request) (int64, error) {
	number, err := strconv.ParseInt(chi.URLParam(r, "version"), 10, 64)
	if err != nil || number < 1 {
		return 0, apierr.Invalid("INVALID_TASK_VERSION", "Version must be positive", nil)
	}
	return number, nil
}

func taskPage(r *http.Request, history bool) (int64, int, error) {
	limit := 20
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err != nil || limit < 1 || limit > 100 {
		return 0, 0, apierr.Invalid("INVALID_TASK_PAGE", "Limit must be between 1 and 100", nil)
	}
	var after int64
	if raw := r.URL.Query().Get("cursor"); history && raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
	}
	if err != nil || after < 0 {
		return 0, 0, apierr.Invalid("INVALID_TASK_PAGE", "Cursor must be a non-negative integer", nil)
	}
	return after, limit, nil
}
