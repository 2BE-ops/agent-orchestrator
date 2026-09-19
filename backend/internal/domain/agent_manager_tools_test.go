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
	for _, paths := range []AgentManagerToolPaths{{}, {SchemaVersion: 1, Executable: "ao\nother"}, {SchemaVersion: 1, Executable: "ao", RunFile: "file\x00"}, {SchemaVersion: 1, Executable: strings.Repeat("a", 4097)}, {SchemaVersion: 3, Executable: "ao"}, {SchemaVersion: 1, Executable: "ao", RunFile: string([]byte{0xff})}} {
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
