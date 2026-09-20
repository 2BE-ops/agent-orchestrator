package domain

import (
	"os"
	"strings"
)

// AgentHarness identifies which agent CLI/runtime a session drives.
type AgentHarness string

// Supported agent harnesses.
const (
	HarnessClaudeCode AgentHarness = "claude-code"
	HarnessCodex      AgentHarness = "codex"
	HarnessAider      AgentHarness = "aider"
	HarnessOpenCode   AgentHarness = "opencode"
	HarnessGrok       AgentHarness = "grok"
	HarnessDroid      AgentHarness = "droid"
	HarnessAmp        AgentHarness = "amp"
	HarnessAgy        AgentHarness = "agy"
	HarnessCrush      AgentHarness = "crush"
	HarnessCursor     AgentHarness = "cursor"
	HarnessQwen       AgentHarness = "qwen"
	HarnessCopilot    AgentHarness = "copilot"
	HarnessGoose      AgentHarness = "goose"
	HarnessAuggie     AgentHarness = "auggie"
	HarnessContinue   AgentHarness = "continue"
	HarnessDevin      AgentHarness = "devin"
	HarnessCline      AgentHarness = "cline"
	HarnessKimi       AgentHarness = "kimi"
	HarnessMuse       AgentHarness = "muse"
	HarnessKiro       AgentHarness = "kiro"
	HarnessKilocode   AgentHarness = "kilocode"
	HarnessVibe       AgentHarness = "vibe"
	HarnessPi         AgentHarness = "pi"
	HarnessKimchi     AgentHarness = "kimchi"
	HarnessPrimeAgent AgentHarness = "prime-agent"
	HarnessAutohand   AgentHarness = "autohand"
	HarnessOMP        AgentHarness = "omp"
	// HarnessFake is the deterministic LLM-free e2e harness. It stays out of
	// AllHarnesses (so no user-facing enumeration lists it) and is only
	// accepted by IsKnown when the explicit AO_FAKE_HARNESS opt-in is set —
	// the same gate the agent adapter registry consults, keeping e2e/dev/lab
	// machines and normal user machines on one consistent rule.
	HarnessFake AgentHarness = "fake"
)

// fakeHarnessOptInEnv is the opt-in env var shared with the agent adapter
// registry (adapters/agent/fake). It is deliberately duplicated as a private
// constant so the domain vocabulary layer keeps zero dependencies.
const fakeHarnessOptInEnv = "AO_FAKE_HARNESS"

// fakeHarnessOptIn reports whether AO_FAKE_HARNESS is set to a truthy token.
func fakeHarnessOptIn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(fakeHarnessOptInEnv))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// AllHarnesses lists every supported harness. It is the canonical set used to
// validate user-supplied harness names (e.g. per-project role overrides).
var AllHarnesses = []AgentHarness{
	HarnessClaudeCode, HarnessCodex, HarnessAider, HarnessOpenCode, HarnessGrok,
	HarnessDroid, HarnessAmp, HarnessAgy, HarnessCrush, HarnessCursor, HarnessQwen,
	HarnessCopilot, HarnessGoose, HarnessAuggie, HarnessContinue, HarnessDevin,
	HarnessCline, HarnessKimi, HarnessMuse, HarnessKiro, HarnessKilocode, HarnessVibe, HarnessPi,
	HarnessKimchi, HarnessPrimeAgent, HarnessAutohand,
	HarnessOMP,
}

// IsKnown reports whether h is one of the supported harnesses. The opt-in
// fake harness counts as known only while AO_FAKE_HARNESS is truthy, so
// Agent Type definitions, role overrides and reviewer pins authored on a
// lab/e2e machine validate normally while every default machine keeps
// refusing the fake harness.
func (h AgentHarness) IsKnown() bool {
	if h == HarnessFake {
		return fakeHarnessOptIn()
	}
	for _, k := range AllHarnesses {
		if h == k {
			return true
		}
	}
	return false
}
