package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// GetTaskPerformanceSummary observes the complete bounded cohort in one SQLite
// transaction. It does not aggregate independently read pages with drifting facts.
func (s *Store) GetTaskPerformanceSummary(ctx context.Context, query domain.TaskPerformanceSummaryQuery) (domain.TaskPerformanceSummary, error) {
	var summary domain.TaskPerformanceSummary
	if err := query.Validate(); err != nil {
		return summary, fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return summary, err
	}
	defer s.writeMu.Unlock()
	err := s.inTx(ctx, "summarize task performance cohort", func(q *gen.Queries) error {
		observedAt := time.Now().UTC()
		if _, err := q.GetProject(ctx, query.ProjectID); err != nil {
			return taskReadError(err)
		}
		rows, err := q.ListTaskPerformanceAttempts(ctx, gen.ListTaskPerformanceAttemptsParams{ProjectID: string(query.ProjectID), WindowStart: query.From.UTC(), WindowEnd: query.To.UTC(), PageLimit: domain.TaskPerformanceSummaryLimit + 1})
		if err != nil {
			return err
		}
		if len(rows) > domain.TaskPerformanceSummaryLimit {
			return ports.ErrTaskPerformanceWindowTooLarge
		}
		items := make([]domain.TaskPerformanceAttempt, 0, len(rows))
		for _, row := range rows {
			item, err := taskPerformanceAttempt(ctx, q, row, observedAt)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		summary, err = domain.SummarizeTaskPerformance(query, items, observedAt)
		return err
	})
	if err != nil {
		return domain.TaskPerformanceSummary{}, err
	}
	return summary, nil
}
