package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentManagerCLIControllerCommandsUseHTTPAndKeepRetryIdentity(t *testing.T) {
	cfg := setConfigEnv(t)
	const body = `{"id":"stable-admission","configurationVersion":1,"reason":"Start native routing"}`
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			got, err := io.ReadAll(r.Body)
			if err != nil || string(got) != body {
				t.Errorf("start identity changed: %s %v", got, err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":{"controller":{"id":"stable-admission"}},"created":false}`)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	deps := aliveDeps()
	deps.In = strings.NewReader(body)
	for _, args := range [][]string{{"agent-manager", "start", "private project", "--file", "-"}, {"agent-manager", "current", "private project"}, {"agent-manager", "controller", "private project", "stable admission"}} {
		out, _, err := executeCLI(t, deps, args...)
		if err != nil || !strings.Contains(out, "stable-admission") {
			t.Fatalf("controller response lost: %s %v", out, err)
		}
	}
	want := []string{"POST /api/v1/projects/private project/agent-manager/controllers", "GET /api/v1/projects/private project/agent-manager/controller", "GET /api/v1/projects/private project/agent-manager/controllers/stable admission"}
	if len(paths) != len(want) {
		t.Fatalf("unexpected HTTP calls: %v", paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("path: %s want %s", paths[i], want[i])
		}
	}
	for _, args := range [][]string{{"agent-manager", "start"}, {"agent-manager", "start", "project"}, {"agent-manager", "controller", "project", "bad\nidentity"}} {
		if _, _, err := executeCLI(t, deps, args...); ExitCode(err) != 2 {
			t.Fatalf("invalid command: %v", err)
		}
	}
	deps.In = strings.NewReader(`{"reason":"` + strings.Repeat("x", 16<<10) + `"}`)
	if _, _, err := executeCLI(t, deps, "agent-manager", "start", "project", "--file", "-"); ExitCode(err) != 2 {
		t.Fatalf("oversized start: %v", err)
	}
}

func TestAgentManagerCLIControllerPreservesDaemonFailure(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"message":"Inspect retained native ownership","code":"AGENT_MANAGER_CONTROLLER_FENCED","requestId":"controller-error"}`)
	writeRunFileFor(t, cfg, srv)
	deps := aliveDeps()
	deps.In = strings.NewReader(`{"id":"admission","configurationVersion":1,"reason":"Start"}`)
	_, _, err := executeCLI(t, deps, "agent-manager", "start", "project", "--file", "-")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "AGENT_MANAGER_CONTROLLER_FENCED") || !strings.Contains(err.Error(), "controller-error") {
		t.Fatalf("controller error envelope lost: %v", err)
	}
}
