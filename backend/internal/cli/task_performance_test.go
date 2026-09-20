package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskPerformanceCLITransportAndErrorEnvelope(t *testing.T) {
	for _, name := range []string{"performance", "metrics"} {
		t.Run(name, func(t *testing.T) {
			cfg := setConfigEnv(t)
			var path, method, from, to, group, cursor, limit string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path, method = r.URL.Path, r.Method
				query := r.URL.Query()
				from, to, group, cursor, limit = query.Get("from"), query.Get("to"), query.Get("groupBy"), query.Get("cursor"), query.Get("limit")
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"evidence":true}`)
			}))
			t.Cleanup(srv.Close)
			writeRunFileFor(t, cfg, srv)
			args := []string{"task", name, "client project", "--from", "2026-09-01T00:00:00+01:00", "--to", "2026-09-19T00:00:00Z"}
			if name == "metrics" {
				args = append(args, "--group-by", "skill_version")
			} else {
				args = append(args, "--limit", "7", "--cursor", "attempt+1")
			}
			out, _, err := executeCLI(t, aliveDeps(), args...)
			wantPath := "/api/v1/projects/client project/task-performance"
			if name == "metrics" {
				wantPath += "/summary"
			}
			if err != nil || method != http.MethodGet || path != wantPath || from != "2026-09-01T00:00:00+01:00" || to != "2026-09-19T00:00:00Z" || !strings.Contains(out, "evidence") {
				t.Fatalf("transport: %s %s from=%s to=%s err=%v", method, path, from, to, err)
			}
			if (name == "metrics" && (group != "skill_version" || cursor != "" || limit != "")) || (name == "performance" && (group != "" || cursor != "attempt+1" || limit != "7")) {
				t.Fatalf("parameters: group=%s cursor=%s limit=%s", group, cursor, limit)
			}
		})
	}
	cfg := setConfigEnv(t)
	srv, _ := reviewServer(t, http.StatusBadRequest, `{"message":"Narrow the window","code":"PERFORMANCE_WINDOW_TOO_LARGE","requestId":"performance-request"}`)
	writeRunFileFor(t, cfg, srv)
	_, _, err := executeCLI(t, aliveDeps(), "task", "metrics", "project", "--from", "2026-01-01T00:00:00Z", "--to", "2027-01-01T00:00:00Z")
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "PERFORMANCE_WINDOW_TOO_LARGE") || !strings.Contains(err.Error(), "performance-request") {
		t.Fatalf("daemon error lost: %v", err)
	}
}

func TestTaskPerformanceCLIUsage(t *testing.T) {
	window := []string{"--from", "2026-01-01T00:00:00Z", "--to", "2027-01-01T00:00:00Z"}
	for _, args := range [][]string{
		{"task", "performance"}, {"task", "metrics", "project"}, {"task", "performance", "project", "--from", "bad", "--to", "bad"},
		append([]string{"task", "performance", ""}, window...), append([]string{"task", "performance", "project", "--limit", "101"}, window...),
		append([]string{"task", "performance", "project", "--cursor", "bad\ncursor"}, window...), append([]string{"task", "metrics", "project", "--group-by", "score"}, window...),
		append([]string{"task", "metrics", "project", "--limit", "1"}, window...), {"task", "metrics", "project", "--from", "2026-01-01T00:00:00Z", "--to", "2025-01-01T00:00:00Z"},
		{"task", "metrics", "project", "--from", "2025-01-01T00:00:00Z", "--to", "2027-01-01T00:00:00Z"},
	} {
		if _, _, err := executeCLI(t, aliveDeps(), args...); ExitCode(err) != 2 {
			t.Fatalf("usage %v: %v", args, err)
		}
	}
}
