package claudecode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/pkg/agentcreds"
)

// The regression this whole ladder exists for: a revoked key in the
// environment is a credential that is present, not a credential that works.
// Reporting it as authorized is what let a 401-ing daemon render as ready.
func TestAuthStatusDoesNotTrustUnvalidatedAPIKey(t *testing.T) {
	for _, name := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		t.Run(name, func(t *testing.T) {
			clearClaudeCredentialEnv(t)
			t.Setenv(name, "sk-ant-revoked-key")

			status, err := claudeLocalAuthStatus(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if status == ports.AgentAuthStatusAuthorized {
				t.Fatalf("%s present reported as authorized; presence is not validity", name)
			}
			if status != ports.AgentAuthStatusConfigured {
				t.Fatalf("state = %q, want %q", status, ports.AgentAuthStatusConfigured)
			}
		})
	}
}

func TestClaudeConfigAuthStatusReadsSynchronously(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"userID":"user-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status, err := claudeConfigAuthStatus(ctx, path)
	if err != nil || status != ports.AgentAuthStatusConfigured {
		t.Fatalf("status = %q, err = %v", status, err)
	}
}

// Precedence: the reported credential must be the one Claude Code will
// actually send, or the diagnostics point at the wrong variable.
func TestLocalAuthStatusReportsConfiguredForCredentialEnvironment(t *testing.T) {
	clearClaudeCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "auth-token")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth-token")

	status, err := claudeLocalAuthStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status != ports.AgentAuthStatusConfigured {
		t.Fatalf("status = %q, want configured", status)
	}
}

func TestConfigAuthVerdictNeverReportsAuthorized(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    ports.AgentAuthStatus
	}{
		{"user id only", `{"userID":"user-1"}`, ports.AgentAuthStatusConfigured},
		{"oauth account", `{"oauthAccount":{"accountUuid":"account-1"}}`, ports.AgentAuthStatusConfigured},
		{
			"oauth subscription",
			`{"hasAvailableSubscription":true,"oauthAccount":{"accountUuid":"account-1"}}`,
			ports.AgentAuthStatusConfigured,
		},
		{"empty oauth account", `{"oauthAccount":{}}`, ports.AgentAuthStatusUnknown},
		{"no identity at all", `{"theme":"dark"}`, ports.AgentAuthStatusUnknown},
		{"empty file", ``, ports.AgentAuthStatusUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".claude.json")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			status, err := claudeConfigAuthStatus(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if status != tc.want {
				t.Fatalf("state = %q, want %q", status, tc.want)
			}
		})
	}
}

func TestConfigAuthVerdictMissingFileIsUnknown(t *testing.T) {
	status, err := claudeConfigAuthStatus(context.Background(), filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if status != ports.AgentAuthStatusUnknown {
		t.Fatalf("state = %q, want %q", status, ports.AgentAuthStatusUnknown)
	}
}

func TestCLIReportVerdict(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantParsed bool
		wantState  ports.AgentAuthStatus
	}{
		{
			name:       "logged in is configured, not authorized",
			output:     `{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"pro"}`,
			wantParsed: true, wantState: ports.AgentAuthStatusConfigured,
		},
		{
			name:       "api key source is reported as the credential",
			output:     `{"loggedIn":true,"apiKeySource":"ANTHROPIC_API_KEY","authMethod":"claude.ai"}`,
			wantParsed: true, wantState: ports.AgentAuthStatusConfigured,
		},
		{
			name:       "signed out is a verified rejection",
			output:     `{"loggedIn":false}`,
			wantParsed: true, wantState: ports.AgentAuthStatusUnauthorized,
		},
		{
			name:       "warning lines around the json are tolerated",
			output:     "warning: ignored config line\n{\"loggedIn\":true,\"authMethod\":\"oauth_token\"}\n",
			wantParsed: true, wantState: ports.AgentAuthStatusConfigured,
		},
		{
			name:   "unparsable output concludes nothing",
			output: "unsupported subcommand on this version",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report, ok := claudeAuthReportFromOutput([]byte(tc.output))
			if ok != tc.wantParsed {
				t.Fatalf("parsed = %v, want %v", ok, tc.wantParsed)
			}
			if !ok {
				return
			}
			status := report.status()
			if status != tc.wantState {
				t.Fatalf("state = %q, want %q", status, tc.wantState)
			}
			if status == ports.AgentAuthStatusAuthorized {
				t.Fatal("the CLI probe cannot prove a credential works")
			}
		})
	}
}

func TestClaudeAuthReportUsesProjectContext(t *testing.T) {
	previous := claudeAuthCommand
	claudeAuthCommand = func(_ context.Context, binary, workingDir string, env map[string]string) ([]byte, error) {
		if binary != "/opt/claude" || workingDir != "/project" || env["CLAUDE_CODE_USE_VERTEX"] != "1" {
			t.Fatalf("command context = binary %q dir %q env %#v", binary, workingDir, env)
		}
		return []byte(`{"loggedIn":true,"apiProvider":"vertex"}`), nil
	}
	t.Cleanup(func() { claudeAuthCommand = previous })

	report, ok := (&Plugin{}).claudeCLIAuthReport(context.Background(), "/opt/claude", "/project", map[string]string{
		"CLAUDE_CODE_USE_VERTEX": "1",
	})
	if !ok || report.APIProvider != "vertex" {
		t.Fatalf("report = %#v, parsed = %v", report, ok)
	}
}

// I1: the ladder is strictly additive. Anything it cannot resolve degrades to
// unknown, which never blocks a launch — it must not manufacture an
// unauthorized verdict out of a failure to look.
func TestUnreadableLocalStateDegradesToUnknownNotUnauthorized(t *testing.T) {
	clearClaudeCredentialEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, _ := claudeConfigAuthStatus(context.Background(), path)
	if status == ports.AgentAuthStatusUnauthorized {
		t.Fatal("a parse failure is our bug, not the user's missing credential")
	}
	if status != ports.AgentAuthStatusUnknown {
		t.Fatalf("state = %q, want %q", status, ports.AgentAuthStatusUnknown)
	}
}

func TestLocalAuthVerdictHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status, err := claudeLocalAuthStatus(ctx)
	if err == nil {
		t.Fatal("want the context error")
	}
	if status != ports.AgentAuthStatusUnknown {
		t.Fatalf("state = %q, want %q", status, ports.AgentAuthStatusUnknown)
	}
}

func clearClaudeCredentialEnv(t *testing.T) {
	t.Helper()
	for _, name := range claudeCredentialEnv {
		t.Setenv(name, "")
	}
}

// Rung 2 is the only rung permitted to return Authorized.
func TestOnlyTheProbeCanAuthorize(t *testing.T) {
	tests := []struct {
		name  string
		state agentcreds.State
		want  ports.AgentAuthStatus
	}{
		{"provider accepted", agentcreds.StateValid, ports.AgentAuthStatusAuthorized},
		{"provider rejected", agentcreds.StateInvalid, ports.AgentAuthStatusUnauthorized},
		{"could not tell", agentcreds.StateUnknown, ports.AgentAuthStatusUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := authStatusFromResult(agentcreds.Result{
				State: tc.state, Source: "ANTHROPIC_API_KEY", Fingerprint: "abc123def456",
			})
			if status != tc.want {
				t.Fatalf("state = %q, want %q", status, tc.want)
			}
		})
	}
}

// The runtime 401 handler drops the cached verdict; the provider has just
// contradicted it.
func TestInvalidateAuthCacheClearsTheStoredVerdict(t *testing.T) {
	fingerprint := (agentcreds.Credential{Secret: "k"}).Fingerprint()
	result := agentcreds.Result{
		State: agentcreds.StateValid, Provider: agentcreds.ProviderFirstParty, Fingerprint: fingerprint,
	}
	claudeAuthCache.put(result)
	if _, ok := claudeAuthCache.get(fingerprint, agentcreds.ProviderFirstParty); !ok {
		t.Fatal("expected the verdict to be cached")
	}
	InvalidateAuthCache()
	if _, ok := claudeAuthCache.get(fingerprint, agentcreds.ProviderFirstParty); ok {
		t.Fatal("a runtime rejection must clear the cached verdict")
	}
}

// withStubValidator points the probe at a local server for the duration of a
// test, so no unit test can reach a real provider.
func withStubValidator(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	previous := claudeValidator
	claudeValidator = func() *agentcreds.Validator {
		return agentcreds.New(server.Client())
	}
	t.Cleanup(func() { claudeValidator = previous })
	return server
}

// Rungs 1 and 2 end to end: the provider accepts, so the ladder returns the
// one state no local rung is allowed to produce.
func TestProbeAuthorizesOnlyOnAProviderAcceptance(t *testing.T) {
	clearClaudeCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-works")
	InvalidateAuthCache()
	server := withStubValidator(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-4-5-20251101"}]}`))
	})
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)

	status, ok := (&Plugin{}).probeAuthStatus(context.Background(), claudeAuthReport{APIProvider: "gateway"}, true)
	if !ok {
		t.Fatal("a definite provider answer must stop the ladder")
	}
	if status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("state = %q, want authorized", status)
	}
}

// A revoked key is what this whole change exists for: presence resolves it,
// and the provider is what turns it into a rejection.
func TestProbeRejectsARevokedCredential(t *testing.T) {
	clearClaudeCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-revoked")
	InvalidateAuthCache()
	server := withStubValidator(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"API key is invalid."}}`))
	})
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)

	status, ok := (&Plugin{}).probeAuthStatus(context.Background(), claudeAuthReport{APIProvider: "gateway"}, true)
	if !ok || status != ports.AgentAuthStatusUnauthorized {
		t.Fatalf("status = %q, want a verified rejection", status)
	}
}

// Rung 1 is a gate, not a guess: an apiProvider this build cannot validate
// must stop the probe rather than pick a host.
func TestProbeGateStopsBeforeSendingAnythingForAnUnknownProvider(t *testing.T) {
	clearClaudeCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-key")
	InvalidateAuthCache()
	withStubValidator(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("an unrecognized provider must not produce any request")
	})

	if _, ok := (&Plugin{}).probeAuthStatus(
		context.Background(), claudeAuthReport{APIProvider: "some-future-provider"}, true,
	); ok {
		t.Fatal("an unrecognized provider must hand down the ladder, not answer it")
	}
}

// I1 at the probe rung: an inconclusive probe hands down so the lower rungs
// can still speak, and never becomes a rejection of its own.
func TestInconclusiveProbeHandsDownTheLadder(t *testing.T) {
	clearClaudeCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-key")
	InvalidateAuthCache()
	server := withStubValidator(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)

	if _, ok := (&Plugin{}).probeAuthStatus(
		context.Background(), claudeAuthReport{APIProvider: "gateway"}, true,
	); ok {
		t.Fatal("a provider outage must not stop the ladder with a verdict")
	}
}

// The cache is consulted before anything is sent, and answers from the stored
// verdict when the credential has not changed.
func TestProbeAnswersFromCacheWithoutReprobing(t *testing.T) {
	clearClaudeCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-cached")
	InvalidateAuthCache()
	t.Cleanup(InvalidateAuthCache)

	claudeAuthCache.put(agentcreds.Result{
		State: agentcreds.StateValid, Provider: agentcreds.ProviderFirstParty, Source: "ANTHROPIC_API_KEY",
		Fingerprint: (agentcreds.Credential{
			Kind: agentcreds.KindAPIKey, Secret: "sk-ant-cached", Provider: agentcreds.ProviderFirstParty,
		}).Fingerprint(),
	})
	withStubValidator(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("a cache hit must not reach the provider")
	})

	status, ok := (&Plugin{}).probeAuthStatus(context.Background(), claudeAuthReport{APIProvider: "firstParty"}, true)
	if !ok || status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("status = %q, want the cached acceptance", status)
	}
}

// The provider response that proves the credential works also owns the model
// and effort catalog. Model discovery must reuse that exact response instead
// of issuing a second validation request that can fail independently.
func TestProviderModelsReuseTheValidatedAuthResponse(t *testing.T) {
	clearClaudeCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-cached-models")
	InvalidateAuthCache()
	t.Cleanup(InvalidateAuthCache)

	requests := 0
	server := withStubValidator(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5","display_name":"Claude Opus 5","capabilities":{"effort":{"supported":true,"low":{"supported":true},"medium":{"supported":true},"high":{"supported":true},"xhigh":{"supported":true},"max":{"supported":true}}}}]}`))
	})
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)

	status, ok := (&Plugin{}).probeAuthStatus(
		context.Background(), claudeAuthReport{APIProvider: "gateway"}, true,
	)
	if !ok || status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("status = %q, want the provider acceptance", status)
	}

	models, err := ProviderModels(context.Background(), "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %+v, want the validated provider model", models)
	}
	if models[0].ID != "claude-opus-5" || models[0].Label != "Claude Opus 5" {
		t.Fatalf("model = %+v, want the provider identity and label", models[0])
	}
	wantEfforts := []string{"low", "medium", "high", "xhigh", "max"}
	if !slices.Equal(models[0].Efforts, wantEfforts) {
		t.Fatalf("efforts = %v, want %v", models[0].Efforts, wantEfforts)
	}
	if requests != 1 {
		t.Fatalf("provider requests = %d, want one validation response reused for discovery", requests)
	}
}

func TestProviderModelsRefreshesAnAuthOnlyCacheEntry(t *testing.T) {
	clearClaudeCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-auth-only")
	InvalidateAuthCache()
	t.Cleanup(InvalidateAuthCache)
	requests := 0
	server := withStubValidator(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5"}]}`))
	})
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	credential := agentcreds.Credential{
		Kind: agentcreds.KindAPIKey, Secret: "sk-ant-auth-only", Provider: agentcreds.ProviderGateway,
		BaseURL: server.URL,
	}
	claudeAuthCache.put(agentcreds.Result{
		State: agentcreds.StateValid, Provider: agentcreds.ProviderGateway,
		Fingerprint: credential.Fingerprint(),
	})

	models, err := ProviderModels(context.Background(), "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || requests != 1 {
		t.Fatalf("models/requests = %+v/%d, want one fresh provider model", models, requests)
	}
}

func TestProviderModelsRunsCLIOncePerDiscoveryAndCachesProviderResult(t *testing.T) {
	clearClaudeCredentialEnv(t)
	InvalidateAuthCache()
	t.Cleanup(InvalidateAuthCache)

	requests := 0
	server := withStubValidator(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5"}]}`))
	})
	env := map[string]string{
		"ANTHROPIC_API_KEY":  "project-key",
		"ANTHROPIC_BASE_URL": server.URL,
	}
	cliReports := 0
	previous := claudeModelAuthReport
	claudeModelAuthReport = func(context.Context, string, string, map[string]string) (claudeAuthReport, bool) {
		cliReports++
		return claudeAuthReport{APIProvider: "gateway"}, true
	}
	t.Cleanup(func() { claudeModelAuthReport = previous })

	for range 2 {
		models, err := ProviderModels(context.Background(), "/opt/claude", "/workspace", env)
		if err != nil {
			t.Fatal(err)
		}
		if len(models) != 1 || models[0].ID != "claude-opus-5" {
			t.Fatalf("models = %+v, want cached provider catalog", models)
		}
	}
	if requests != 1 {
		t.Fatalf("provider requests = %d, want one", requests)
	}
	if cliReports != 2 {
		t.Fatalf("claude auth status calls = %d, want one per discovery", cliReports)
	}
}

func TestProviderModelsCachesInconclusiveResultBriefly(t *testing.T) {
	clearClaudeCredentialEnv(t)
	InvalidateAuthCache()
	t.Cleanup(InvalidateAuthCache)

	requests := 0
	server := withStubValidator(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	})
	env := map[string]string{
		"ANTHROPIC_API_KEY":  "project-key",
		"ANTHROPIC_BASE_URL": server.URL,
	}
	for range 2 {
		if _, err := ProviderModels(context.Background(), "", "/workspace", env); err == nil {
			t.Fatal("inconclusive provider response should fail discovery")
		}
	}
	if requests != 1 {
		t.Fatalf("provider requests = %d, want one cached inconclusive probe", requests)
	}
}

func TestProviderModelsUsesCLIReportedProvider(t *testing.T) {
	clearClaudeCredentialEnv(t)
	InvalidateAuthCache()
	t.Cleanup(InvalidateAuthCache)

	server := withStubValidator(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1beta1/publishers/anthropic/models") {
			t.Fatalf("path = %q, want Vertex model catalog", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"publisherModels":[{"name":"publishers/anthropic/models/claude-vertex"}]}`))
	})
	env := map[string]string{
		"ANTHROPIC_API_KEY":         "stale-first-party-key",
		"GOOGLE_OAUTH_ACCESS_TOKEN": "vertex-token",
		"GOOGLE_CLOUD_PROJECT":      "project",
		"ANTHROPIC_VERTEX_BASE_URL": server.URL,
	}
	firstParty := agentcreds.Credential{
		Kind: agentcreds.KindAPIKey, Secret: env["ANTHROPIC_API_KEY"], Provider: agentcreds.ProviderFirstParty,
	}
	claudeAuthCache.put(agentcreds.Result{
		State: agentcreds.StateValid, Provider: agentcreds.ProviderFirstParty,
		Fingerprint: firstParty.Fingerprint(), Models: []agentcreds.Model{{ID: "claude-wrong-provider"}},
	})

	previous := claudeModelAuthReport
	claudeModelAuthReport = func(_ context.Context, binary, workingDir string, gotEnv map[string]string) (claudeAuthReport, bool) {
		if binary != "/opt/claude" {
			t.Fatalf("binary = %q, want /opt/claude", binary)
		}
		if workingDir != "/workspace" || gotEnv["GOOGLE_CLOUD_PROJECT"] != "project" {
			t.Fatalf("discovery context = %q %#v", workingDir, gotEnv)
		}
		return claudeAuthReport{APIProvider: "vertex"}, true
	}
	t.Cleanup(func() { claudeModelAuthReport = previous })

	models, err := ProviderModels(context.Background(), "/opt/claude", "/workspace", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "claude-vertex" {
		t.Fatalf("models = %+v, want the CLI-reported Vertex catalog", models)
	}
}

func TestProviderModelsUsesConfiguredFoundryDeployments(t *testing.T) {
	InvalidateAuthCache()
	models, err := ProviderModels(context.Background(), "", "/workspace", map[string]string{
		"CLAUDE_CODE_USE_FOUNDRY":        "1",
		"ANTHROPIC_FOUNDRY_API_KEY":      "key",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "sonnet-deployment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "sonnet-deployment" {
		t.Fatalf("models = %+v", models)
	}
}
