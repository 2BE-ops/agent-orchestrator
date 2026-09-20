package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestTaskResultCLIUsageBoundsAndErrorEnvelope(t *testing.T) {
	for _, args := range [][]string{
		{"task", "results", "task"}, {"task", "result", "task", "attempt"}, {"task", "results", "task", ""},
		{"task", "results", "task", "attempt", "--limit", "101"}, {"task", "results", "task", "attempt", "--cursor", "-1"},
		{"task", "submit-result", "worker"}, {"task", "submit-result", "", "--file", "-"},
	} {
		if _, _, err := executeCLI(t, aliveDeps(), args...); ExitCode(err) != 2 {
			t.Fatalf("usage %v: %v", args, err)
		}
	}
	deps := aliveDeps()
	deps.In = strings.NewReader(`{"definition":"` + strings.Repeat("x", 512<<10) + `"}`)
	if _, _, err := executeCLI(t, deps, "task", "submit-result", "worker", "--file", "-"); ExitCode(err) != 2 {
		t.Fatalf("unbounded result JSON: %v", err)
	}
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"code":"TASK_RESULT_OWNER_CHANGED","message":"Reconcile ownership","requestId":"result-request"}`)
	writeRunFileFor(t, cfg, srv)
	deps.In = strings.NewReader(`{"sourceGeneration":"old"}`)
	_, _, err := executeCLI(t, deps, "task", "submit-result", "worker", "--file", "-")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "TASK_RESULT_OWNER_CHANGED") || !strings.Contains(err.Error(), "result-request") {
		t.Fatalf("lost daemon result envelope: %v", err)
	}
}
