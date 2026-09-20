package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestTaskReviewCLITransportAndUsage(t *testing.T) {
	for _, tc := range []struct {
		args               []string
		method, path, body string
	}{
		{[]string{"task", "request-review", "work", "attempt", "--file", "-"}, http.MethodPost, "/api/v1/tasks/work/attempts/attempt/reviews", `{"resultId":"result"}`},
		{[]string{"task", "reviews", "work", "attempt", "result"}, http.MethodGet, "/api/v1/tasks/work/attempts/attempt/results/result/reviews", ""},
		{[]string{"task", "review", "work", "attempt", "run"}, http.MethodGet, "/api/v1/tasks/work/attempts/attempt/reviews/run", ""},
	} {
		t.Run(tc.args[1], func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv, capture := reviewServer(t, http.StatusOK, `{"retained":true}`)
			writeRunFileFor(t, cfg, srv)
			deps := aliveDeps()
			deps.In = strings.NewReader(tc.body)
			out, _, err := executeCLI(t, deps, tc.args...)
			if err != nil || capture.method != tc.method || capture.path != tc.path || strings.TrimSpace(capture.body) != tc.body || !strings.Contains(out, "retained") {
				t.Fatalf("request: %+v output=%s err=%v", capture, out, err)
			}
		})
	}
	for _, args := range [][]string{
		{"task", "request-review", "task"}, {"task", "request-review", "task", "attempt"}, {"task", "request-review", "", "attempt", "--file", "-"},
		{"task", "reviews", "task", "attempt"}, {"task", "review", "task", "attempt", ""},
	} {
		if _, _, err := executeCLI(t, aliveDeps(), args...); ExitCode(err) != 2 {
			t.Fatalf("usage %v: %v", args, err)
		}
	}
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"message":"Review requires reconciliation","code":"TASK_REVIEW_RECONCILE_REQUIRED","requestId":"review-request"}`)
	writeRunFileFor(t, cfg, srv)
	deps := aliveDeps()
	deps.In = strings.NewReader(`{"resultId":"result"}`)
	_, _, err := executeCLI(t, deps, "task", "request-review", "task", "attempt", "--file", "-")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "TASK_REVIEW_RECONCILE_REQUIRED") || !strings.Contains(err.Error(), "review-request") {
		t.Fatalf("lost daemon error: %v", err)
	}
}
