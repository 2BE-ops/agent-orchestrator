package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestManagerRegistryActionValidationAndSealedReceipt(t *testing.T) {
	action := AgentManagerRegistryAction{Action: "create", Kind: RegistrySkill, Name: "Parser tests", Definition: RegistryDefinition{Skill: &SkillDefinition{Instructions: "Verify binary boundaries"}}, Reason: "Existing Skills lack parser checks"}
	_, hash, err := action.Definition.MarshalContent(action.Kind)
	if err != nil {
		t.Fatal(err)
	}
	r := AgentManagerRegistryReceipt{SchemaVersion: 1, ID: "receipt", ProjectID: "project", RequestID: "request", RequestHash: hash, ConfigurationHash: hash, ControllerID: "controller", SessionID: "session", NativeGeneration: "native", WorkerConfigurationHash: hash, ContextID: "context", ContextHash: hash, ConversationContextHash: hash, Classification: ContextTechnical, EngagementID: "client-a", Action: action, Target: WorkerDefinitionRef{ID: "skill", Version: 1, Name: action.Name, ContentHash: hash}, MetadataRevision: 1, CreatedAt: time.Now().UTC()}
	r.ContentHash = r.Hash()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AgentManagerRegistryReceipt
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Validate() != nil || decoded.ContentHash != r.ContentHash {
		t.Fatalf("receipt does not round-trip: %+v %v", decoded, err)
	}
	for _, name := range []string{"label changed", "classified authored content", "unknown schema", "wrong content", "creation version", "creation name", "scope", "unsealed provenance"} {
		t.Run(name, func(t *testing.T) {
			mutated := r
			switch name {
			case "label changed", "classified authored content":
				mutated.Classification = ContextMission
			case "unknown schema":
				mutated.SchemaVersion++
			case "wrong content":
				mutated.Target.ContentHash = strings.Repeat("0", 64)
			case "creation version":
				mutated.Target.Version++
			case "creation name":
				mutated.Target.Name = "Other"
			case "scope":
				mutated.EngagementID = "\nprivate"
			case "unsealed provenance":
				mutated.ConversationContextHash = ""
			}
			if name != "label changed" {
				mutated.ContentHash = mutated.Hash()
			}
			if mutated.Validate() == nil {
				t.Fatal("forged receipt accepted")
			}
		})
	}
	for _, name := range []string{"human identity", "ownership mutation", "existing id", "version metadata", "floating version", "wrong kind", "missing instructions", "unknown clearance", "oversized escaped output"} {
		t.Run(name, func(t *testing.T) {
			mutated := action
			switch name {
			case "human identity":
				mutated.Action = "USER"
			case "ownership mutation":
				mutated.Action = "update_metadata"
			case "existing id":
				mutated.EntryID = "protected"
			case "version metadata":
				mutated.Action, mutated.EntryID, mutated.ExpectedRevision = "append_version", "skill", 1
			case "floating version":
				mutated.Action, mutated.EntryID, mutated.Name = "append_version", "skill", ""
			case "wrong kind":
				mutated.Kind = RegistryAgentType
			case "missing instructions":
				mutated.Definition = RegistryDefinition{Skill: &SkillDefinition{}}
			case "unknown clearance":
				mutated.Kind, mutated.Definition = RegistryAgentType, RegistryDefinition{AgentType: &AgentTypeDefinition{Harness: HarnessCodex, MaxParallelWorkers: 1, MaxContextClass: "public"}}
			case "oversized escaped output":
				// Within the ordinary Skill instruction limit, but JSON escaping
				// makes the native action exceed its separate 256 KiB bound.
				mutated.Definition = RegistryDefinition{Skill: &SkillDefinition{Instructions: strings.Repeat("<", 65536)}}
			}
			if mutated.Validate() == nil {
				t.Fatal("invalid native action accepted")
			}
		})
	}
}
