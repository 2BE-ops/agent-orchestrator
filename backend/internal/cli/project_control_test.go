package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestControlCLICommandsRouteThroughHTTP(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/control":
			_, _ = io.WriteString(w, `{"view":{"control":{"projectId":"my project","state":"running"}},"effectiveState":"running"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/my project/control":
			got, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(got), `"state":"paused"`) || !strings.Contains(string(got), `"reason":"Evening maintenance"`) {
				t.Errorf("pause envelope: %s", got)
			}
			_, _ = io.WriteString(w, `{"view":{"control":{"state":"paused"}},"effectiveState":"paused"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/my project/control/cancel-work":
			got, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(got), `"scope":"all"`) || !strings.Contains(string(got), `"reason":"Stop everything"`) {
				t.Errorf("cancel envelope: %s", got)
			}
			_, _ = io.WriteString(w, `{"cancel":{"result":{"scope":"all","cancelled":["task-1"]},"killServiceWired":true}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/needs-human":
			if r.URL.RawQuery != "afterId=nh-0&limit=20" {
				t.Errorf("needs human query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"items":[{"id":"nh-1","taskId":"task-1"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/task 1/needs-human":
			got, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(got), `"reasonCode":"credential_missing"`) || !strings.Contains(string(got), `"detail":"Expired credential"`) {
				t.Errorf("raise envelope: %s", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"needsHuman":{"id":"nh-1"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/task 1/needs-human/resolve":
			got, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(got), `"resolution":"Rotated the credential"`) {
				t.Errorf("resolve envelope: %s", got)
			}
			_, _ = io.WriteString(w, `{"needsHuman":{"id":"nh-1","resolution":{"resolution":"Rotated the credential"}}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/my project/dry-run":
			got, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(got), `"action":"create_task"`) || !strings.Contains(string(got), `"title":"New work"`) {
				t.Errorf("dry run envelope: %s", got)
			}
			_, _ = io.WriteString(w, `{"verdict":{"graphValid":true,"costEstimate":"unknown"}}`)
		default:
			t.Errorf("unexpected transport: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	if _, _, err := executeCLI(t, aliveDeps(), "project", "control", "my project"); err != nil {
		t.Fatalf("control read: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "project", "pause", "my project", "--reason", "Evening maintenance"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "project", "cancel", "my project", "--scope", "all", "--reason", "Stop everything"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "project", "needs-human", "my project", "--after", "nh-0"); err != nil {
		t.Fatalf("needs human list: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "task", "needs-human", "task 1", "--code", "credential_missing", "--detail", "Expired credential"); err != nil {
		t.Fatalf("raise: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "task", "resolve-human", "task 1", "--resolution", "Rotated the credential"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	dryRunDeps := aliveDeps()
	dryRunDeps.In = strings.NewReader(`{"actions":[{"action":"create_task","reason":"Add work","definition":{"title":"New work","brief":"x","category":"chore","maxAttempts":2}}]}`)
	if _, _, err := executeCLI(t, dryRunDeps, "project", "dry-run", "my project", "--file", "-"); err != nil {
		t.Fatalf("dry run: %v", err)
	}
}

func TestControlCLIRejectsInvalidUsage(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	for _, tc := range [][]string{
		{"project", "pause", "proj"},
		{"project", "resume", "proj", "--reason", "  "},
		{"project", "cancel", "proj", "--scope", "some", "--reason", "x"},
		{"project", "cancel", "proj", "--reason", ""},
		{"project", "needs-human", "proj", "--limit", "0"},
		{"task", "needs-human", "task", "--code", "credential_missing"},
		{"task", "needs-human", "task", "--detail", "only detail"},
		{"task", "resolve-human", "task"},
		{"project", "dry-run", "proj"},
	} {
		if _, _, err := executeCLI(t, aliveDeps(), tc...); ExitCode(err) != 2 {
			t.Fatalf("expected exit 2 for %v: %v", tc, err)
		}
	}
}
