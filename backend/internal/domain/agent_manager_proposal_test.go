package domain

import (
	"strings"
	"testing"
	"time"
)

func TestAgentManagerProposalStrictProtocol(t *testing.T) {
	valid := `{"schemaVersion":1,"action":"select_existing","agentTypeId":"specialist","agentTypeVersion":2,"rationale":"Suitable native capabilities","candidates":[{"agentTypeId":"specialist","version":2,"reason":"Matches the required capability"}]}`
	definition, err := ParseAgentManagerProposal(valid)
	if err != nil || definition.AgentTypeID != "specialist" {
		t.Fatalf("valid protocol: %+v %v", definition, err)
	}
	for _, raw := range []string{"", "Choose specialist", "```json\n" + valid + "\n```", "null", "[]", "{}", valid + " {}", strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":2`, 1), strings.Replace(valid, `"agentTypeVersion":2`, `"agentTypeVersion":0`, 1), strings.Replace(valid, `"rationale":`, `"origin":"USER","rationale":`, 1), strings.Replace(valid, `"select_existing"`, `"create_unrestricted"`, 1), strings.Repeat("x", (64<<10)+1)} {
		if parsed, err := ParseAgentManagerProposal(raw); err == nil || parsed != nil {
			t.Fatalf("accepted malformed proposal: %q", raw[:min(len(raw), 100)])
		}
	}
	escalation := `{"schemaVersion":1,"action":"needs_human","rationale":"No permitted match","candidates":[]}`
	if _, err := ParseAgentManagerProposal(escalation); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{strings.Replace(escalation, `"candidates":[]`, `"candidates":null`, 1), strings.Replace(escalation, `,"candidates":[]`, "", 1)} {
		if _, err := ParseAgentManagerProposal(raw); err == nil {
			t.Fatal("nullable/missing candidate list escaped the wire contract")
		}
	}
	definition.Candidates = append(definition.Candidates, definition.Candidates[0])
	if err := definition.Validate(); err == nil {
		t.Fatal("duplicate candidate explanations")
	}
}

func TestAgentManagerProposalSealsExactOutputAndParserOutcome(t *testing.T) {
	hash := strings.Repeat("a", 64)
	p := AgentManagerProposal{ID: "proposal", RequestID: "request", Number: 1, ControllerID: "controller", SessionID: "manager", NativeGeneration: "generation", ConfigurationHash: hash, RequestHash: hash, Raw: "", ValidationError: "Empty output", CreatedAt: time.Now().UTC()}
	p.ContentHash = p.Hash()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.ValidationError = "Different parser outcome"
	if err := p.Validate(); err == nil {
		t.Fatal("parser rejection changed without resealing")
	}
	p.ContentHash = p.Hash()
	p.Raw = "new output"
	if err := p.Validate(); err == nil {
		t.Fatal("native output changed without resealing")
	}
}
