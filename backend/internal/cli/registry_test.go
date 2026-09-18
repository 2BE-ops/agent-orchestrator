package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestRegistryCLIHTTPPaths(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		method, path string
	}{
		{[]string{"agent-type", "list"}, http.MethodGet, "/api/v1/agent-types"},
		{[]string{"skill", "show", "s1"}, http.MethodGet, "/api/v1/skills/s1"},
		{[]string{"agent-types", "versions", "a1"}, http.MethodGet, "/api/v1/agent-types/a1/versions"},
		{[]string{"skills", "audit", "s1"}, http.MethodGet, "/api/v1/skills/s1/audit"},
		{[]string{"agent-type", "create", "--file", "-"}, http.MethodPost, "/api/v1/agent-types"},
		{[]string{"skill", "new-version", "s1", "--file", "-"}, http.MethodPost, "/api/v1/skills/s1/versions"},
		{[]string{"agent-type", "update", "a1", "--file", "-"}, http.MethodPatch, "/api/v1/agent-types/a1"},
		{[]string{"agent-type", "activate", "a1", "--file", "-"}, http.MethodPost, "/api/v1/agent-types/a1/activate"},
		{[]string{"skill", "clone", "s1", "--file", "-"}, http.MethodPost, "/api/v1/skills/s1/clone"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv, captured := reviewServer(t, http.StatusOK, `{"ok":true}`)
			writeRunFileFor(t, cfg, srv)
			deps := aliveDeps()
			deps.In = strings.NewReader(`{"reason":"explicit action"}`)
			out, _, err := executeCLI(t, deps, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if captured.method != tc.method || captured.path != tc.path || !strings.Contains(out, `"ok": true`) {
				t.Fatalf("request: %+v; output %s", captured, out)
			}
		})
	}
}

func TestRegistryCLIUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"agent-type", "show"}, {"skill", "create"}, {"skill", "list", "--limit", "201"}, {"agent-type", "activate", ""}, {"skill", "create", "--file", "-"}} {
		deps := aliveDeps()
		deps.In = strings.NewReader(`[]`)
		_, _, err := executeCLI(t, deps, args...)
		if ExitCode(err) != 2 {
			t.Fatalf("%v: expected usage error, got %v", args, err)
		}
	}
}

func TestRegistryCLIPreservesDaemonError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"message":"Reload before saving","code":"REGISTRY_REVISION_CONFLICT","requestId":"reg-request"}`)
	writeRunFileFor(t, cfg, srv)
	_, _, err := executeCLI(t, aliveDeps(), "agent-type", "list")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "REGISTRY_REVISION_CONFLICT") || !strings.Contains(err.Error(), "reg-request") {
		t.Fatalf("lost daemon error: %v", err)
	}
}
