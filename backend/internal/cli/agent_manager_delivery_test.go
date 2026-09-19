package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentManagerCLIInputHistoryUsesScopedHTTPReads(t *testing.T) {
	for _, spec := range []struct {
		args   []string
		suffix string
	}{
		{[]string{"contexts", "private project", "work item"}, "contexts"},
		{[]string{"context", "private project", "work item", "exact input"}, "contexts/exact input"},
		{[]string{"deliveries", "private project", "work item"}, "deliveries"},
	} {
		t.Run(spec.args[0], func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/internal/telemetry/cli-invoked" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/projects/private project/agent-manager/requests/work item/"+spec.suffix {
					t.Errorf("incorrect scoped read: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"state":"uncertain"}`)
			}))
			t.Cleanup(srv.Close)
			writeRunFileFor(t, cfg, srv)
			out, _, err := executeCLI(t, aliveDeps(), append([]string{"agent-manager"}, spec.args...)...)
			if err != nil || !strings.Contains(out, "uncertain") {
				t.Fatalf("history output: %s %v", out, err)
			}
			if _, _, err := executeCLI(t, aliveDeps(), "agent-manager", spec.args[0], "project"); ExitCode(err) != 2 {
				t.Fatalf("missing arguments: %v", err)
			}
		})
	}
}

func TestAgentManagerCLIHistoryPreservesErrorEnvelope(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"not_found","code":"AGENT_MANAGER_REQUEST_NOT_FOUND","message":"Input not found","requestId":"trace-input"}`)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	_, _, err := executeCLI(t, aliveDeps(), "agent-manager", "context", "project", "work", "missing")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "AGENT_MANAGER_REQUEST_NOT_FOUND") || !strings.Contains(err.Error(), "trace-input") {
		t.Fatalf("error identity lost: %v", err)
	}
}
