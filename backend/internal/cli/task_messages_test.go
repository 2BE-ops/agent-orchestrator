package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskMessageCLIUsageBoundsAndDaemonError(t *testing.T) {
	for _, args := range [][]string{
		{"task", "messages"}, {"task", "messages", ""}, {"task", "messages", "project", "--task", ""},
		{"task", "messages", "project", "--limit", "101"}, {"task", "messages", "project", "--cursor", "-1"},
		{"task", "message", "project"}, {"task", "message", "project", ""},
		{"task", "send-message", "worker"}, {"task", "send-message", "", "--file", "-"},
	} {
		if _, _, err := executeCLI(t, aliveDeps(), args...); ExitCode(err) != 2 {
			t.Fatalf("usage %v: %v", args, err)
		}
	}
	deps := aliveDeps()
	deps.In = strings.NewReader(`{"definition":"` + strings.Repeat("x", 64<<10) + `"}`)
	if _, _, err := executeCLI(t, deps, "task", "send-message", "worker", "--file", "-"); ExitCode(err) != 2 {
		t.Fatalf("unbounded JSON: %v", err)
	}
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"code":"TASK_MESSAGE_OWNER_CHANGED","message":"Reconcile ownership","requestId":"message-request"}`)
	writeRunFileFor(t, cfg, srv)
	deps.In = strings.NewReader(`{"sourceGeneration":"old"}`)
	_, _, err := executeCLI(t, deps, "task", "send-message", "worker", "--file", "-")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "TASK_MESSAGE_OWNER_CHANGED") || !strings.Contains(err.Error(), "message-request") {
		t.Fatalf("lost envelope: %v", err)
	}
}

func TestTaskMessageCLIPreservesTimelineFilters(t *testing.T) {
	cfg := setConfigEnv(t)
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	if _, _, err := executeCLI(t, aliveDeps(), "task", "messages", "project", "--task", "task 1", "--cursor", "19", "--limit", "7"); err != nil {
		t.Fatal(err)
	}
	if query != "cursor=19&limit=7&taskId=task+1" {
		t.Fatalf("lost query: %s", query)
	}
}
