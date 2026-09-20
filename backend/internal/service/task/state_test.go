package task_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestTaskStateNeverTreatsExpiredLeaseOrCancellationAsStopped(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	svc := tasksvc.New(s)
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	input := tasksvc.CreateInput{Definition: domain.TaskDefinition{Title: "Keep ownership", Brief: "Cancellation is intent", MaxAttempts: 1}, Reason: "Author bounded work"}
	view, err := svc.Create(ctx, actor, "project", input)
	if err != nil || view.State.Phase != "planned" {
		t.Fatalf("unfrozen state: %+v %v", view.State, err)
	}
	id := view.Task.ID
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "review", Requirement: "Verify boundary", EvidenceKind: "review"}}}
	if _, err := svc.ReviseCriteria(ctx, actor, id, tasksvc.CriteriaInput{Criteria: criteria, ExpectedRevision: 1, Reason: "Freeze criteria"}); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Get(ctx, id)
	if err != nil || view.State.Phase != "ready" {
		t.Fatalf("ready state: %+v %v", view.State, err)
	}
	_, lease, err := s.ReserveTask(ctx, domain.TaskReservation{ID: "attempt", TaskID: id, LaunchIntentID: "launch", HolderID: "scheduler", Mutation: domain.TaskMutation{Actor: actor, ExpectedRevision: 2, Reason: "Reserve work"}, Now: time.Now().Add(-time.Hour), TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.Get(ctx, id)
	if err != nil || view.State.Phase != "leased" || !view.State.RequiresReconciliation {
		t.Fatalf("expired lease became free: %+v %v", view.State, err)
	}
	if _, err := svc.ChangeIntent(ctx, actor, id, tasksvc.IntentInput{Intent: "cancel", ExpectedRevision: 2, Reason: "Cancel requested work"}); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Get(ctx, id)
	if err != nil || view.State.Phase != "cancelling" || view.Lease == nil || view.State.CancelledBy != id {
		t.Fatalf("cancellation lost ownership: %+v %v", view, err)
	}
	if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, Reason: "Unseeded reservation has no native process", Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Get(ctx, id)
	if err != nil || view.State.Phase != "cancelled" || view.State.RequiresReconciliation {
		t.Fatalf("released cancellation: %+v %v", view.State, err)
	}
	if _, err := svc.ChangeIntent(ctx, actor, id, tasksvc.IntentInput{Intent: "run", ExpectedRevision: 2, ExpectedVersion: 1, Reason: "Request another attempt"}); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Get(ctx, id)
	if err != nil || view.State.Phase != "failed" {
		t.Fatalf("resume bypassed retry limit: %+v %v", view.State, err)
	}
}

func TestTaskStateRequiresDependencyVerificationAndRespectsParentCancellation(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	svc := tasksvc.New(s)
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	input := tasksvc.CreateInput{Definition: domain.TaskDefinition{Title: "Parent", Brief: "Plan dependencies", MaxAttempts: 2}, Criteria: &domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "review", Requirement: "Verify dependency", EvidenceKind: "review"}}}, Reason: "Author work"}
	parent, err := svc.Create(ctx, actor, "project", input)
	if err != nil {
		t.Fatal(err)
	}
	input.Definition.Dependencies = []string{parent.Task.ID}
	input.Definition.ParentID = parent.Task.ID
	child, err := svc.Create(ctx, actor, "project", input)
	if err != nil || child.State.Phase != "blocked" {
		t.Fatalf("unverified dependency ready: %+v %v", child.State, err)
	}
	if _, err := svc.ChangeIntent(ctx, actor, parent.Task.ID, tasksvc.IntentInput{Intent: "cancel", ExpectedRevision: 1, Reason: "Cancel subtree"}); err != nil {
		t.Fatal(err)
	}
	child, err = svc.Get(ctx, child.Task.ID)
	if err != nil || child.Intent.Intent != "run" || child.State.Phase != "cancelled" || child.State.CancelledBy != parent.Task.ID {
		t.Fatalf("inherited cancellation: %+v %v", child, err)
	}
}

func TestTaskStateSurfacesNeedsHumanForTaskAndDescendants(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	svc := tasksvc.New(s)
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	input := tasksvc.CreateInput{Definition: domain.TaskDefinition{Title: "Parent", Brief: "Plan dependencies", MaxAttempts: 2}, Criteria: &domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "review", Requirement: "Verify dependency", EvidenceKind: "review"}}}, Reason: "Author work"}
	parent, err := svc.Create(ctx, actor, "project", input)
	if err != nil {
		t.Fatal(err)
	}
	input.Definition.ParentID = parent.Task.ID
	input.Definition.Dependencies = []string{parent.Task.ID}
	child, err := svc.Create(ctx, actor, "project", input)
	if err != nil || child.State.Phase != "blocked" {
		t.Fatalf("unverified dependency ready: %+v %v", child.State, err)
	}
	if _, err := s.RaiseTaskNeedsHuman(ctx, parent.Task.ID, domain.TaskNeedsHuman{ID: "nh-1", ReasonCode: "credential_missing", Detail: "The provider credential expired", Actor: actor, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	parent, err = svc.Get(ctx, parent.Task.ID)
	if err != nil || parent.State.Phase != "needs-human" || parent.State.NeedsHumanTaskID != parent.Task.ID || !strings.Contains(parent.State.Reason, "credential_missing") {
		t.Fatalf("needs human phase: %+v %v", parent.State, err)
	}
	child, err = svc.Get(ctx, child.Task.ID)
	if err != nil || child.State.Phase != "blocked" || child.State.NeedsHumanTaskID != parent.Task.ID {
		t.Fatalf("descendant not blocked: %+v %v", child.State, err)
	}
	if _, err := s.ResolveTaskNeedsHuman(ctx, parent.Task.ID, domain.TaskNeedsHumanResolution{Resolution: "Rotated the credential", Actor: actor, ResolvedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	parent, err = svc.Get(ctx, parent.Task.ID)
	if err != nil || parent.State.Phase == "needs-human" {
		t.Fatalf("resolved task still needs human: %+v %v", parent.State, err)
	}
	child, err = svc.Get(ctx, child.Task.ID)
	if err != nil || child.State.NeedsHumanTaskID != "" {
		t.Fatalf("resolved descendant still blocked: %+v %v", child.State, err)
	}
}
