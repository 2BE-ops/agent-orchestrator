package domain_test

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func dryRunAction(action string) domain.OrchestratorPlanAction {
	item := domain.OrchestratorPlanAction{Action: action, Reason: "rehearse the plan"}
	switch action {
	case "create_task":
		item.Definition = &domain.TaskDefinition{
			Title:       "Ship the checklist",
			Brief:       "Update the shipping checklist for the release",
			Category:    "chore",
			MaxAttempts: 3,
		}
	case "revise_task":
		item.TaskID = "task-1"
		item.ExpectedRevision = 1
		item.Definition = &domain.TaskDefinition{
			Title:       "Ship the checklist",
			Brief:       "Narrower brief",
			Category:    "chore",
			MaxAttempts: 3,
		}
	case "freeze_criteria":
		item.TaskID = "task-1"
		item.ExpectedRevision = 1
		item.Criteria = &domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{
			{ID: "tests", Requirement: "Tests pass", EvidenceKind: "test"},
		}}
	}
	return item
}

func TestDryRunRequestValidate(t *testing.T) {
	request := domain.DryRunRequest{Actions: []domain.OrchestratorPlanAction{
		dryRunAction("create_task"),
		dryRunAction("revise_task"),
		dryRunAction("freeze_criteria"),
	}}
	if err := request.Validate(); err != nil {
		t.Fatalf("valid dry run rejected: %v", err)
	}
	empty := domain.DryRunRequest{}
	if err := empty.Validate(); err == nil {
		t.Fatal("empty dry run accepted")
	}
	var many []domain.OrchestratorPlanAction
	for i := 0; i < domain.DryRunActionLimit+1; i++ {
		many = append(many, dryRunAction("create_task"))
	}
	if err := (domain.DryRunRequest{Actions: many}).Validate(); err == nil {
		t.Fatal("oversized dry run accepted")
	}
	broken := domain.DryRunRequest{Actions: []domain.OrchestratorPlanAction{
		{Action: "create_task", Definition: &domain.TaskDefinition{Title: "x", Brief: "y", Category: "z", MaxAttempts: 3}, Reason: ""},
	}}
	if err := broken.Validate(); err == nil {
		t.Fatal("empty reason accepted")
	}
}

func TestDryRunCostEstimateStaysUnknown(t *testing.T) {
	verdict := domain.DryRunVerdict{CostEstimate: domain.DryRunCostUnknown}
	if verdict.CostEstimate != "unknown" {
		t.Fatalf("dry run cost estimate must stay unknown, got %q", verdict.CostEstimate)
	}
}
