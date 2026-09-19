package telemetrymeta

import "strings"

// NormalizeCommandPath canonicalizes command paths received from current CLIs
// and best-effort legacy loopback callers before cost-control classification.
func NormalizeCommandPath(commandPath string) string {
	return strings.ToLower(strings.Join(strings.Fields(commandPath), " "))
}

// IsRoutineInternalCLICommand reports whether a successful CLI invocation is
// routine desktop/agent plumbing rather than product usage.
func IsRoutineInternalCLICommand(commandPath string) bool {
	normalized := NormalizeCommandPath(commandPath)
	for _, routine := range routineInternalCLICommands {
		if normalized == routine || strings.HasPrefix(normalized, routine+" ") {
			return true
		}
	}
	return false
}

var routineInternalCLICommands = []string{
	"ao status",
	"ao session ls",
	"ao session get",
	"ao session agent-switch ls",
	"ao session handoff",
	"ao project ls",
	"ao project get",
	"ao orchestrator ls",
	"ao hooks",
	"ao pty-host",
	"ao codex-login",
}

// CLIActorType infers the actor for legacy loopback CLI telemetry requests that
// predate the explicit actor_type field. Unknown actor-less commands are treated
// as system activity so foreign/local automation cannot inflate DAU by default.
func CLIActorType(actorType, commandPath string) string {
	normalized := NormalizeCommandPath(commandPath)
	if _, ok := legacyActorlessSystemCLICommands[normalized]; ok {
		return "system"
	}

	switch actorType {
	case "agent", "user":
		return actorType
	case "system":
		return "system"
	}

	if _, ok := legacyActorlessUserCLICommands[normalized]; ok {
		return "user"
	}
	switch normalized {
	case "ao session agent-switch", "ao session agent-switch ls", "ao session switch-agent":
		return "user"
	}
	if normalized == "ao hooks" {
		return "agent"
	}
	return "system"
}

var legacyActorlessSystemCLICommands = map[string]struct{}{
	"ao agent-process":           {},
	"ao agent-process supervise": {},
	"ao chat-host":               {},
	"ao completion":              {},
	"ao daemon":                  {},
	"ao help":                    {},
	"ao pty-host":                {},
	"ao start":                   {},
}

var legacyActorlessUserCLICommands = map[string]struct{}{
	"ao task":                   {},
	"ao task list":              {},
	"ao task show":              {},
	"ao task create":            {},
	"ao task revise":            {},
	"ao task revisions":         {},
	"ao task revision":          {},
	"ao task criteria":          {},
	"ao task set-criteria":      {},
	"ao task audit":             {},
	"ao task attempts":          {},
	"ao task context":           {},
	"ao task results":           {},
	"ao task result":            {},
	"ao task evaluate":          {},
	"ao task evaluations":       {},
	"ao task evaluation":        {},
	"ao task request-review":    {},
	"ao task performance":       {},
	"ao task metrics":           {},
	"ao task reviews":           {},
	"ao task review":            {},
	"ao task submit-result":     {},
	"ao task send-message":      {},
	"ao task messages":          {},
	"ao task message":           {},
	"ao task intents":           {},
	"ao task set-intent":        {},
	"ao knowledge":              {},
	"ao knowledge list":         {},
	"ao knowledge show":         {},
	"ao knowledge versions":     {},
	"ao knowledge version":      {},
	"ao knowledge create":       {},
	"ao knowledge revise":       {},
	"ao agent":                  {},
	"ao agent ls":               {},
	"ao agent-type":             {},
	"ao agent-type import":      {},
	"ao agent-type export":      {},
	"ao skill import":           {},
	"ao skill export":           {},
	"ao agent-type list":        {},
	"ao agent-type show":        {},
	"ao agent-type versions":    {},
	"ao agent-type audit":       {},
	"ao agent-type create":      {},
	"ao agent-type new-version": {},
	"ao agent-type update":      {},
	"ao agent-type activate":    {},
	"ao agent-type clone":       {},
	"ao skill":                  {},
	"ao skill list":             {},
	"ao skill show":             {},
	"ao skill versions":         {},
	"ao skill audit":            {},
	"ao skill create":           {},
	"ao skill new-version":      {},
	"ao skill update":           {},
	"ao skill activate":         {},
	"ao skill clone":            {},
	"ao browser":                {},
	"ao browser act":            {},
	"ao browser check":          {},
	"ao browser click":          {},
	"ao browser console":        {},
	"ao browser dblclick":       {},
	"ao browser devtools":       {},
	"ao browser devtools close": {},
	"ao browser devtools open":  {},
	"ao browser dialog":         {},
	"ao browser dialog accept":  {},
	"ao browser dialog dismiss": {},
	"ao browser dialog status":  {},
	"ao browser drag":           {},
	"ao browser errors":         {},
	"ao browser fill":           {},
	"ao browser focus":          {},
	"ao browser frame":          {},
	"ao browser get":            {},
	"ao browser highlight":      {},
	"ao browser hover":          {},
	"ao browser network":        {},
	"ao browser network clear":  {},
	"ao browser network list":   {},
	"ao browser network start":  {},
	"ao browser network status": {},
	"ao browser network stop":   {},
	"ao browser open":           {},
	"ao browser press":          {},
	"ao browser screenshot":     {},
	"ao browser scroll":         {},
	"ao browser scrollintoview": {},
	"ao browser select":         {},
	"ao browser snapshot":       {},
	"ao browser tab":            {},
	"ao browser tab close":      {},
	"ao browser tab new":        {},
	"ao browser tab select":     {},
	"ao browser status":         {},
	"ao browser tabs":           {},
	"ao browser type":           {},
	"ao browser uncheck":        {},
	"ao browser unhighlight":    {},
	"ao browser wait":           {},
	"ao dev":                    {},
	"ao dev import-projects":    {},
	"ao doctor":                 {},
	"ao import":                 {},
	"ao launch":                 {},
	"ao orchestrator":           {},
	"ao orchestrator done":      {},
	"ao pr":                     {},
	"ao pr merge":               {},
	"ao pr resolve-comments":    {},
	"ao preview":                {},
	"ao preview clear":          {},
	"ao preview start":          {},
	"ao preview status":         {},
	"ao preview stop":           {},
	"ao project":                {},
	"ao project add":            {},
	"ao project rm":             {},
	"ao project set-config":     {},
	"ao review":                 {},
	"ao review cancel":          {},
	"ao review ls":              {},
	"ao review submit":          {},
	"ao review trigger":         {},
	"ao send":                   {},
	"ao session":                {},
	"ao session claim-pr":       {},
	"ao session cleanup":        {},
	"ao session exit-agent":     {},
	"ao session kill":           {},
	"ao session rename":         {},
	"ao session resume-agent":   {},
	"ao session restore":        {},
	"ao spawn":                  {},
	"ao stop":                   {},
	"ao version":                {},

	// Legacy commands observed in PostHog's current billing-period data.
	"ao handoff":                   {},
	"ao project orchestration get": {},
	"ao project orchestration set": {},
	"ao smoke list":                {},
	"ao smoke set":                 {},
}
