package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestKnowledgeCLIUsesHTTPForAuthoringAndHistory(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		method, path string
	}{
		{[]string{"knowledge", "list", "project", "--status", "accepted", "--search", "daemon"}, http.MethodGet, "/api/v1/projects/project/knowledge"},
		{[]string{"knowledge", "show", "fact"}, http.MethodGet, "/api/v1/knowledge/fact"},
		{[]string{"knowledge", "versions", "fact", "--cursor", "1"}, http.MethodGet, "/api/v1/knowledge/fact/versions"},
		{[]string{"knowledge", "version", "fact", "2"}, http.MethodGet, "/api/v1/knowledge/fact/versions/2"},
		{[]string{"knowledge", "create", "project", "--file", "-"}, http.MethodPost, "/api/v1/projects/project/knowledge"},
		{[]string{"knowledge", "revise", "fact", "--file", "-"}, http.MethodPost, "/api/v1/knowledge/fact/versions"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv, captured := reviewServer(t, http.StatusOK, `{"ok":true}`)
			writeRunFileFor(t, cfg, srv)
			deps := aliveDeps()
			deps.In = strings.NewReader(`{"reason":"Document verified evidence"}`)
			out, _, err := executeCLI(t, deps, tc.args...)
			if err != nil || captured.method != tc.method || captured.path != tc.path || !strings.Contains(out, `"ok": true`) {
				t.Fatalf("HTTP boundary: %+v %s %v", captured, out, err)
			}
			if tc.method == http.MethodGet && captured.body != "" {
				t.Fatalf("GET sent a body: %q", captured.body)
			}
		})
	}
}

func TestKnowledgeCLIUsageBoundsAndDaemonErrors(t *testing.T) {
	for _, args := range [][]string{{"knowledge", "show"}, {"knowledge", "show", ""}, {"knowledge", "version", "fact", "0"}, {"knowledge", "versions", "fact", "--cursor", "-1"}, {"knowledge", "list", "project", "--limit", "101"}, {"knowledge", "create", "project"}} {
		if _, _, err := executeCLI(t, aliveDeps(), args...); ExitCode(err) != 2 {
			t.Fatalf("usage %v: %v", args, err)
		}
	}
	deps := aliveDeps()
	deps.In = strings.NewReader(`{"content":"` + strings.Repeat("x", 128<<10) + `"}`)
	if _, _, err := executeCLI(t, deps, "knowledge", "revise", "fact", "--file", "-"); ExitCode(err) != 2 {
		t.Fatalf("oversized payload: %v", err)
	}
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"code":"KNOWLEDGE_VERSION_CONFLICT","message":"Reload the fact","requestId":"knowledge-request"}`)
	writeRunFileFor(t, cfg, srv)
	if _, _, err := executeCLI(t, aliveDeps(), "knowledge", "show", "fact"); ExitCode(err) != 1 || !strings.Contains(err.Error(), "knowledge-request") || !strings.Contains(err.Error(), "KNOWLEDGE_VERSION_CONFLICT") {
		t.Fatalf("daemon envelope lost: %v", err)
	}
}
