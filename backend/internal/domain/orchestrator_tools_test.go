package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOrchestratorToolsSealLiteralProtocolV1(t *testing.T) {
	c := OrchestratorProtocolContext{ProjectID: "project with spaces", GoalVersion: 3, GoalHash: "0000000000000000000000000000000000000000000000000000000000000123", Tools: &OrchestratorToolPaths{SchemaVersion: 1, Executable: `C:\AO tools\ao $name.exe`, RunFile: `C:\AO data\running.json`}}
	text, err := c.ToolPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if hash := ContextTextHash(text); hash != "3ace095c02ff39120066f97d238d59da480b0f5e19021aa56dcc577803ae82d4" {
		t.Fatalf("protocol v1 bytes drifted: %s", hash)
	}
	_, encoded, found := strings.Cut(text, "AO_ORCHESTRATOR_TOOLS_JSON\n")
	if !found {
		t.Fatal("sealed prompt lacks protocol JSON")
	}
	var protocol struct {
		SchemaVersion int                                                    `json:"schemaVersion"`
		Goal          map[string]int64                                       `json:"goal"`
		Commands      map[string][]string                                    `json:"commands"`
		Environment   map[string]string                                      `json:"environment"`
		PlanExample   struct{ SourceGeneration, IdempotencyKey, Raw string } `json:"planExample"`
	}
	if err := json.Unmarshal([]byte(encoded), &protocol); err != nil {
		t.Fatal(err)
	}
	if protocol.SchemaVersion != 1 || protocol.Goal["version"] != 3 {
		t.Fatalf("goal pin lost: %+v", protocol)
	}
	plan := protocol.Commands["plan"]
	if len(plan) != 6 || plan[0] != c.Tools.Executable || plan[3] != string(c.ProjectID) || plan[4] != "--file" || plan[5] != "-" || protocol.Environment["AO_RUN_FILE"] != c.Tools.RunFile {
		t.Fatalf("literal routing lost: %+v", protocol)
	}
	var action OrchestratorPlanAction
	if err := json.Unmarshal([]byte(protocol.PlanExample.Raw), &action); err != nil {
		t.Fatalf("example is not a plan action: %v", err)
	}
	if err := action.Validate(); err != nil && !strings.Contains(err.Error(), "REPLACE_WITH") {
		t.Fatalf("example validation: %v", err)
	}
}

func TestOrchestratorToolsRefuseUnpinnedInput(t *testing.T) {
	c := OrchestratorProtocolContext{ProjectID: "project", GoalVersion: 1, GoalHash: "short", Tools: &OrchestratorToolPaths{SchemaVersion: 1, Executable: "ao"}}
	if _, err := c.ToolPrompt(); err == nil {
		t.Fatal("unpinned goal accepted")
	}
	c.GoalHash = "0000000000000000000000000000000000000000000000000000000000000123"
	c.Tools.SchemaVersion = 2
	if _, err := c.ToolPrompt(); err == nil {
		t.Fatal("unknown schema version accepted")
	}
	c.Tools = nil
	text, err := c.ToolPrompt()
	if err != nil || text != "" {
		t.Fatalf("missing tools must render nothing: %q %v", text, err)
	}
}
