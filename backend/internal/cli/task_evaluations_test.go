package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestTaskEvaluationCLIUsageAndDaemonError(t *testing.T) {
	for _, args := range [][]string{
		{"task", "evaluate", "task"}, {"task", "evaluate", "task", "attempt"}, {"task", "evaluate", "", "attempt", "--file", "-"},
		{"task", "evaluations", "task"}, {"task", "evaluations", "task", "attempt", "--cursor", "-1"}, {"task", "evaluations", "task", "attempt", "--limit", "101"},
		{"task", "evaluation", "task", "attempt"}, {"task", "evaluation", "task", "attempt", ""},
	} {
		if _, _, err := executeCLI(t, aliveDeps(), args...); ExitCode(err) != 2 {
			t.Fatalf("%v: expected usage error: %v", args, err)
		}
	}
	deps := aliveDeps()
	deps.In = strings.NewReader(`{"reason":"` + strings.Repeat("x", 8<<10) + `"}`)
	if _, _, err := executeCLI(t, deps, "task", "evaluate", "task", "attempt", "--file", "-"); ExitCode(err) != 2 {
		t.Fatalf("oversized evaluation accepted: %v", err)
	}
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"message":"Result changed","code":"TASK_EVALUATION_CONFLICT","requestId":"evaluation-request"}`)
	writeRunFileFor(t, cfg, srv)
	deps.In = strings.NewReader(`{"resultId":"result","expectedVersion":1,"idempotencyKey":"assessment","reason":"Collect evidence"}`)
	_, _, err := executeCLI(t, deps, "task", "evaluate", "task", "attempt", "--file", "-")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "TASK_EVALUATION_CONFLICT") || !strings.Contains(err.Error(), "evaluation-request") {
		t.Fatalf("lost daemon envelope: %v", err)
	}
}
