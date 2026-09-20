package controllers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	controlsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/control"
)

// ControlService is the deterministic project-control boundary shared with
// the CLI. Actors are established by the application, never by a JSON field:
// HTTP control changes are user-authored.
type ControlService interface {
	Control(context.Context, domain.ProjectID) (domain.ProjectControlView, error)
	SetControl(context.Context, domain.AdaptiveActor, domain.ProjectID, controlsvc.StateInput) (domain.ProjectControlView, error)
	CancelWork(context.Context, domain.AdaptiveActor, domain.ProjectID, controlsvc.CancelInput) (controlsvc.CancelResult, error)
	Raise(context.Context, domain.AdaptiveActor, string, controlsvc.NeedsHumanInput) (domain.TaskNeedsHuman, error)
	Resolve(context.Context, domain.AdaptiveActor, string, controlsvc.ResolveInput) (domain.TaskNeedsHuman, error)
	List(context.Context, domain.ProjectID, string, int) ([]domain.TaskNeedsHuman, error)
	DryRun(context.Context, domain.ProjectID, domain.DryRunRequest) (domain.DryRunVerdict, error)
}

// ControlController mounts the project control, Needs Human and dry-run API.
// Controls are deterministic durable state: handlers only validate input and
// attribute actors; every fence lives in the storage transactions.
type ControlController struct{ Svc ControlService }

// Register exposes project-scoped controls and task-scoped human requests.
func (c *ControlController) Register(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(c.available)
		r.Get("/projects/{id}/control", c.getControl)
		r.Post("/projects/{id}/control", c.setControl)
		r.Post("/projects/{id}/control/cancel-work", c.cancelWork)
		r.Get("/projects/{id}/needs-human", c.listNeedsHuman)
		r.Post("/projects/{id}/dry-run", c.dryRun)
		r.Post("/tasks/{taskId}/needs-human", c.raiseNeedsHuman)
		r.Post("/tasks/{taskId}/needs-human/resolve", c.resolveNeedsHuman)
	})
}

func (c *ControlController) available(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.Svc == nil {
			envelope.WriteError(w, r, apierr.NotImplemented("CONTROL_UNAVAILABLE", "Project controls are not available"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func controlHumanActor() domain.AdaptiveActor {
	return domain.AdaptiveActor{Kind: "USER", ID: "local-user"}
}

func (c *ControlController) getControl(w http.ResponseWriter, r *http.Request) {
	view, err := c.Svc.Control(r.Context(), domain.ProjectID(chi.URLParam(r, "id")))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ProjectControlResponse{View: view})
}

func (c *ControlController) setControl(w http.ResponseWriter, r *http.Request) {
	var input controlsvc.StateInput
	if !decodeControlBody(w, r, &input, "INVALID_CONTROL_JSON", "control change") {
		return
	}
	view, err := c.Svc.SetControl(r.Context(), controlHumanActor(), domain.ProjectID(chi.URLParam(r, "id")), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ProjectControlResponse{View: view})
}

func (c *ControlController) cancelWork(w http.ResponseWriter, r *http.Request) {
	var input controlsvc.CancelInput
	if !decodeControlBody(w, r, &input, "INVALID_CONTROL_JSON", "work cancellation") {
		return
	}
	result, err := c.Svc.CancelWork(r.Context(), controlHumanActor(), domain.ProjectID(chi.URLParam(r, "id")), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ProjectCancelWorkResponse{Cancel: result})
}

func (c *ControlController) listNeedsHuman(w http.ResponseWriter, r *http.Request) {
	afterID, limit, ok := parseControlPage(w, r)
	if !ok {
		return
	}
	items, err := c.Svc.List(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), afterID, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := ProjectNeedsHumanResponse{Items: items}
	if len(items) == limit {
		response.NextAfterID = items[len(items)-1].ID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *ControlController) raiseNeedsHuman(w http.ResponseWriter, r *http.Request) {
	var input controlsvc.NeedsHumanInput
	if !decodeControlBody(w, r, &input, "INVALID_NEEDS_HUMAN_JSON", "needs human raise") {
		return
	}
	item, err := c.Svc.Raise(r.Context(), controlHumanActor(), chi.URLParam(r, "taskId"), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, TaskNeedsHumanResponse{NeedsHuman: item})
}

func (c *ControlController) resolveNeedsHuman(w http.ResponseWriter, r *http.Request) {
	var input controlsvc.ResolveInput
	if !decodeControlBody(w, r, &input, "INVALID_NEEDS_HUMAN_JSON", "needs human resolution") {
		return
	}
	if input.Resolution == "" {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_NEEDS_HUMAN_INPUT", "A resolution is required", nil))
		return
	}
	item, err := c.Svc.Resolve(r.Context(), controlHumanActor(), chi.URLParam(r, "taskId"), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, TaskNeedsHumanResponse{NeedsHuman: item})
}

func (c *ControlController) dryRun(w http.ResponseWriter, r *http.Request) {
	var request domain.DryRunRequest
	if !decodeControlBody(w, r, &request, "INVALID_DRY_RUN_JSON", "dry run") {
		return
	}
	verdict, err := c.Svc.DryRun(r.Context(), domain.ProjectID(chi.URLParam(r, "id")), request)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, DryRunResponse{Verdict: verdict})
}

// decodeControlBody mirrors the shared bounded JSON decoding with the
// control-surface error envelopes.
func decodeControlBody(w http.ResponseWriter, r *http.Request, target any, code, what string) bool {
	return decodeTaskOutput(w, r, target, 288<<10, code, what)
}

// parseControlPage reads the strict id-keyset page for human requests.
func parseControlPage(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	return parseStrictIDPage(w, r, apierr.Invalid("INVALID_NEEDS_HUMAN_PAGE", "Use one bounded afterId and a limit from 1 to 100", nil))
}
