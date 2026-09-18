package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestTaskCLIHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		method, path string
	}{
		{[]string{"task", "list", "project"}, http.MethodGet, "/api/v1/projects/project/tasks"},
		{[]string{"task", "show", "task-1"}, http.MethodGet, "/api/v1/tasks/task-1"},
		{[]string{"tasks", "revisions", "task-1"}, http.MethodGet, "/api/v1/tasks/task-1/revisions"},
		{[]string{"task", "revision", "task-1", "2"}, http.MethodGet, "/api/v1/tasks/task-1/revisions/2"},
		{[]string{"task", "criteria", "task-1", "1"}, http.MethodGet, "/api/v1/tasks/task-1/criteria/1"},
		{[]string{"task", "audit", "task-1"}, http.MethodGet, "/api/v1/tasks/task-1/audit"},
		{[]string{"task", "attempts", "task-1"}, http.MethodGet, "/api/v1/tasks/task-1/attempts"},
		{[]string{"task", "context", "task-1", "attempt-1"}, http.MethodGet, "/api/v1/tasks/task-1/attempts/attempt-1/context"},
		{[]string{"task", "results", "task-1", "attempt-1"}, http.MethodGet, "/api/v1/tasks/task-1/attempts/attempt-1/results"},
		{[]string{"task", "result", "task-1", "attempt-1", "result-1"}, http.MethodGet, "/api/v1/tasks/task-1/attempts/attempt-1/results/result-1"},
		{[]string{"task", "submit-result", "worker-1", "--file", "-"}, http.MethodPost, "/api/v1/sessions/worker-1/task-results"},
		{[]string{"task", "intents", "task-1"}, http.MethodGet, "/api/v1/tasks/task-1/intents"},
		{[]string{"task", "set-intent", "task-1", "--file", "-"}, http.MethodPost, "/api/v1/tasks/task-1/intents"},
		{[]string{"task", "create", "project", "--file", "-"}, http.MethodPost, "/api/v1/projects/project/tasks"},
		{[]string{"task", "revise", "task-1", "--file", "-"}, http.MethodPost, "/api/v1/tasks/task-1/revisions"},
		{[]string{"task", "set-criteria", "task-1", "--file", "-"}, http.MethodPost, "/api/v1/tasks/task-1/criteria"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv, captured := reviewServer(t, http.StatusOK, `{"ok":true}`)
			writeRunFileFor(t, cfg, srv)
			deps := aliveDeps()
			deps.In = strings.NewReader(`{"reason":"Plan requested work"}`)
			out, _, err := executeCLI(t, deps, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if captured.method != tc.method || captured.path != tc.path || !strings.Contains(out, `"ok": true`) {
				t.Fatalf("request: %+v output: %s", captured, out)
			}
		})
	}
}

func TestTaskCLIUsageAndBoundedJSON(t *testing.T) {
	for _, args := range [][]string{{"task", "list"}, {"task", "show", ""}, {"task", "list", "project", "--limit", "101"}, {"task", "audit", "task", "--cursor", "invalid"}, {"task", "revision", "task", "0"}, {"task", "create", "project"}, {"task", "create", "project", "--file", "-"}} {
		deps := aliveDeps()
		deps.In = strings.NewReader(`[]`)
		_, _, err := executeCLI(t, deps, args...)
		if ExitCode(err) != 2 {
			t.Fatalf("%v: expected usage error: %v", args, err)
		}
	}
	deps := aliveDeps()
	deps.In = strings.NewReader(`{"brief":"` + strings.Repeat("x", 256<<10) + `"}`)
	_, _, err := executeCLI(t, deps, "task", "create", "project", "--file", "-")
	if ExitCode(err) != 2 {
		t.Fatalf("oversized input accepted: %v", err)
	}
}

func TestTaskCLIPreservesDaemonError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"message":"Reload before revising","code":"TASK_REVISION_CONFLICT","requestId":"task-request"}`)
	writeRunFileFor(t, cfg, srv)
	_, _, err := executeCLI(t, aliveDeps(), "task", "show", "task")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "TASK_REVISION_CONFLICT") || !strings.Contains(err.Error(), "task-request") {
		t.Fatalf("lost daemon envelope: %v", err)
	}
}

func TestTaskContextCLIUsageAndDaemonError(t *testing.T) {
	for _, args := range [][]string{{"task", "context"}, {"task", "context", "task"}, {"task", "context", "", "attempt"}, {"task", "context", "task", ""}} {
		if _, _, err := executeCLI(t, aliveDeps(), args...); ExitCode(err) != 2 {
			t.Fatalf("missing context identity: %v %v", args, err)
		}
	}
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusNotFound, `{"message":"No sealed context","code":"TASK_CONTEXT_NOT_FOUND","requestId":"context-request"}`)
	writeRunFileFor(t, cfg, srv)
	_, _, err := executeCLI(t, aliveDeps(), "task", "context", "task", "attempt")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "TASK_CONTEXT_NOT_FOUND") || !strings.Contains(err.Error(), "context-request") {
		t.Fatalf("lost context error: %v", err)
	}
}
