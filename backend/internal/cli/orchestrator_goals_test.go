package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrchestratorCLIGoalPlanAndFeedbackRoutes(t *testing.T) {
	cfg := setConfigEnv(t)
	const planBody = `{"sourceGeneration":"gen-1","idempotencyKey":"plan-1","action":{"action":"create_task","reason":"Decompose","definition":{"title":"Slice","brief":"Work","maxAttempts":2}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/orchestrator/goal":
			_, _ = io.WriteString(w, `{"goal":{"number":1},"sourceGeneration":"gen-1"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/my project/goal":
			got, err := io.ReadAll(r.Body)
			if err != nil || !strings.Contains(string(got), `"goal":"Ship it"`) {
				t.Errorf("set-goal envelope: %s %v", got, err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"goal":{"number":2}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/my project/orchestrator/plan":
			got, err := io.ReadAll(r.Body)
			if err != nil || string(got) != planBody {
				t.Errorf("plan envelope: %s %v", got, err)
			}
			_, _ = io.WriteString(w, `{"created":true,"receipt":{"outcome":{"taskId":"task-1","revision":1}}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/my project/orchestrator/complete":
			_, _ = io.WriteString(w, `{"created":true,"completion":{"goalVersion":1}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/orchestrator/feedback":
			if r.URL.RawQuery != "after=task-0&limit=20" {
				t.Errorf("feedback query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"items":[{"taskId":"task-1","state":"pending"}],"nextAfter":"task-1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/orchestrator/receipts":
			if r.URL.RawQuery != "afterId=receipt-0&limit=20" {
				t.Errorf("receipts query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"items":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/goal/versions":
			if r.URL.RawQuery != "after=1&limit=20" {
				t.Errorf("versions query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"items":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/orchestrator/receipts/receipt-9":
			if r.URL.RawQuery != "" {
				t.Errorf("exact read takes no query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"receipt":{"outcome":{"taskId":"task-1"}}}`)
		default:
			t.Errorf("unexpected transport: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)
	out, _, err := executeCLI(t, aliveDeps(), "orchestrator", "goal", "my project")
	if err != nil || !strings.Contains(out, "sourceGeneration") {
		t.Fatalf("native goal read: %s %v", out, err)
	}
	deps := aliveDeps()
	deps.In = strings.NewReader(`{"goal":"Ship it","reason":"Kick off"}`)
	out, _, err = executeCLI(t, deps, "orchestrator", "set-goal", "my project", "--file", "-")
	if err != nil || !strings.Contains(out, `"number": 2`) {
		t.Fatalf("set-goal: %s %v", out, err)
	}
	deps = aliveDeps()
	deps.In = strings.NewReader(planBody)
	out, _, err = executeCLI(t, deps, "orchestrator", "plan", "my project", "--file", "-")
	if err != nil || !strings.Contains(out, `"created": true`) {
		t.Fatalf("plan: %s %v", out, err)
	}
	deps = aliveDeps()
	deps.In = strings.NewReader(`{"sourceGeneration":"gen-1","goalVersion":1,"summary":"Done","reason":"Terminal"}`)
	if _, _, err := executeCLI(t, deps, "orchestrator", "complete", "my project", "--file", "-"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "orchestrator", "feedback", "my project", "--after", "task-0"); err != nil {
		t.Fatalf("feedback: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "orchestrator", "receipts", "my project", "--after", "receipt-0"); err != nil {
		t.Fatalf("receipts: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "orchestrator", "goal-versions", "my project", "--after", "1"); err != nil {
		t.Fatalf("goal-versions: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "orchestrator", "receipt", "my project", "receipt-9"); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "orchestrator", "feedback", "my project", "--limit", "0"); ExitCode(err) != 2 {
		t.Fatalf("invalid feedback limit: %v", err)
	}
}
