package task_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

type evaluationServiceStore struct {
	tasksvc.Store
	readErr, writeErr error
	captured          *domain.TaskEvaluationRequest
}

func (s *evaluationServiceStore) GetTaskResult(context.Context, string) (domain.TaskResult, error) {
	return domain.TaskResult{ID: "result", TaskID: "task", AttemptID: "attempt"}, s.readErr
}
func (s *evaluationServiceStore) EvaluateTaskResult(_ context.Context, input domain.TaskEvaluationRequest) (domain.TaskEvaluation, bool, error) {
	s.captured = &input
	return domain.TaskEvaluation{ID: input.ID}, s.writeErr == nil, s.writeErr
}

func TestEvaluationServiceScopeAuthorityAndStorageFailures(t *testing.T) {
	s := &evaluationServiceStore{}
	m := tasksvc.New(s)
	ctx := context.Background()
	actor := domain.AdaptiveActor{Kind: "USER", ID: "caller"}
	input := tasksvc.EvaluationInput{ResultID: "result", IdempotencyKey: "evaluate", Reason: "Collect frozen evidence"}
	if receipt, err := m.Evaluate(ctx, actor, "task", "attempt", input); err != nil || !receipt.Created || s.captured == nil || s.captured.ID == "" || s.captured.Mutation.Actor != actor {
		t.Fatalf("lost service authority: %+v %v", s.captured, err)
	}
	s.captured = nil
	if _, err := m.Evaluate(ctx, actor, "other", "attempt", input); err == nil || s.captured != nil {
		t.Fatalf("cross-task evaluation reached write: %v", err)
	}
	broken := errors.New("database unavailable")
	s.readErr = broken
	if _, err := m.Evaluate(ctx, actor, "task", "attempt", input); !errors.Is(err, broken) || s.captured != nil {
		t.Fatalf("read error became assessment: %v", err)
	}
	s.readErr, s.writeErr = nil, broken
	if receipt, err := m.Evaluate(ctx, actor, "task", "attempt", input); !errors.Is(err, broken) || receipt.Created {
		t.Fatalf("write error became assessment: %+v %v", receipt, err)
	}
}
