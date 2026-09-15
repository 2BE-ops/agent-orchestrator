package agentcreds

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Chain-sourced credentials.
//
// Bedrock and Vertex can both draw credentials from sources no amount of code
// here can read: IMDS, ECS/EKS container credentials, SSO profiles,
// credential_process executables, workload identity federation, external
// account files. Reimplementing those is not a feature — it is adopting two
// credential-chain implementations and maintaining them permanently, in
// modules that today have zero AWS, GCP, or Azure dependencies.
//
// So this file does not resolve the chain. It asks the tool that already has:
// the provider's own CLI, which the user has necessarily configured if they
// are using chain credentials at all. That is consistent with how AO shells
// out to agent binaries everywhere else.
//
// Everything here is optional and fails soft. A missing CLI, a timeout, a
// non-zero exit — all Unknown, never a rejection. The runtime 401 handler
// remains the real coverage for this tier; this only buys earliness.

// commandRunner runs an external command and returns its combined output.
// It is an indirection so tests never execute a real CLI.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, args...).CombinedOutput()
}

func newWithCommandRunner(client *http.Client, runner commandRunner) *Validator {
	v := New(client)
	if runner != nil {
		v.execCmd = runner
	}
	return v
}

// ValidateVertexViaCLI asks gcloud for an access token and probes with it.
//
// gcloud has already resolved whatever the chain holds — user login, service
// account impersonation, workload identity federation — so one command turns
// an unresolvable credential into an ordinary bearer token.
func (v *Validator) ValidateVertexViaCLI(ctx context.Context, project, region string) Result {
	return v.validateVertexViaCLI(ctx, project, region, "")
}

func (v *Validator) validateVertexViaCLI(ctx context.Context, project, region, baseURL string) Result {
	result := Result{Provider: ProviderVertex, Source: "gcloud", CheckedAt: time.Now()}
	out, err := v.execCmd(ctx, "gcloud", "auth", "print-access-token")
	if err != nil {
		result.State = StateUnknown
		result.Detail = "gcloud is unavailable, so Vertex credentials could not be resolved"
		result.Err = err
		return result
	}
	token := strings.TrimSpace(lastNonEmptyLine(string(out)))
	if token == "" {
		result.State = StateUnknown
		result.Detail = "gcloud returned no access token"
		return result
	}
	return v.Validate(ctx, Credential{
		Kind: KindGoogleAccessToken, Secret: token, Source: "gcloud",
		Provider: ProviderVertex, Project: project, Region: region, BaseURL: baseURL,
	})
}

// ValidateBedrockViaCLI asks the aws CLI to list Bedrock foundation models.
//
// Here the exit code is the verdict: the CLI resolves the chain, signs, and
// calls the same control-plane endpoint the signed probe would. A non-zero
// exit is reported as Unknown rather than a rejection, because the CLI
// conflates "credentials refused" with "no CLI config", "wrong profile", and
// "network down", and this layer must never manufacture a lockout.
func (v *Validator) ValidateBedrockViaCLI(ctx context.Context, region string) Result {
	result := Result{Provider: ProviderBedrock, Source: "aws", CheckedAt: time.Now()}
	args := []string{"bedrock", "list-foundation-models", "--by-provider", "anthropic", "--output", "json"}
	if strings.TrimSpace(region) != "" {
		args = append(args, "--region", region)
	}
	out, err := v.execCmd(ctx, "aws", args...)
	if err != nil {
		result.State = StateUnknown
		result.Detail = "the aws CLI is unavailable or could not list Bedrock models"
		result.Err = err
		return result
	}
	models, parseErr := parseBedrockModels(out)
	if parseErr != nil {
		result.State = StateUnknown
		result.Detail = "the aws CLI returned output this build could not read"
		result.Err = parseErr
		return result
	}
	result.Models = models
	if len(models) == 0 {
		result.State = StateUnknown
		result.Detail = "the aws CLI listed no Anthropic models; this account may lack Bedrock access to Claude"
		return result
	}
	result.State = StateValid
	result.Detail = "the aws CLI resolved credentials and listed Claude models on Bedrock"
	return result
}

func lastNonEmptyLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if trimmed := strings.TrimSpace(lines[index]); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func readAllLimited(response *http.Response, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(response.Body, limit))
}
