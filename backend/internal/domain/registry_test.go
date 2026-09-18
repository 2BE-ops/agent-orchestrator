package domain

import (
	"strings"
	"testing"
)

func TestRegistryDefinitionValidation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       RegistryKind
		definition RegistryDefinition
	}{
		{"missing", RegistryAgentType, RegistryDefinition{}},
		{"wrong union", RegistrySkill, RegistryDefinition{AgentType: &AgentTypeDefinition{Harness: HarnessCodex, MaxParallelWorkers: 1}}},
		{"unknown kind", "other", RegistryDefinition{Skill: &SkillDefinition{Instructions: "read"}}},
		{"unsupported harness", RegistryAgentType, RegistryDefinition{AgentType: &AgentTypeDefinition{Harness: "imaginary", MaxParallelWorkers: 1}}},
		{"unbounded population", RegistryAgentType, RegistryDefinition{AgentType: &AgentTypeDefinition{Harness: HarnessCodex, MaxParallelWorkers: 0}}},
		{"unsupported permission", RegistryAgentType, RegistryDefinition{AgentType: &AgentTypeDefinition{Harness: HarnessCodex, MaxParallelWorkers: 1, Config: AgentConfig{Permissions: "root"}}}},
		{"empty instructions", RegistrySkill, RegistryDefinition{Skill: &SkillDefinition{}}},
		{"oversized instructions", RegistrySkill, RegistryDefinition{Skill: &SkillDefinition{Instructions: strings.Repeat("x", 65537)}}},
		{"duplicate skills", RegistryAgentType, RegistryDefinition{AgentType: &AgentTypeDefinition{Harness: HarnessCodex, MaxParallelWorkers: 1, Skills: []SkillVersionRef{{ID: "x", Version: 1}, {ID: "x", Version: 2}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.definition.Validate(tc.kind); err == nil {
				t.Fatal("invalid definition accepted")
			}
		})
	}
}

func TestSkillResourcePathsArePortableAndContained(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "C:/secret", "..\\secret", "a/../b", "a//b", ".git/config", "CON", "nul.md", "a/LPT1.txt", "a/file.", "file:stream", "a\x00b", "a\x01b", "a\"b", "skill.MD"} {
		t.Run(name, func(t *testing.T) {
			d := RegistryDefinition{Skill: &SkillDefinition{Instructions: "reference", Resources: []SkillResource{{Path: name, Content: "inert"}}}}
			if err := d.Validate(RegistrySkill); err == nil {
				t.Fatal("unsafe resource accepted")
			}
		})
	}
	d := RegistryDefinition{Skill: &SkillDefinition{Instructions: "reference", Resources: []SkillResource{{Path: "references/guide.md", Content: "inert"}}}}
	first, hash, err := d.MarshalContent(RegistrySkill)
	if err != nil {
		t.Fatal(err)
	}
	second, hash2, err := d.MarshalContent(RegistrySkill)
	if err != nil || string(first) != string(second) || hash != hash2 || len(hash) != 64 {
		t.Fatal("content hash is not stable")
	}
	d.Skill.Resources = append(d.Skill.Resources, SkillResource{Path: "REFERENCES/GUIDE.md"})
	if err := d.Validate(RegistrySkill); err == nil {
		t.Fatal("case-insensitive path collision accepted")
	}
}

func TestRegistryActorAndMetadataValidation(t *testing.T) {
	if err := (RegistryMutation{Actor: RegistryActor{Origin: "user", ID: "x"}, Reason: "create"}).Validate(); err == nil {
		t.Fatal("unknown actor accepted")
	}
	if err := (RegistryMutation{Actor: RegistryActor{Origin: RegistryUser, ID: "x"}}).Validate(); err == nil {
		t.Fatal("missing audit reason accepted")
	}
	if err := (RegistryMetadata{Name: "  "}).Validate(); err == nil {
		t.Fatal("blank name accepted")
	}
}

func TestSkillResourceFileDirectoryConflicts(t *testing.T) {
	for _, paths := range [][]string{{"references", "references/guide.md"}, {"REFERENCES/guide.md", "references"}, {"SKILL.md/guide.md"}} {
		d := RegistryDefinition{Skill: &SkillDefinition{Instructions: "Reference"}}
		for _, path := range paths {
			d.Skill.Resources = append(d.Skill.Resources, SkillResource{Path: path})
		}
		if err := d.Validate(RegistrySkill); err == nil {
			t.Fatalf("accepted file/directory conflict: %v", paths)
		}
	}
}
