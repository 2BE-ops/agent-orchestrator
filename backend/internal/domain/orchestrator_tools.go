package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// OrchestratorToolPaths are daemon-supplied CLI routing facts, identical in
// spirit to the Manager paths: literal argv values, never shell syntax and
// never caller-provided prompt content.
type OrchestratorToolPaths struct {
	SchemaVersion int    `json:"schemaVersion"`
	Executable    string `json:"executable"`
	RunFile       string `json:"runFile,omitempty"`
}

// Validate bounds literal argv/environment values without evaluating a shell.
func (p OrchestratorToolPaths) Validate() error {
	if p.SchemaVersion != 1 || strings.TrimSpace(p.Executable) == "" || !utf8.ValidString(p.Executable) || !utf8.ValidString(p.RunFile) || len(p.Executable) > 4096 || len(p.RunFile) > 4096 || strings.IndexFunc(p.Executable, unicode.IsControl) >= 0 || strings.IndexFunc(p.RunFile, unicode.IsControl) >= 0 {
		return fmt.Errorf("invalid orchestrator CLI routing paths")
	}
	return nil
}

// OrchestratorProtocolContext renders the orchestrator's sealed planning
// protocol. Version 1 is immutable: later tools must add a new versioned
// renderer while retaining this bytes-exact output for hash verification.
type OrchestratorProtocolContext struct {
	ProjectID   ProjectID
	GoalVersion int64
	GoalHash    string
	Tools       *OrchestratorToolPaths
}

// ToolPrompt renders protocol v1 for an orchestrator session whose project has
// a current goal. Projects without a goal keep their legacy prompt.
func (c OrchestratorProtocolContext) ToolPrompt() (string, error) {
	if c.Tools == nil {
		return "", nil
	}
	if err := c.Tools.Validate(); err != nil {
		return "", err
	}
	if !messageIdentity(string(c.ProjectID)) {
		return "", fmt.Errorf("orchestrator tool input requires project attribution")
	}
	if c.GoalVersion < 1 || len(c.GoalHash) != 64 {
		return "", fmt.Errorf("orchestrator tools require the current pinned goal version")
	}
	executable := c.Tools.Executable
	definition := OrchestratorPlanAction{Action: "create_task", Definition: &TaskDefinition{Title: "REPLACE_WITH_TITLE", Brief: "REPLACE_WITH_VERBATIM_WORK_DESCRIPTION", Category: "feature", Priority: 5, MaxAttempts: 3, Dependencies: []string{}, RequiredCapabilities: []string{}}, Reason: "Replace with why this task is needed now"}
	raw, err := json.Marshal(definition)
	if err != nil {
		return "", err
	}
	protocol := struct {
		SchemaVersion int                 `json:"schemaVersion"`
		Goal          map[string]int64    `json:"goal"`
		Commands      map[string][]string `json:"commands"`
		Environment   map[string]string   `json:"environment,omitempty"`
		PlanExample   any                 `json:"planExample"`
	}{
		SchemaVersion: 1,
		Goal:          map[string]int64{"version": c.GoalVersion},
		Commands: map[string][]string{
			"goal":         {executable, "orchestrator", "goal", string(c.ProjectID)},
			"plan":         {executable, "orchestrator", "plan", string(c.ProjectID), "--file", "-"},
			"complete":     {executable, "orchestrator", "complete", string(c.ProjectID), "--file", "-"},
			"feedback":     {executable, "orchestrator", "feedback", string(c.ProjectID), "--after", "REPLACE_WITH_LAST_TASK_ID", "--limit", "20"},
			"receipts":     {executable, "orchestrator", "receipts", string(c.ProjectID), "--limit", "100"},
			"tasks":        {executable, "task", "list", string(c.ProjectID), "--limit", "100"},
			"taskShow":     {executable, "task", "show", "REPLACE_WITH_TASK_ID"},
			"goalVersions": {executable, "orchestrator", "goal-versions", string(c.ProjectID), "--limit", "100"},
		},
		PlanExample: struct {
			SourceGeneration string `json:"sourceGeneration"`
			IdempotencyKey   string `json:"idempotencyKey"`
			Raw              string `json:"raw"`
		}{"REPLACE_WITH_GOAL_SOURCE_GENERATION", "plan-1", string(raw)},
	}
	if c.Tools.RunFile != "" {
		protocol.Environment = map[string]string{"AO_RUN_FILE": c.Tools.RunFile}
	}
	encoded, err := json.Marshal(protocol)
	if err != nil {
		return "", err
	}
	return `

## AO Orchestrator planning protocol
Run the supplied JSON argv as executable plus literal arguments and apply the
literal environment values to reach this daemon. You own goal decomposition:
create tasks, dependencies and frozen acceptance criteria through the plan
command; revise or freeze through it as well. The goal block pins the version
this protocol was rendered for; goal re-reads return the current version and
your current sourceGeneration. Read the goal before planning and before
completing; both submissions require that exact sourceGeneration, and AO
rechecks it against this project's live orchestrator.

Send one action per plan envelope on stdin with a stable idempotencyKey per
identical submission; a changed action uses a new key. AO mints task identity;
never choose task IDs. Graph cycles, unknown dependencies, stale revisions and
unfrozen criteria are refused transactionally. Inspect retained outcomes with
receipts before replanning. Repeated identical submissions return the original
receipt without duplicating work.

Completion against criteria is deterministic: AO verifies every project task is
independently verified against its frozen criteria or explicitly cancelled
before any goal completion is retained, and refuses with the concrete blockers
otherwise. Read terminal outcomes through feedback; exhausted task attempt
limits are reported there as failed work. Do not spawn workers, choose agents
or bypass scheduler admission: delegation belongs to the Agent Manager. Do not
rewrite the project goal; the user authors it. Raw output is limited to 64 KiB.

AO_ORCHESTRATOR_TOOLS_JSON
` + string(encoded), nil
}
