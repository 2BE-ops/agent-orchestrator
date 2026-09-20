package controllers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
)

func performanceWindow(r *http.Request, summary bool) (time.Time, time.Time, error) {
	query := r.URL.Query()
	for key, values := range query {
		allowed := key == "from" || key == "to" || (summary && key == "groupBy") || (!summary && (key == "cursor" || key == "limit"))
		if !allowed || len(values) != 1 {
			return time.Time{}, time.Time{}, apierr.Invalid("INVALID_PERFORMANCE_QUERY", "Unsupported or repeated performance parameter", nil)
		}
	}
	from, err := time.Parse(time.RFC3339Nano, query.Get("from"))
	if err != nil {
		return time.Time{}, time.Time{}, apierr.Invalid("INVALID_PERFORMANCE_QUERY", "from must be an RFC3339 admission timestamp", nil)
	}
	to, err := time.Parse(time.RFC3339Nano, query.Get("to"))
	if err != nil {
		return time.Time{}, time.Time{}, apierr.Invalid("INVALID_PERFORMANCE_QUERY", "to must be an RFC3339 admission timestamp", nil)
	}
	return from, to, nil
}

func (c *AdaptiveTasksController) performance(w http.ResponseWriter, r *http.Request) {
	from, to, err := performanceWindow(r, false)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			envelope.WriteError(w, r, apierr.Invalid("INVALID_PERFORMANCE_QUERY", "limit must be 1 to 100", nil))
			return
		}
	}
	page, err := c.Svc.Performance(r.Context(), domain.TaskPerformanceQuery{ProjectID: domain.ProjectID(chi.URLParam(r, "id")), From: from, To: to, After: r.URL.Query().Get("cursor"), Limit: limit})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, page)
}

func (c *AdaptiveTasksController) performanceSummary(w http.ResponseWriter, r *http.Request) {
	from, to, err := performanceWindow(r, true)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	group := r.URL.Query().Get("groupBy")
	if group == "" {
		group = "agent_type_version"
	}
	summary, err := c.Svc.PerformanceSummary(r.Context(), domain.TaskPerformanceSummaryQuery{ProjectID: domain.ProjectID(chi.URLParam(r, "id")), From: from, To: to, GroupBy: group})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, summary)
}
