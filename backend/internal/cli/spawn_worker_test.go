package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSpawnAgentTypeUsesOnlyDaemonResolution(t *testing.T) {
	cfg := setConfigEnv(t)
	var requests []string
	var received spawnRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appendPrimaryRequest(&requests, r)
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sessions" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"session":{"id":"worker-1","status":"idle"}}`)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	file := filepath.Join(t.TempDir(), "overrides.json")
	if err := os.WriteFile(file, []byte(`{"instructions":"","skills":[],"model":"configured/model"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "spawn", "--standalone", "--name", "Reviewer", "--agent-type", "type-1", "--type-version", "4", "--worker-overrides", file)
	if err != nil {
		t.Fatal(err)
	}
	if received.WorkerSelection == nil || received.WorkerSelection.AgentTypeID != "type-1" || received.WorkerSelection.Version != 4 || received.Harness != "" || received.Model != "" || received.Mode != "" {
		t.Fatalf("wrong request: %+v", received)
	}
	var overrides map[string]any
	if err := json.Unmarshal(received.WorkerSelection.Overrides, &overrides); err != nil {
		t.Fatal(err)
	}
	if overrides["instructions"] != "" || len(overrides["skills"].([]any)) != 0 {
		t.Fatalf("explicit clearing lost: %v", overrides)
	}
	if !reflect.DeepEqual(requests, []string{"POST /api/v1/sessions"}) {
		t.Fatalf("unexpected preflight: %v", requests)
	}
}

func TestSpawnAgentTypeRejectsAmbiguousOptions(t *testing.T) {
	for _, flags := range [][]string{
		{"--agent-type", " "}, {"--type-version", "2"}, {"--worker-overrides", "-"},
		{"--agent-type", "type", "--type-version", "-1"}, {"--agent-type", "type", "--agent", "codex"},
		{"--agent-type", "type", "--model", "other"}, {"--agent-type", "type", "--mode", "tui"}, {"--agent-type", "type", "--kind", "orchestrator"},
	} {
		t.Run(flags[0]+flags[len(flags)-1], func(t *testing.T) {
			_, _, err := executeCLI(t, Deps{}, append([]string{"spawn", "--standalone", "--name", "Review"}, flags...)...)
			var usage usageError
			if !errors.As(err, &usage) {
				t.Fatalf("want usage error before I/O, got %v", err)
			}
		})
	}
}
