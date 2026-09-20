package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestTaskReviewPolicyPlanningPinsAndRollback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	if _, err := s.CreateRegistryEntry(ctx, "reviewer", domain.RegistryAgentType, registryMetadata("Reviewer"), registryAgentDefinition(), registryMutation(domain.RegistryUser, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRegistryEntry(ctx, "skill", domain.RegistrySkill, registryMetadata("Skill"), domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Review"}}, registryMutation(domain.RegistryUser, 0)); err != nil {
		t.Fatal(err)
	}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "review", Requirement: "No blocking findings", EvidenceKind: "review"}}, ReviewPolicy: &domain.TaskReviewPolicy{AgentTypeID: "reviewer", Version: 1, DifferentAgentType: true}}
	for _, policy := range []domain.TaskReviewPolicy{{AgentTypeID: "missing", Version: 1}, {AgentTypeID: "skill", Version: 1}, {AgentTypeID: "reviewer", Version: 2}} {
		bad := criteria
		bad.ReviewPolicy = &policy
		if _, err := s.CreateAdaptiveTask(ctx, "invalid", "project", taskDefinition(), &bad, taskMutation(0)); !errors.Is(err, ports.ErrTaskInvalid) {
			t.Fatalf("invalid pin accepted: %+v %v", policy, err)
		}
		if _, err := s.GetAdaptiveTask(ctx, "invalid"); !errors.Is(err, ports.ErrTaskNotFound) {
			t.Fatalf("partial task survived: %v", err)
		}
		if audit, err := s.ListTaskAudit(ctx, "invalid", 0, 100); err != nil || len(audit) != 0 {
			t.Fatalf("partial audit: %+v %v", audit, err)
		}
	}
	if _, err := s.CreateAdaptiveTask(ctx, "task", "project", taskDefinition(), &criteria, taskMutation(0)); err != nil {
		t.Fatal(err)
	}
	attempt, _, err := s.ReserveTask(ctx, domain.TaskReservation{ID: "attempt", TaskID: "task", LaunchIntentID: "launch", HolderID: "scheduler", Mutation: taskMutation(1), Now: time.Now().UTC(), TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendRegistryVersion(ctx, "reviewer", registryAgentDefinition(), registryMutation(domain.RegistryUser, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ActivateRegistryVersion(ctx, "reviewer", 2, registryMutation(domain.RegistryUser, 2)); err != nil {
		t.Fatal(err)
	}
	criteria.ReviewPolicy.Version = 2
	if _, err := s.ReviseAcceptanceCriteria(ctx, "task", criteria, taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	metadata := registryMetadata("Disabled reviewer")
	metadata.Enabled = false
	if _, err := s.UpdateRegistryMetadata(ctx, "reviewer", metadata, registryMutation(domain.RegistryUser, 3)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReviseAcceptanceCriteria(ctx, "task", criteria, taskMutation(2)); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("disabled reviewer accepted for new planning: %v", err)
	}
	worker := taskMutation(2)
	worker.Actor.Kind = "WORKER"
	criteria.ReviewPolicy = nil
	if _, err := s.ReviseAcceptanceCriteria(ctx, "task", criteria, worker); !errors.Is(err, ports.ErrTaskForbidden) {
		t.Fatalf("worker removed review policy: %v", err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	old, err := reopened.GetAcceptanceCriteria(ctx, "task", attempt.CriteriaVersion)
	if err != nil || old.Definition.ReviewPolicy == nil || old.Definition.ReviewPolicy.Version != 1 || !old.Definition.ReviewPolicy.DifferentAgentType {
		t.Fatalf("attempt's review policy changed: %+v %v", old, err)
	}
	current, err := reopened.GetAcceptanceCriteria(ctx, "task", 2)
	if err != nil || current.Definition.ReviewPolicy.Version != 2 || old.ContentHash == current.ContentHash {
		t.Fatalf("revised policy lost: %+v %v", current, err)
	}
	if task, err := reopened.GetAdaptiveTask(ctx, "task"); err != nil || task.Revision != 2 {
		t.Fatalf("rejected policy mutated planning: %+v %v", task, err)
	}
}
