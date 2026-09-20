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

func TestEvolutionCLICommandsRouteThroughHTTP(t *testing.T) {
	cfg := setConfigEnv(t)
	from := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	to := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	expectedWindow := url.Values{"from": {from}, "to": {to}}.Encode()
	conclusionBody := `{"outcome":"keep_control","reason":"No comparable work","from":"` + from + `","to":"` + to + `"}`
	recommendationBody := `{"kind":"skill","entryId":"reviewer","fromVersion":2,"observation":"Six tasks needed revisions","sampleSize":11,"proposed":{"skill":{"instructions":"Require component tests"}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/telemetry/cli-invoked" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/my project/experiments":
			_, _ = io.WriteString(w, `{"experiment":{"id":"experiment-1","status":"running"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/experiments":
			if r.URL.RawQuery != "afterId=experiment-0&limit=20" {
				t.Errorf("experiments query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"items":[{"id":"experiment-1"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/experiments/experiment-1":
			if r.URL.RawQuery != "" {
				t.Errorf("exact read takes no query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"experiment":{"id":"experiment-1","status":"running"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/experiments/experiment-1/evidence":
			if r.URL.RawQuery != expectedWindow {
				t.Errorf("evidence window: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"evidence":{"control":{"comparableAttempts":2}}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/my project/experiments/experiment-1/conclude":
			got, err := io.ReadAll(r.Body)
			if err != nil || string(got) != conclusionBody {
				t.Errorf("conclude envelope: %s %v", got, err)
			}
			_, _ = io.WriteString(w, `{"experiment":{"id":"experiment-1","status":"concluded"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/experiments/experiment-1/diff":
			_, _ = io.WriteString(w, `{"diff":{"fromVersion":1,"toVersion":2,"fields":[{"path":"instructions"}]}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/my project/recommendations":
			got, err := io.ReadAll(r.Body)
			if err != nil || string(got) != recommendationBody {
				t.Errorf("recommendation envelope: %s %v", got, err)
			}
			_, _ = io.WriteString(w, `{"recommendation":{"id":"recommendation-1","status":"pending"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/recommendations":
			if r.URL.RawQuery != "limit=20" {
				t.Errorf("recommendations query: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"items":[{"id":"recommendation-1"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/recommendations/recommendation-1":
			_, _ = io.WriteString(w, `{"recommendation":{"id":"recommendation-1"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/my project/recommendations/recommendation-1/decide":
			got, err := io.ReadAll(r.Body)
			if err != nil || !strings.Contains(string(got), `"disposition":"dismissed"`) {
				t.Errorf("decide envelope: %s %v", got, err)
			}
			_, _ = io.WriteString(w, `{"recommendation":{"id":"recommendation-1","status":"dismissed"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/my project/recommendations/recommendation-1/diff":
			_, _ = io.WriteString(w, `{"diff":{"fromVersion":2,"toVersion":0,"fields":[]}}`)
		default:
			t.Errorf("unexpected transport: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	createBody := `{"kind":"agent_type","entryId":"rust-coder","controlVersion":3,"candidateVersion":4,"hypothesis":"Fewer revisions","minimumSamples":12}`
	deps := aliveDeps()
	deps.In = strings.NewReader(createBody)
	if _, _, err := executeCLI(t, deps, "evolution", "experiment-create", "my project", "--file", "-"); err != nil {
		t.Fatalf("experiment-create: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "evolution", "experiments", "my project", "--after", "experiment-0"); err != nil {
		t.Fatalf("experiments: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "evolution", "experiment", "my project", "experiment-1"); err != nil {
		t.Fatalf("experiment: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "evolution", "evidence", "my project", "experiment-1", "--from", from, "--to", to); err != nil {
		t.Fatalf("evidence: %v", err)
	}
	deps = aliveDeps()
	deps.In = strings.NewReader(conclusionBody)
	if _, _, err := executeCLI(t, deps, "evolution", "conclude", "my project", "experiment-1", "--file", "-"); err != nil {
		t.Fatalf("conclude: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "evolution", "experiment-diff", "my project", "experiment-1"); err != nil {
		t.Fatalf("experiment-diff: %v", err)
	}
	deps = aliveDeps()
	deps.In = strings.NewReader(recommendationBody)
	if _, _, err := executeCLI(t, deps, "evolution", "recommend", "my project", "--file", "-"); err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "evolution", "recommendations", "my project"); err != nil {
		t.Fatalf("recommendations: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "evolution", "recommendation", "my project", "recommendation-1"); err != nil {
		t.Fatalf("recommendation: %v", err)
	}
	deps = aliveDeps()
	deps.In = strings.NewReader(`{"disposition":"dismissed","reason":"Keep the current version"}`)
	if _, _, err := executeCLI(t, deps, "evolution", "decide", "my project", "recommendation-1", "--file", "-"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "evolution", "recommendation-diff", "my project", "recommendation-1"); err != nil {
		t.Fatalf("recommendation-diff: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "evolution", "experiments", "my project", "--limit", "0"); ExitCode(err) != 2 {
		t.Fatalf("invalid limit: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "evolution", "evidence", "my project", "experiment-1", "--from", to, "--to", from); ExitCode(err) != 2 {
		t.Fatalf("inverted window: %v", err)
	}
	if _, _, err := executeCLI(t, aliveDeps(), "evolution", "recommend", "my project"); ExitCode(err) != 2 {
		t.Fatalf("missing file: %v", err)
	}
}
