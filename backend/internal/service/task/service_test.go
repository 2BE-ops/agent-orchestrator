package task_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestTaskServiceChecksRequestedDefinitionAndExactVersion(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpen(t)
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	mutation := domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Author task configuration"}
	for _, item := range []struct {
		id         string
		kind       domain.RegistryKind
		definition domain.RegistryDefinition
	}{
		{"type", domain.RegistryAgentType, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, MaxParallelWorkers: 1}}},
		{"skill", domain.RegistrySkill, domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Retain evidence"}}},
	} {
		if _, err := s.CreateRegistryEntry(ctx, item.id, item.kind, domain.RegistryMetadata{Name: item.id, Enabled: true}, item.definition, mutation); err != nil {
			t.Fatal(err)
		}
	}
	svc := tasksvc.New(s)
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	input := tasksvc.CreateInput{Definition: domain.TaskDefinition{Title: "Explicit worker", Brief: "Retain exact requested version", MaxAttempts: 2}, Reason: "Author work"}
	for _, selection := range []domain.WorkerSelection{{AgentTypeID: "missing"}, {AgentTypeID: "skill", Version: 1}, {AgentTypeID: "type", Version: 2}} {
		input.Definition.RequestedWorker = &selection
		_, err := svc.Create(ctx, actor, "project", input)
		var problem *apierr.Error
		if !errors.As(err, &problem) || problem.Kind != apierr.KindNotFound {
			t.Fatalf("invalid selection accepted: %+v %v", selection, err)
		}
	}
	input.Definition.RequestedWorker = &domain.WorkerSelection{AgentTypeID: "type", Version: 1}
	view, err := svc.Create(ctx, actor, "project", input)
	if err != nil || view.Revision.Definition.RequestedWorker.Version != 1 || view.Criteria != nil {
		t.Fatalf("planned view: %+v %v", view, err)
	}
	if _, _, err := s.ReserveTask(ctx, domain.TaskReservation{ID: "premature", TaskID: view.Task.ID, LaunchIntentID: "premature", HolderID: "scheduler", Mutation: domain.TaskMutation{Actor: actor, ExpectedRevision: 1, Reason: "Try unfrozen work"}, Now: time.Now().UTC(), TTL: time.Minute}); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("planned work started without criteria: %v", err)
	}
}

type unavailableTaskStore struct {
	tasksvc.Store
	err error
}

func (s unavailableTaskStore) GetAdaptiveTask(context.Context, string) (domain.AdaptiveTask, error) {
	return domain.AdaptiveTask{}, s.err
}

func TestTaskServicePreservesOperationalReadFailure(t *testing.T) {
	failure := errors.New("database unavailable")
	svc := tasksvc.New(unavailableTaskStore{err: failure})
	if _, err := svc.Get(context.Background(), "task"); !errors.Is(err, failure) {
		t.Fatalf("read failure disguised as absence: %v", err)
	}
}
