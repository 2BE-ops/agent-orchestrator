package task_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

type delegationServiceStore struct {
	tasksvc.Store
	attempt      domain.TaskAttempt
	delegations  []domain.TaskDelegation
	exact        domain.TaskDelegation
	listErr      error
	exactMissing bool
}

func (s *delegationServiceStore) GetTaskAttempt(context.Context, string) (domain.TaskAttempt, error) {
	return s.attempt, nil
}

func (s *delegationServiceStore) ListTaskDelegations(_ context.Context, attemptID string, after int64, limit int) ([]domain.TaskDelegation, error) {
	if attemptID != s.attempt.ID {
		return nil, ports.ErrTaskNotFound
	}
	return s.delegations, s.listErr
}

func (s *delegationServiceStore) GetTaskDelegation(_ context.Context, attemptID string, number int64) (domain.TaskDelegation, error) {
	if attemptID != s.attempt.ID || s.exactMissing || s.exact.Number != number {
		return domain.TaskDelegation{}, ports.ErrTaskNotFound
	}
	return s.exact, nil
}

func TestTaskDelegationServiceScopesHistoryToItsAttempt(t *testing.T) {
	ctx := context.Background()
	store := &delegationServiceStore{attempt: domain.TaskAttempt{ID: "attempt", TaskID: "task"}, delegations: []domain.TaskDelegation{{AttemptID: "attempt", Number: 1}}, exact: domain.TaskDelegation{AttemptID: "attempt", Number: 1}}
	svc := tasksvc.New(store)
	items, err := svc.Delegations(ctx, "task", "attempt", 0, 20)
	if err != nil || len(items) != 1 || items[0].Number != 1 {
		t.Fatalf("delegation history: %+v %v", items, err)
	}
	if _, err := svc.Delegations(ctx, "other-task", "attempt", 0, 20); err == nil || !isAPICode(err, "TASK_DELEGATION_NOT_FOUND") {
		t.Fatalf("cross-task history leaked: %v", err)
	}
	if _, err := svc.Delegation(ctx, "task", "attempt", 1); err != nil {
		t.Fatalf("exact delegation read: %v", err)
	}
	if _, err := svc.Delegation(ctx, "other-task", "attempt", 1); err == nil || !isAPICode(err, "TASK_DELEGATION_NOT_FOUND") {
		t.Fatalf("cross-task exact read leaked: %v", err)
	}
	store.exactMissing = true
	if _, err := svc.Delegation(ctx, "task", "attempt", 1); err == nil || !isAPICode(err, "TASK_DELEGATION_NOT_FOUND") {
		t.Fatalf("missing delegation: %v", err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := svc.Delegations(ctx, "task", "attempt", 0, limit); err == nil {
			t.Fatalf("unbounded page accepted: %d", limit)
		}
	}
	for _, number := range []int64{0, 1001} {
		if _, err := svc.Delegation(ctx, "task", "attempt", number); err == nil {
			t.Fatalf("unbounded number accepted: %d", number)
		}
	}
	if _, err := tasksvc.ParseTaskDelegationNumber("nope"); err == nil {
		t.Fatal("non-numeric delegation number accepted")
	}
	if number, err := tasksvc.ParseTaskDelegationNumber("12"); err != nil || number != 12 {
		t.Fatalf("numeric delegation number refused: %d %v", number, err)
	}
	failure := errors.New("delegation database unavailable")
	store.listErr = failure
	if _, err := svc.Delegations(ctx, "task", "attempt", 0, 20); !errors.Is(err, failure) {
		t.Fatalf("list failure disguised: %v", err)
	}
}

func isAPICode(err error, code string) bool {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == code
	}
	return false
}
