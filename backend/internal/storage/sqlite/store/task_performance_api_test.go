package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

const performanceWindowQuery = "?from=2026-01-01T00:00:00Z&to=2027-01-01T00:00:00Z"

func TestTaskPerformanceAPIExposesCompleteCohortsAndRetainedEvidence(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, result := taskEvaluationFixture(t, s, evaluationCriteria())
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	_, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "assessed", 0))
	mustNoError(t, err)
	seedProject(t, s, "empty")
	router := chi.NewRouter()
	(&controllers.AdaptiveTasksController{Svc: tasksvc.New(s)}).Register(router)
	get := func(path string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Fatalf("%s = %d: %s", path, w.Code, w.Body.String())
		}
		return w
	}
	base := "/projects/project/task-performance"
	w := get(base+performanceWindowQuery+"&limit=1", http.StatusOK)
	var page domain.TaskPerformancePage
	mustNoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	if len(page.Items) != 1 || page.NextCursor == "" || page.Items[0].Usage.InputTokens != nil || !strings.Contains(w.Body.String(), `"inputTokens":null`) {
		t.Fatalf("missing evidence/null/cursor: %s", w.Body.String())
	}
	w = get(base+performanceWindowQuery+"&limit=1&cursor="+page.NextCursor, http.StatusOK)
	mustNoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	if len(page.Items) != 1 || page.Items[0].ResultID != result.ID || page.Items[0].EvaluationID != "assessed" {
		t.Fatalf("lost evidence identity: %s", w.Body.String())
	}
	w = get(base+"/summary"+performanceWindowQuery, http.StatusOK)
	var summary domain.TaskPerformanceSummary
	mustNoError(t, json.Unmarshal(w.Body.Bytes(), &summary))
	if summary.Total.Attempts != 2 || summary.Total.AssessedPassed != 1 || summary.Total.RetryAttempts != 1 || summary.Total.FirstPassCompleted != 0 || summary.GroupBy != "agent_type_version" || len(summary.Groups) != 1 || summary.Groups[0].Metrics.Attempts != 2 {
		t.Fatalf("incorrect denominators: %+v", summary)
	}
	for _, dimension := range []string{"agent_type", "skill", "skill_version", "harness", "model", "category", "capability"} {
		get(base+"/summary"+performanceWindowQuery+"&groupBy="+dimension, http.StatusOK)
	}
	w = get("/projects/empty/task-performance/summary"+performanceWindowQuery, http.StatusOK)
	summary = domain.TaskPerformanceSummary{}
	mustNoError(t, json.Unmarshal(w.Body.Bytes(), &summary))
	if summary.Total.Attempts != 0 || len(summary.Groups) != 0 || summary.ObservedAt.IsZero() {
		t.Fatalf("empty cohort invented data: %+v", summary)
	}
	get("/projects/missing/task-performance"+performanceWindowQuery, http.StatusNotFound)
	get("/projects/missing/task-performance/summary"+performanceWindowQuery, http.StatusNotFound)
	for _, suffix := range []string{"", "?from=bad&to=bad", performanceWindowQuery + "&limit=101", performanceWindowQuery + "&limit=bad", performanceWindowQuery + "&cursor=%0A", performanceWindowQuery + "&from=2026-01-01T00:00:00Z", performanceWindowQuery + "&taskId=ignored", performanceWindowQuery + "&groupBy=model", "?from=2027-01-01T00:00:00Z&to=2026-01-01T00:00:00Z"} {
		w = get(base+suffix, http.StatusBadRequest)
		if !strings.Contains(w.Body.String(), "INVALID_PERFORMANCE_QUERY") {
			t.Fatalf("validation envelope lost: %s", w.Body.String())
		}
	}
	for _, suffix := range []string{performanceWindowQuery + "&groupBy=intelligence", performanceWindowQuery + "&limit=1", performanceWindowQuery + "&cursor=ignored"} {
		get(base+"/summary"+suffix, http.StatusBadRequest)
	}
}

func TestTaskPerformanceSummaryRejectsOversizedCohortBeforeReadingPartialRows(t *testing.T) {
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	reserveTask(t, s, "attempt", "work")
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// Bulk fixture exceeds the report limit. No leases are fabricated for these
	// rows: rejection must occur before any partial evidence is inspected.
	_, err = db.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<1000)
INSERT INTO adaptive_task_attempts(id,task_id,task_revision,criteria_version,number,launch_intent_id,dependencies,actor,reason,created_at)
SELECT 'bulk-'||n.x,a.task_id,a.task_revision,a.criteria_version,n.x+1,'bulk-launch-'||n.x,a.dependencies,a.actor,a.reason,a.created_at
FROM n CROSS JOIN adaptive_task_attempts a WHERE a.id='attempt'`)
	mustNoError(t, err)
	q := performanceQuery()
	summary, err := s.GetTaskPerformanceSummary(context.Background(), domain.TaskPerformanceSummaryQuery{ProjectID: q.ProjectID, From: q.From, To: q.To, GroupBy: "category"})
	if !errors.Is(err, ports.ErrTaskPerformanceWindowTooLarge) || summary.Total.Attempts != 0 {
		t.Fatalf("partial cohort escaped: %+v %v", summary, err)
	}
	router := chi.NewRouter()
	(&controllers.AdaptiveTasksController{Svc: tasksvc.New(s)}).Register(router)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/projects/project/task-performance/summary"+performanceWindowQuery, nil))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "PERFORMANCE_WINDOW_TOO_LARGE") || strings.Contains(w.Body.String(), `"total"`) {
		t.Fatalf("large cohort envelope: %d %s", w.Code, w.Body.String())
	}
}
