package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentManagerCLIRegistryAuthorPreservesEnvelopeAndNativeScope(t *testing.T) {
	cfg := setConfigEnv(t)
	const body = `{"sourceGeneration":"native-generation","idempotencyKey":"author-key","action":{"action":"create","kind":"skill","name":"Binary Format Tests","reason":"Missing coverage","definition":{"skill":{"instructions":"Verify boundaries"}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sessions/native manager/agent-manager/requests/work item/registry-actions" {
			t.Errorf("native transport: %s %s", r.Method, r.URL.Path)
		}
		got, err := io.ReadAll(r.Body)
		if err != nil || string(got) != body {
			t.Errorf("altered authoring envelope: %s %v", got, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":true,"receipt":{"target":{"id":"skill","version":1}}}`)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	deps := aliveDeps()
	deps.In = strings.NewReader(body)
	out, _, err := executeCLI(t, deps, "agent-manager", "registry-author", "native manager", "work item", "--file", "-")
	if err != nil || !strings.Contains(out, `"created": true`) || !strings.Contains(out, `"target"`) {
		t.Fatalf("authoring receipt lost: %s %v", out, err)
	}
	deps.In = strings.NewReader(`{"action":{"definition":{"skill":{"instructions":"` + strings.Repeat("x", 288<<10) + `"}}}}`)
	if _, _, err := executeCLI(t, deps, "agent-manager", "registry-author", "manager", "work", "--file", "-"); ExitCode(err) != 2 {
		t.Fatalf("oversized authoring envelope: %v", err)
	}
}

func TestAgentManagerCLIRegistryReceiptHistoryBuildsBoundedQueries(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("history transport: %s", r.Method)
		}
		if strings.HasSuffix(r.URL.Path, "/registry-receipts/receipt-9") {
			if r.URL.RawQuery != "" {
				t.Errorf("exact read takes no query: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"receipt-9"}`)
			return
		}
		switch r.URL.RawQuery {
		case "afterId=receipt-1&limit=100":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[],"nextAfterId":"receipt-2"}`)
		case "limit=20":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[]}`)
		default:
			t.Errorf("unbounded page query: %s", r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	out, _, err := executeCLI(t, aliveDeps(), "agent-manager", "registry-receipts", "project", "request", "--limit", "100", "--after", "receipt-1")
	if err != nil || !strings.Contains(out, "nextAfterId") {
		t.Fatalf("receipt page lost: %s %v", out, err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "agent-manager", "registry-receipts", "project", "request"); err != nil {
		t.Fatalf("default page refused: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "agent-manager", "registry-receipt", "project", "request", "receipt-9"); err != nil {
		t.Fatalf("exact receipt read refused: %v", err)
	}
	for _, args := range [][]string{{"--limit", "0"}, {"--limit", "101"}} {
		if _, _, err := executeCLI(t, aliveDeps(), append([]string{"agent-manager", "registry-receipts", "project", "request"}, args...)...); ExitCode(err) != 2 {
			t.Fatalf("unbounded limit accepted: %v", err)
		}
	}
}
