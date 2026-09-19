package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAttributionCLICommandsRouteThroughHTTP(t *testing.T) {
	cfg := setConfigEnv(t)
	from := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	to := time.Date(2026, 9, 19, 11, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	expectedPlanningWindow := url.Values{"from": {from}, "to": {to}}.Encode()
	expectedRoutingWindow := url.Values{"from": {from}, "to": {to}}.Encode()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/orchestrator/planning-outcomes":
			if r.URL.RawQuery != "afterId=receipt-0&limit=20" {
				t.Errorf("planning outcomes query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"items":[{"receiptId":"receipt-1","taskId":"task-1","state":"pending"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/orchestrator/planning-summary":
			if r.URL.RawQuery != expectedPlanningWindow {
				t.Errorf("planning summary window: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"summary":{"totals":{"receipts":1}}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/agent-manager/routing-outcomes":
			if r.URL.RawQuery != "after=4&limit=20" {
				t.Errorf("routing outcomes query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"items":[{"sequence":5,"outcome":"rejected","state":"pending"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/agent-manager/routing-summary":
			if r.URL.RawQuery != expectedRoutingWindow {
				t.Errorf("routing summary window: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"summary":{"totals":{"decisions":1}}}`)
		default:
			t.Errorf("unexpected transport: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	if _, _, err := executeCLI(t, aliveDeps(), "orchestrator", "planning-outcomes", "my project", "--after", "receipt-0"); err != nil {
		t.Fatalf("planning outcomes: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "orchestrator", "planning-summary", "my project", "--from", from, "--to", to); err != nil {
		t.Fatalf("planning summary: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "agent-manager", "routing-outcomes", "my project", "--after", "4"); err != nil {
		t.Fatalf("routing outcomes: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "agent-manager", "routing-summary", "my project", "--from", from, "--to", to); err != nil {
		t.Fatalf("routing summary: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "agent-manager", "routing-outcomes", "my project", "--limit", "0"); ExitCode(err) != 2 {
		t.Fatalf("invalid routing limit: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "orchestrator", "planning-summary", "my project", "--from", to, "--to", from); ExitCode(err) != 2 {
		t.Fatalf("inverted window: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "orchestrator", "planning-summary", "my project", "--from", from); ExitCode(err) != 2 {
		t.Fatalf("missing window bound: %v", err)
	}
	if !strings.Contains(from, "2026-09-19") {
		t.Fatalf("window fixture drifted: %s", from)
	}
}
