package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentManagerToolsRetainLiteralNativeIdentityAndEnvelope(t *testing.T) {
	c := managerContextFixture(t)
	c.ProjectID = "project with spaces"
	c.Tools = &AgentManagerToolPaths{SchemaVersion: 1, Executable: `C:\AO tools\ao $name.exe`, RunFile: `C:\AO data\running.json`}
	var err error
	c.Prompt, err = c.RenderPrompt()
	if err != nil {
		t.Fatal(err)
	}
	c.ContentHash = c.Hash()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	_, encoded, found := strings.Cut(c.Prompt, "AO_MANAGER_TOOLS_JSON\n")
	if !found {
		t.Fatal("sealed input lacks native protocol")
	}
	var protocol struct {
		Commands        map[string][]string                                    `json:"commands"`
		Environment     map[string]string                                      `json:"environment"`
		ProposalExample struct{ SourceGeneration, IdempotencyKey, Raw string } `json:"proposalExample"`
	}
	if err := json.Unmarshal([]byte(encoded), &protocol); err != nil {
		t.Fatal(err)
	}
	argv := protocol.Commands["propose"]
	if len(argv) != 7 || argv[0] != c.Tools.Executable || argv[3] != string(c.SessionID) || argv[4] != c.RequestID || argv[6] != "-" || protocol.Environment["AO_RUN_FILE"] != c.Tools.RunFile || protocol.ProposalExample.SourceGeneration != c.NativeGeneration {
		t.Fatalf("native identity or literal routing lost: %+v", protocol)
	}
	if _, err := ParseAgentManagerProposal(protocol.ProposalExample.Raw); err != nil {
		t.Fatalf("example cannot enter strict parser: %v", err)
	}
	if !strings.Contains(c.Prompt, "maxProposalAttempts") || !strings.Contains(c.Prompt, "OWNER_CHANGED") {
		t.Fatal("bounded correction and stale-owner rules missing")
	}
	text, err := c.toolPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if ContextTextHash(text) != "7c2458b28a239ae4156aa2ece4e5144676bfb227c4acfa1d557ac8f8f52d6cc5" {
		t.Fatal("protocol v1 changed; add a new renderer version instead of rewriting retained input")
	}
}

func TestAgentManagerToolPathsRejectMissingExecutableAndControls(t *testing.T) {
	for _, paths := range []AgentManagerToolPaths{{}, {SchemaVersion: 1, Executable: "ao\nother"}, {SchemaVersion: 1, Executable: "ao", RunFile: "file\x00"}, {SchemaVersion: 1, Executable: strings.Repeat("a", 4097)}, {SchemaVersion: 5, Executable: "ao"}, {SchemaVersion: 1, Executable: "ao", RunFile: string([]byte{0xff})}} {
		if err := paths.Validate(); err == nil {
			t.Fatalf("unsafe native routing accepted: %+v", paths)
		}
	}
}

func TestAgentManagerCandidateProtocolRetainsV1AndScopesV2(t *testing.T) {
	c := managerContextFixture(t)
	c.ProjectID = "project with spaces"
	c.Tools = &AgentManagerToolPaths{SchemaVersion: 2, Executable: `C:\AO tools\ao $name.exe`, RunFile: `C:\AO data\running.json`}
	var err error
	c.Prompt, err = c.RenderPrompt()
	if err != nil {
		t.Fatal(err)
	}
	c.ContentHash = c.Hash()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	_, encoded, found := strings.Cut(c.Prompt, "AO_MANAGER_TOOLS_JSON\n")
	if !found {
		t.Fatal("missing protocol")
	}
	var protocol struct {
		SchemaVersion int                 `json:"schemaVersion"`
		Commands      map[string][]string `json:"commands"`
	}
	if err := json.Unmarshal([]byte(encoded), &protocol); err != nil {
		t.Fatal(err)
	}
	argv := protocol.Commands["candidates"]
	if protocol.SchemaVersion != 2 || len(argv) != 7 || argv[0] != c.Tools.Executable || argv[3] != string(c.ProjectID) || argv[4] != c.RequestID || argv[6] != "20" || len(protocol.Commands["candidate"]) != 8 {
		t.Fatalf("candidate routing lost: %+v", protocol)
	}
	if !strings.Contains(c.Prompt, "nextCursor") || !strings.Contains(c.Prompt, "not a semantic quality ranking") {
		t.Fatal("bounded semantic selection rules missing")
	}
	text, err := c.toolPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if hash := ContextTextHash(text); hash != "190ac41041b5e290f80c0e5942ed8f1f6cdea73c499bc9c740fca1716e494831" {
		t.Fatalf("protocol v2 hash: %s", hash)
	}
}

func TestAgentManagerDecisionProtocolRetainsLiteralFeedbackTools(t *testing.T) {
	c := managerContextFixture(t)
	c.ProjectID = "project with spaces"
	c.Tools = &AgentManagerToolPaths{SchemaVersion: 3, Executable: `C:\AO tools\ao $name.exe`, RunFile: `C:\AO data\running.json`}
	var err error
	c.Prompt, err = c.RenderPrompt()
	if err != nil {
		t.Fatal(err)
	}
	c.ContentHash = c.Hash()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	_, encoded, _ := strings.Cut(c.Prompt, "AO_MANAGER_TOOLS_JSON\n")
	var protocol struct {
		SchemaVersion int                 `json:"schemaVersion"`
		Commands      map[string][]string `json:"commands"`
	}
	if err := json.Unmarshal([]byte(encoded), &protocol); err != nil {
		t.Fatal(err)
	}
	argv := protocol.Commands["decision"]
	if protocol.SchemaVersion != 3 || len(argv) != 6 || argv[0] != c.Tools.Executable || argv[3] != string(c.ProjectID) || argv[4] != c.RequestID || len(protocol.Commands["decisions"]) != 5 || len(protocol.Commands["candidates"]) != 7 {
		t.Fatalf("feedback routing lost: %+v", protocol)
	}
	if !strings.Contains(c.Prompt, "routingOutcome") || !strings.Contains(c.Prompt, "Parser and\nsemantic failures share that budget") {
		t.Fatal("terminal/correction semantics absent")
	}
	text, err := c.toolPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if hash := ContextTextHash(text); hash != "1c37e79d61ea4cf1f86c931f5a0967d6b931ec222c7156d0b9e16fe0490e2e55" {
		t.Fatalf("protocol v3 hash: %s", hash)
	}
}

func TestAgentManagerRegistryProtocolAddsGovernedAuthoringTools(t *testing.T) {
	c := managerContextFixture(t)
	c.ProjectID = "project with spaces"
	c.Tools = &AgentManagerToolPaths{SchemaVersion: 4, Executable: `C:\AO tools\ao $name.exe`, RunFile: `C:\AO data\running.json`}
	var err error
	c.Prompt, err = c.RenderPrompt()
	if err != nil {
		t.Fatal(err)
	}
	c.ContentHash = c.Hash()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	_, encoded, _ := strings.Cut(c.Prompt, "AO_MANAGER_TOOLS_JSON\n")
	var protocol struct {
		SchemaVersion int                 `json:"schemaVersion"`
		Commands      map[string][]string `json:"commands"`
	}
	if err := json.Unmarshal([]byte(encoded), &protocol); err != nil {
		t.Fatal(err)
	}
	author := protocol.Commands["registryAuthor"]
	receipts := protocol.Commands["registryReceipts"]
	if protocol.SchemaVersion != 4 || len(author) != 7 || author[0] != c.Tools.Executable || author[3] != string(c.SessionID) || author[4] != c.RequestID || author[6] != "-" || len(receipts) != 7 || receipts[3] != string(c.ProjectID) || receipts[4] != c.RequestID || receipts[6] != "100" || len(protocol.Commands["registryReceipt"]) != 6 || len(protocol.Commands["decision"]) != 6 || len(protocol.Commands["candidates"]) != 7 || len(protocol.Commands["propose"]) != 7 {
		t.Fatalf("authoring routing or retained prior tools lost: %+v", protocol)
	}
	if !strings.Contains(c.Prompt, "you never choose entry IDs") || !strings.Contains(c.Prompt, "never activates it") || !strings.Contains(c.Prompt, "engagement or mission material") {
		t.Fatal("governed authoring rules missing")
	}
	text, err := c.toolPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if hash := ContextTextHash(text); hash != "02efc6fbb28327111dcaacc3e207731406c77d2dafcf0aad4046fdc195e922df" {
		t.Fatalf("protocol v4 hash: %s", hash)
	}
}
