package authprobe

import (
	"context"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// CmdRunner runs the command and returns the combined stdout/stderr.
// It is exposed as a package variable to allow mocking in tests.
var CmdRunner = func(ctx context.Context, name string, arg ...string) ([]byte, error) {
	return aoprocess.CommandContext(ctx, name, arg...).CombinedOutput()
}

// CLIStatus runs bounded local CLI probes and classifies their output.
// Callers must pass adapter-specific commands; catalog refresh should not run
// a generic sequence of auth-like commands against every installed binary.
func CLIStatus(ctx context.Context, binary string, commands [][]string) (ports.AgentAuthStatus, error) {
	return CLIStatusWithTimeout(ctx, binary, commands, 3*time.Second)
}

// CLIStatusWithTimeout is CLIStatus with an adapter-specific per-command
// timeout for CLIs whose native status command has documented startup work.
func CLIStatusWithTimeout(ctx context.Context, binary string, commands [][]string, timeout time.Duration) (ports.AgentAuthStatus, error) {
	if err := ctx.Err(); err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	if binary == "" {
		return ports.AgentAuthStatusUnknown, nil
	}
	if len(commands) == 0 {
		return ports.AgentAuthStatusUnknown, nil
	}
	// Run probes in parallel to avoid blocking on multiple 3s timeouts sequentially.
	type result struct {
		status ports.AgentAuthStatus
		err    error
		index  int
	}
	resultsChan := make(chan result, len(commands))
	for i, args := range commands {
		go func(idx int, cmdArgs []string) {
			status, err := commandStatus(ctx, binary, cmdArgs, timeout)
			resultsChan <- result{status, err, idx}
		}(i, args)
	}
	// Collect results, returning first definite status or last error.
	var lastErr error
	for i := 0; i < len(commands); i++ {
		res := <-resultsChan
		if res.err != nil {
			lastErr = res.err
		}
		if res.status != ports.AgentAuthStatusUnknown {
			return res.status, nil
		}
	}
	return ports.AgentAuthStatusUnknown, lastErr
}

func commandStatus(ctx context.Context, binary string, args []string, timeout time.Duration) (ports.AgentAuthStatus, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := CmdRunner(probeCtx, binary, args...)
	if probeCtx.Err() != nil {
		if probeCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return ports.AgentAuthStatusUnknown, nil
		}
		return ports.AgentAuthStatusUnknown, probeCtx.Err()
	}
	status := StatusFromText(string(out))
	if status != ports.AgentAuthStatusUnknown {
		return status, nil
	}
	// Return actual error from CmdRunner rather than silently swallowing it.
	if err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	return ports.AgentAuthStatusUnknown, nil
}

// StatusFromText classifies common CLI auth/status output.
func StatusFromText(out string) ports.AgentAuthStatus {
	text := strings.ToLower(out)
	compactText := compact(text)
	if hasAny(text,
		"not logged in",
		"not currently logged in",
		"logged out",
		"not authenticated",
		"unauthenticated",
		"authentication required",
		"not authorized",
		"unauthorized",
		"login required",
		"no credentials",
		"0 credentials",
		"no api key",
		"no token",
		`"loggedin": false`,
		`"loggedin":false`,
	) || hasAny(compactText,
		`"authenticated":false`,
		`'authenticated':false`,
		"authenticated:false",
		"authenticated=false",
		`"authorized":false`,
		`'authorized':false`,
		"authorized:false",
		"authorized=false",
		`"logged_in":false`,
		`'logged_in':false`,
		"logged_in:false",
		"logged_in=false",
		`"loggedin":false`,
		`'loggedin':false`,
		"loggedin:false",
		"loggedin=false",
	) {
		return ports.AgentAuthStatusUnauthorized
	}
	if hasAny(text,
		"logged in",
		"authenticated",
		"authorized",
		"token valid",
		"api key found",
		"credentials found",
		`"loggedin": true`,
		`"loggedin":true`,
	) {
		return ports.AgentAuthStatusAuthorized
	}
	return ports.AgentAuthStatusUnknown
}

func compact(text string) string {
	return strings.NewReplacer(" ", "", "\t", "", "\n", "", "\r", "").Replace(text)
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
