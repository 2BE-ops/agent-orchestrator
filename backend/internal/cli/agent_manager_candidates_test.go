package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentManagerCLICandidatesUseScopedBoundedHTTP(t *testing.T) {
	for _, tc := range []struct {
		args          []string
		suffix, query string
	}{
		{[]string{"candidates", "private project", "work item"}, "candidates", "limit=20"},
		{[]string{"candidates", "private project", "work item", "--cursor", "a & b", "--limit", "3"}, "candidates", "cursor=a+%26+b&limit=3"},
		{[]string{"candidate", "private project", "work item", "private type", "--version", "12"}, "candidates/private type", "version=12"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/internal/telemetry/cli-invoked" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/projects/private project/agent-manager/requests/work item/"+tc.suffix || r.URL.RawQuery != tc.query {
					t.Errorf("wrong scope or query: %s %s", r.Method, r.URL.String())
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"eligible":false,"issues":[{"code":"CONTEXT_CLEARANCE_EXCEEDED","state":"invalid"}]}`)
			}))
			t.Cleanup(srv.Close)
			writeRunFileFor(t, cfg, srv)
			out, _, err := executeCLI(t, aliveDeps(), append([]string{"agent-manager"}, tc.args...)...)
			if err != nil || !strings.Contains(out, "CONTEXT_CLEARANCE_EXCEEDED") {
				t.Fatalf("candidate output lost: %s %v", out, err)
			}
		})
	}
}

func TestAgentManagerCLICandidateUsageAndDaemonErrors(t *testing.T) {
	cfg := setConfigEnv(t)
	for _, args := range [][]string{{"candidates", "project"}, {"candidates", "project", "request", "--limit", "21"}, {"candidate", "project", "request", "type"}, {"candidate", "project", "request", "type", "--version", "0"}, {"candidates", "project", "bad\nrequest"}, {"candidates", "project", "request", "--cursor", "\n"}} {
		if _, _, err := executeCLI(t, aliveDeps(), append([]string{"agent-manager"}, args...)...); ExitCode(err) != 2 {
			t.Fatalf("usage not exit 2: %v %v", args, err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":"conflict","code":"AGENT_MANAGER_WORK_FENCED","message":"Routing changed","requestId":"trace-candidate"}`)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	_, _, err := executeCLI(t, aliveDeps(), "agent-manager", "candidates", "project", "request")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "AGENT_MANAGER_WORK_FENCED") || !strings.Contains(err.Error(), "trace-candidate") {
		t.Fatalf("daemon error lost: %v", err)
	}
}
