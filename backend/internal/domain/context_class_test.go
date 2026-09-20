package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContextClassificationLatticeAndEngagementIsolation(t *testing.T) {
	classes := []ContextClass{ContextTechnical, ContextEngagement, ContextMission}
	for clearanceIndex, clearance := range classes {
		for classIndex, class := range classes {
			if got := CanEmbedContext(clearance, "client-a", class, "client-a"); got != (classIndex <= clearanceIndex) {
				t.Fatalf("%s <- %s: %v", clearance, class, got)
			}
			if got := CanEmbedContext(clearance, "client-a", class, "client-b"); got != (class == ContextTechnical) {
				t.Fatalf("cross-engagement %s <- %s: %v", clearance, class, got)
			}
		}
	}
	if !CanEmbedContext("", "", "", "") || CanEmbedContext("unknown", "", ContextTechnical, "") || CanEmbedContext(ContextMission, "", "unknown", "") || CanEmbedContext(ContextMission, "", ContextEngagement, "") {
		t.Fatal("legacy/unknown/unscoped classification mishandled")
	}
	for _, id := range []string{" ", " client", "client ", "client\n", strings.Repeat("x", 201), string([]byte{0xff})} {
		if ValidateContextScope(ContextEngagement, id) == nil {
			t.Fatalf("invalid engagement accepted: %q", id)
		}
	}
}

func TestContextClassificationKeepsLegacyDefinitionBytes(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		target any
	}{
		{`{"title":"Work","brief":"Meet the criteria","category":"","priority":0,"dependencies":null,"requiredCapabilities":null,"maxAttempts":2}`, &TaskDefinition{}},
		{`{"title":"Fact","kind":"convention","content":"Use tests","status":"accepted","confidence":"high","pinned":false,"sources":[{"kind":"user","reference":"review"}]}`, &KnowledgeDefinition{}},
		{`{"agentType":{"harness":"codex","config":{},"instructions":"Review","capabilities":[],"skills":[],"maxParallelWorkers":1}}`, &RegistryDefinition{}},
	} {
		if err := json.Unmarshal([]byte(tc.raw), tc.target); err != nil {
			t.Fatal(err)
		}
		encoded, _, err := TaskContent(tc.target)
		if err != nil || string(encoded) != tc.raw {
			t.Fatalf("historical hash bytes changed: %s %v", encoded, err)
		}
	}
}

func TestClassifiedContextRejectsMislabelledManifestAndDelegation(t *testing.T) {
	s := validTaskContext(t)
	s.SchemaVersion = 2
	s.MaxContextClass, s.Classification = ContextTechnical, ContextTechnical
	s.SystemPrompt = "0123456789"
	s.SystemPromptHash = ContextTextHash(s.SystemPrompt)
	for i := range s.Sources {
		s.Sources[i].Classification = ContextTechnical
	}
	s.ContentHash = s.Hash()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*TaskContextSnapshot){
		func(s *TaskContextSnapshot) { s.Sources[0].Classification = ContextMission },
		func(s *TaskContextSnapshot) {
			s.MaxContextClass = ContextMission
			s.Classification = ContextMission
			s.EngagementID = "a"
			s.Sources[0].Classification = ContextEngagement
			s.Sources[0].EngagementID = "b"
		},
		func(s *TaskContextSnapshot) { s.Sources[0].Classification = "" },
		func(s *TaskContextSnapshot) { s.SystemPrompt = "tampered" },
		func(s *TaskContextSnapshot) { s.SchemaVersion = 1 },
	} {
		changed := s
		changed.Sources = append([]ContextSource{}, s.Sources...)
		change(&changed)
		changed.ContentHash = changed.Hash()
		if changed.Validate() == nil {
			t.Fatal("invalid classified manifest accepted")
		}
	}
	d := TaskDelegation{SchemaVersion: 1, AttemptID: s.AttemptID, Number: 1, SessionID: s.SessionID, ExecutionOperationID: s.ExecutionOperationID, ConfigurationHash: s.ConfigurationHash, ContextHash: s.ContentHash, MaxContextClass: s.MaxContextClass, Classification: s.Classification, SystemPrompt: s.SystemPrompt, Prompt: s.Prompt, CreatedAt: s.CreatedAt}
	d.ContentHash = d.Hash()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	d.Classification = ContextMission
	d.ContentHash = d.Hash()
	if d.Validate() == nil {
		t.Fatal("delegation exceeded clearance")
	}
}
