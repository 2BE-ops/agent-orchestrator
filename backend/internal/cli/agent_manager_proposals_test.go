package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentManagerCLIProposePreservesEnvelopeAndNativeScope(t *testing.T) {
	cfg := setConfigEnv(t)
	const body = `{"sourceGeneration":"native-generation","idempotencyKey":"key","raw":"not JSON yet"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sessions/native manager/agent-manager/requests/work item/proposals" {
			t.Errorf("native transport: %s %s", r.Method, r.URL.Path)
		}
		got, err := io.ReadAll(r.Body)
		if err != nil || string(got) != body {
			t.Errorf("altered raw output envelope: %s %v", got, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":true,"proposal":{"validationError":"invalid JSON"}}`)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	deps := aliveDeps()
	deps.In = strings.NewReader(body)
	out, _, err := executeCLI(t, deps, "agent-manager", "propose", "native manager", "work item", "--file", "-")
	if err != nil || !strings.Contains(out, "validationError") {
		t.Fatalf("proposal receipt lost: %s %v", out, err)
	}
	deps.In = strings.NewReader(`{"raw":"` + strings.Repeat("x", 512<<10) + `"}`)
	if _, _, err := executeCLI(t, deps, "agent-manager", "propose", "manager", "work", "--file", "-"); ExitCode(err) != 2 {
		t.Fatalf("oversized native envelope: %v", err)
	}
}
