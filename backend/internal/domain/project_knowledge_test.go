package domain

import (
	"strings"
	"testing"
)

func TestKnowledgeDefinitionBoundsReviewAndProvenance(t *testing.T) {
	valid := func() KnowledgeDefinition {
		return KnowledgeDefinition{Title: "Convention", Kind: "convention", Content: "Preserve service boundaries", Status: "candidate", Confidence: "medium", Sources: []KnowledgeSource{{Kind: "user", Reference: "Reviewed design"}}}
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*KnowledgeDefinition)
	}{
		{"empty", func(d *KnowledgeDefinition) { d.Content = " " }},
		{"large", func(d *KnowledgeDefinition) { d.Content = strings.Repeat("x", 16385) }},
		{"pinned_candidate", func(d *KnowledgeDefinition) { d.Pinned = true }},
		{"missing_source", func(d *KnowledgeDefinition) { d.Sources = nil }},
		{"unowned_worker", func(d *KnowledgeDefinition) { d.Sources[0].Kind = "worker" }},
		{"bad_hash", func(d *KnowledgeDefinition) { d.Sources[0].Commit = strings.Repeat("g", 40) }},
		{"missing_replacement", func(d *KnowledgeDefinition) { d.Status = "superseded" }},
		{"duplicate_relevance", func(d *KnowledgeDefinition) { d.TaskIDs = []string{"task", "task"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := valid()
			tc.change(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("invalid knowledge accepted")
			}
		})
	}
}
