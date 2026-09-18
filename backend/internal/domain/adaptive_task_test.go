package domain_test

import (
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestAdaptiveTaskInputBounds(t *testing.T) {
	valid := func() domain.TaskDefinition {
		return domain.TaskDefinition{Title: "Work", Brief: "Meet the criteria", MaxAttempts: 2}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*domain.TaskDefinition)
	}{
		{"empty brief", func(d *domain.TaskDefinition) { d.Brief = " " }},
		{"large brief", func(d *domain.TaskDefinition) { d.Brief = strings.Repeat("x", 32001) }},
		{"duplicate dependency", func(d *domain.TaskDefinition) { d.Dependencies = []string{"a", "a"} }},
		{"invalid capability", func(d *domain.TaskDefinition) { d.RequiredCapabilities = []string{""} }},
		{"unbounded retries", func(d *domain.TaskDefinition) { d.MaxAttempts = 11 }},
		{"missing worker identity", func(d *domain.TaskDefinition) { d.RequestedWorker = &domain.WorkerSelection{} }},
		{"negative version", func(d *domain.TaskDefinition) {
			d.RequestedWorker = &domain.WorkerSelection{AgentTypeID: "worker", Version: -1}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := valid()
			tc.mutate(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("invalid definition accepted")
			}
		})
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptanceCriteriaBoundedEvidence(t *testing.T) {
	valid := func() domain.AcceptanceCriteria {
		return domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "test", Requirement: "Tests pass", EvidenceKind: "test", Command: []string{"go", "test", "./..."}}}}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*domain.AcceptanceCriteria)
	}{
		{"empty", func(c *domain.AcceptanceCriteria) { c.Criteria = nil }},
		{"duplicate", func(c *domain.AcceptanceCriteria) { c.Criteria = append(c.Criteria, c.Criteria[0]) }},
		{"empty requirement", func(c *domain.AcceptanceCriteria) { c.Criteria[0].Requirement = " " }},
		{"unknown verifier", func(c *domain.AcceptanceCriteria) { c.Criteria[0].EvidenceKind = "worker-says-done" }},
		{"missing artifact", func(c *domain.AcceptanceCriteria) { c.Criteria[0].EvidenceKind = "artifact" }},
		{"missing executable", func(c *domain.AcceptanceCriteria) { c.Criteria[0].Command = []string{""} }},
		{"nul argument", func(c *domain.AcceptanceCriteria) { c.Criteria[0].Command = []string{"go", "test\x00"} }},
		{"large argument", func(c *domain.AcceptanceCriteria) { c.Criteria[0].Command = []string{strings.Repeat("x", 2001)} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("invalid criteria accepted")
			}
		})
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
}
