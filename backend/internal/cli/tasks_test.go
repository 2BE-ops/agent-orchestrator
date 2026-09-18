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
