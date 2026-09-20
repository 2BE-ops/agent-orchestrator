package task_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

type performanceStore struct {
	tasksvc.Store
	calls int
	err   error
}

func (s *performanceStore) ListTaskPerformance(context.Context, domain.TaskPerformanceQuery) (domain.TaskPerformancePage, error) {
	s.calls++
	return domain.TaskPerformancePage{}, s.err
}
func (s *performanceStore) GetTaskPerformanceSummary(context.Context, domain.TaskPerformanceSummaryQuery) (domain.TaskPerformanceSummary, error) {
	s.calls++
	return domain.TaskPerformanceSummary{}, s.err
}

func TestTaskPerformanceServiceValidationAndStorageErrors(t *testing.T) {
	s := &performanceStore{err: errors.New("storage unavailable")}
	m := tasksvc.New(s)
	ctx := context.Background()
	_, err := m.Performance(ctx, domain.TaskPerformanceQuery{})
	if err == nil || s.calls != 0 {
		t.Fatalf("invalid query reached storage: %v", err)
	}
	_, err = m.PerformanceSummary(ctx, domain.TaskPerformanceSummaryQuery{})
	if err == nil || s.calls != 0 {
		t.Fatalf("invalid summary reached storage: %v", err)
	}
	from := time.Now().UTC().Add(-time.Hour)
	to := from.Add(time.Hour)
	_, err = m.Performance(ctx, domain.TaskPerformanceQuery{ProjectID: "project", From: from, To: to, Limit: 20})
	if !errors.Is(err, s.err) || s.calls != 1 {
		t.Fatalf("storage failure became empty evidence: %v", err)
	}
	_, err = m.PerformanceSummary(ctx, domain.TaskPerformanceSummaryQuery{ProjectID: "project", From: from, To: to, GroupBy: "model"})
	if !errors.Is(err, s.err) || s.calls != 2 {
		t.Fatalf("storage failure became zero metrics: %v", err)
	}
}
