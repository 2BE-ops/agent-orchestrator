package controllers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	orchestratorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/orchestrator"
)

// OrchestratorGoalService is the controller-facing goal/planning contract.
// Public reads and user goal authoring share it with the native surface; the
// service derives native attribution from durable session facts itself.
type OrchestratorGoalService interface {
	SetGoal(ctx context.Context, actor domain.AdaptiveActor, project domain.ProjectID, input orchestratorsvc.GoalInput) (domain.ProjectGoalVersion, error)
	Goal(ctx context.Context, project domain.ProjectID) (domain.ProjectGoalVersion, error)
	GoalVersions(ctx context.Context, project domain.ProjectID, after int64, limit int) ([]domain.ProjectGoalVersion, error)
	Completions(ctx context.Context, project domain.ProjectID, afterID string, limit int) ([]domain.ProjectGoalCompletion, error)
	NativeGoal(ctx context.Context, project domain.ProjectID) (orchestratorsvc.NativeGoal, error)
	Plan(ctx context.Context, project domain.ProjectID, input orchestratorsvc.PlanInput) (orchestratorsvc.PlanReceipt, error)
	Complete(ctx context.Context, project domain.ProjectID, input orchestratorsvc.CompleteInput) (domain.ProjectGoalCompletion, bool, error)
	Feedback(ctx context.Context, project domain.ProjectID, afterTaskID string, limit int) ([]domain.ProjectFeedbackItem, error)
	Receipts(ctx context.Context, project domain.ProjectID, afterID string, limit int) ([]domain.OrchestratorPlanReceipt, error)
	Receipt(ctx context.Context, project domain.ProjectID, id string) (domain.OrchestratorPlanReceipt, error)
}

// OrchestratorController owns the durable project goal and native planning API.
type OrchestratorController struct{ Svc OrchestratorGoalService }

// Register mounts goal authoring, native planning and loop feedback routes.
func (c *OrchestratorController) Register(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(c.available)
		r.Get("/projects/{id}/goal", c.getGoal)
		r.Post("/projects/{id}/goal", c.setGoal)
		r.Get("/projects/{id}/goal/versions", c.goalVersions)
		r.Get("/projects/{id}/goal/completions", c.completions)
		r.Get("/projects/{id}/orchestrator/goal", c.nativeGoal)
		r.Post("/projects/{id}/orchestrator/plan", c.plan)
		r.Post("/projects/{id}/orchestrator/complete", c.complete)
		r.Get("/projects/{id}/orchestrator/feedback", c.feedback)
		r.Get("/projects/{id}/orchestrator/receipts", c.receipts)
		r.Get("/projects/{id}/orchestrator/receipts/{receiptId}", c.receipt)
	})
}

func (c *OrchestratorController) available(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.Svc == nil {
			envelope.WriteError(w, r, apierr.NotImplemented("ORCHESTRATOR_GOALS_UNAVAILABLE", "Orchestrator goal service is unavailable"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (c *OrchestratorController) getGoal(w http.ResponseWriter, r *http.Request) {
	goal, err := c.Svc.Goal(r.Context(), goalProjectID(r))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ProjectGoalResponse{Goal: goal})
}

func (c *OrchestratorController) setGoal(w http.ResponseWriter, r *http.Request) {
	var input SetProjectGoalRequest
	if !decodeTaskOutput(w, r, &input, 64<<10, "INVALID_GOAL_JSON", "project-goal") {
		return
	}
	goal, err := c.Svc.SetGoal(r.Context(), taskHumanActor(), goalProjectID(r), orchestratorsvc.GoalInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, ProjectGoalResponse{Goal: goal})
}

func (c *OrchestratorController) goalVersions(w http.ResponseWriter, r *http.Request) {
	after, limit, ok := parseNumberPage(w, r)
	if !ok {
		return
	}
	items, err := c.Svc.GoalVersions(r.Context(), goalProjectID(r), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := ProjectGoalVersionsResponse{Items: items}
	if len(items) == limit {
		response.NextAfter = items[len(items)-1].Number
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *OrchestratorController) completions(w http.ResponseWriter, r *http.Request) {
	afterID, limit, ok := parseIDPage(w, r)
	if !ok {
		return
	}
	items, err := c.Svc.Completions(r.Context(), goalProjectID(r), afterID, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := ProjectGoalCompletionsResponse{Items: items}
	if len(items) == limit {
		response.NextAfterID = items[len(items)-1].ID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *OrchestratorController) nativeGoal(w http.ResponseWriter, r *http.Request) {
	goal, err := c.Svc.NativeGoal(r.Context(), goalProjectID(r))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, NativeOrchestratorGoalResponse{Goal: goal.Goal, SourceGeneration: goal.SourceGeneration, SessionID: goal.SessionID})
}

func (c *OrchestratorController) plan(w http.ResponseWriter, r *http.Request) {
	var input OrchestratorPlanRequest
	if !decodeTaskOutput(w, r, &input, 128<<10, "INVALID_ORCHESTRATOR_JSON", "orchestrator-plan") {
		return
	}
	receipt, err := c.Svc.Plan(r.Context(), goalProjectID(r), orchestratorsvc.PlanInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, OrchestratorPlanResponse(receipt))
}

func (c *OrchestratorController) complete(w http.ResponseWriter, r *http.Request) {
	var input OrchestratorCompleteRequest
	if !decodeTaskOutput(w, r, &input, 32<<10, "INVALID_ORCHESTRATOR_JSON", "orchestrator-complete") {
		return
	}
	completion, created, err := c.Svc.Complete(r.Context(), goalProjectID(r), orchestratorsvc.CompleteInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	envelope.WriteJSON(w, status, ProjectGoalCompletionResponse{Completion: completion, Created: created})
}

func (c *OrchestratorController) feedback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	invalid := apierr.Invalid("INVALID_FEEDBACK_PAGE", "Use one bounded after task id and a limit from 1 to 100", nil)
	for key, values := range query {
		if (key != "after" && key != "limit") || len(values) != 1 {
			envelope.WriteError(w, r, invalid)
			return
		}
	}
	after := query.Get("after")
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			envelope.WriteError(w, r, invalid)
			return
		}
	}
	items, err := c.Svc.Feedback(r.Context(), goalProjectID(r), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := ProjectFeedbackResponse{Items: items}
	if len(items) == limit {
		response.NextAfter = items[len(items)-1].TaskID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *OrchestratorController) receipts(w http.ResponseWriter, r *http.Request) {
	afterID, limit, ok := parseIDPage(w, r)
	if !ok {
		return
	}
	items, err := c.Svc.Receipts(r.Context(), goalProjectID(r), afterID, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := OrchestratorPlanReceiptsResponse{Items: items}
	if len(items) == limit {
		response.NextAfterID = items[len(items)-1].Outcome.ReceiptID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *OrchestratorController) receipt(w http.ResponseWriter, r *http.Request) {
	item, err := c.Svc.Receipt(r.Context(), goalProjectID(r), chi.URLParam(r, "receiptId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, OrchestratorPlanReceiptResponse{Receipt: item})
}

// goalProjectID reads the shared {id} project path parameter.
func goalProjectID(r *http.Request) domain.ProjectID {
	return domain.ProjectID(chi.URLParam(r, "id"))
}

// parseNumberPage reads a strict numeric-version page: only after and limit
// are accepted, both bounded, so callers cannot smuggle extra query intent.
func parseNumberPage(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	invalid := apierr.Invalid("INVALID_GOAL_PAGE", "Use one non-negative after and a limit from 1 to 100", nil)
	query := r.URL.Query()
	for key, values := range query {
		if (key != "after" && key != "limit") || len(values) != 1 {
			envelope.WriteError(w, r, invalid)
			return 0, 0, false
		}
	}
	var after int64
	if raw := query.Get("after"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			envelope.WriteError(w, r, invalid)
			return 0, 0, false
		}
		after = value
	}
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			envelope.WriteError(w, r, invalid)
			return 0, 0, false
		}
		limit = value
	}
	return after, limit, true
}

// parseIDPage reads a strict id-keyset page shared by receipts and completions.
func parseIDPage(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	invalid := apierr.Invalid("INVALID_ORCHESTRATOR_PAGE", "Use one bounded afterId and a limit from 1 to 100", nil)
	query := r.URL.Query()
	for key, values := range query {
		if (key != "afterId" && key != "limit") || len(values) != 1 || values[0] == "" && key == "limit" {
			envelope.WriteError(w, r, invalid)
			return "", 0, false
		}
	}
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			envelope.WriteError(w, r, invalid)
			return "", 0, false
		}
		limit = value
	}
	return query.Get("afterId"), limit, true
}
