package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentManagerCLITransport(t *testing.T) {
	for _, tc := range []struct {
		args                          []string
		method, suffix, cursor, limit string
	}{
		{[]string{"show", "client project"}, http.MethodGet, "", "", ""},
		{[]string{"configuration", "client project", "2"}, http.MethodGet, "/configurations/2", "", ""},
		{[]string{"configurations", "client project", "--cursor", "2", "--limit", "7"}, http.MethodGet, "/configurations", "2", "7"},
		{[]string{"audit", "client project"}, http.MethodGet, "/audit", "0", "20"},
		{[]string{"inbox", "client project"}, http.MethodGet, "/inbox", "0", "20"},
		{[]string{"requests", "client project", "--cursor", "2", "--limit", "7"}, http.MethodGet, "/requests", "2", "7"},
		{[]string{"request", "client project", "work item"}, http.MethodGet, "/requests/work item", "", ""},
		{[]string{"request-resolution", "client project", "work item"}, http.MethodGet, "/requests/work item/resolution", "", ""},
		{[]string{"enqueue", "client project", "--file", "-"}, http.MethodPost, "/requests", "", ""},
		{[]string{"resolve", "client project", "work item", "--file", "-"}, http.MethodPost, "/requests/work item/resolution", "", ""},
		{[]string{"configure", "client project", "--file", "-"}, http.MethodPut, "", "", ""},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			cfg := setConfigEnv(t)
			var method, path, body, cursor, limit string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method, path = r.Method, r.URL.Path
				cursor, limit = r.URL.Query().Get("cursor"), r.URL.Query().Get("limit")
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				body = string(b)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"configuration":true}`)
			}))
			t.Cleanup(srv.Close)
			writeRunFileFor(t, cfg, srv)
			deps := aliveDeps()
			const input = `{"expectedRevision":2,"reason":"Set explicit policy","definition":{"schemaVersion":1}}`
			deps.In = strings.NewReader(input)
			out, _, err := executeCLI(t, deps, append([]string{"agent-manager"}, tc.args...)...)
			if err != nil || method != tc.method || path != "/api/v1/projects/client project/agent-manager"+tc.suffix || cursor != tc.cursor || limit != tc.limit || !strings.Contains(out, "configuration") {
				t.Fatalf("transport: %s %s cursor=%s limit=%s %v", method, path, cursor, limit, err)
			}
			if (tc.method == http.MethodPut || tc.method == http.MethodPost) && body != input {
				t.Fatalf("rewrote governance body: %s", body)
			}
		})
	}
}

func TestAgentManagerCLIUsageAndDaemonFailure(t *testing.T) {
	for _, args := range [][]string{
		{"show"}, {"show", ""}, {"show", "bad\nproject"}, {"configuration", "project", "0"}, {"configuration", "project", "1001"},
		{"configurations", "project", "--cursor", "-1"}, {"audit", "project", "--limit", "101"}, {"show", "project", "--limit", "1"},
		{"configure", "project"}, {"configure", "project", "--file", "-"},
		{"inbox"}, {"inbox", "project", "--limit", "0"}, {"requests", "project", "--cursor", "-1"},
		{"request", "project"}, {"request", "project", "bad\nrequest"}, {"request-resolution", "project", ""},
		{"enqueue", "project"}, {"enqueue", "project", "--file", "-"}, {"resolve", "project", "work"}, {"resolve", "project", "work", "--file", "-"},
	} {
		deps := aliveDeps()
		deps.In = strings.NewReader(`[]`)
		if _, _, err := executeCLI(t, deps, append([]string{"agent-manager"}, args...)...); ExitCode(err) != 2 {
			t.Fatalf("usage %v: %v", args, err)
		}
	}
	deps := aliveDeps()
	deps.In = strings.NewReader(`{"reason":"` + strings.Repeat("x", 64<<10) + `"}`)
	if _, _, err := executeCLI(t, deps, "agent-manager", "configure", "project", "--file", "-"); ExitCode(err) != 2 {
		t.Fatalf("unbounded body: %v", err)
	}
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"message":"Reload governance","code":"AGENT_MANAGER_REVISION_CONFLICT","requestId":"manager-request"}`)
	writeRunFileFor(t, cfg, srv)
	deps = aliveDeps()
	deps.In = strings.NewReader(`{"expectedRevision":1}`)
	_, _, err := executeCLI(t, deps, "agent-manager", "configure", "project", "--file", "-")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "AGENT_MANAGER_REVISION_CONFLICT") || !strings.Contains(err.Error(), "manager-request") {
		t.Fatalf("daemon envelope lost: %v", err)
	}
}

func TestAgentManagerCLIInboxBodyLimitAndFailureEnvelope(t *testing.T) {
	for _, args := range [][]string{{"enqueue", "project"}, {"resolve", "project", "request"}} {
		deps := aliveDeps()
		deps.In = strings.NewReader(`{"reason":"` + strings.Repeat("x", 16<<10) + `"}`)
		if _, _, err := executeCLI(t, deps, append(append([]string{"agent-manager"}, args...), "--file", "-")...); ExitCode(err) != 2 {
			t.Fatalf("inbox body limit: %v", err)
		}
	}
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusConflict, `{"message":"Routing intent changed","code":"AGENT_MANAGER_REQUEST_CONFLICT","requestId":"inbox-error"}`)
	writeRunFileFor(t, cfg, srv)
	deps := aliveDeps()
	deps.In = strings.NewReader(`{"id":"work"}`)
	_, _, err := executeCLI(t, deps, "agent-manager", "enqueue", "project", "--file", "-")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "AGENT_MANAGER_REQUEST_CONFLICT") || !strings.Contains(err.Error(), "inbox-error") {
		t.Fatalf("inbox envelope lost: %v", err)
	}
}
