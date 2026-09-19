package store

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.ContextArtifactStore = (*Store)(nil)

// SelectTaskContextResults returns only the latest correction of each selected
// attempt. Four rows permit a three-attempt cohort plus explicit truncation evidence.
func (s *Store) SelectTaskContextResults(ctx context.Context, taskID string, revision int64, excludedAttempt string, limit int) ([]domain.TaskResult, error) {
	if taskID == "" || revision < 0 || excludedAttempt == "" || limit < 1 || limit > 4 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.SelectTaskContextResults(ctx, gen.SelectTaskContextResultsParams{TaskID: taskID, TaskRevision: revision, ExcludedAttempt: excludedAttempt, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]domain.TaskResult, 0, len(rows))
	for _, row := range rows {
		item, err := taskResultFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// SelectTaskContextInterfaces returns the most recent bounded incoming contracts
// for this task; they remain proposals, independent of their delivery state.
func (s *Store) SelectTaskContextInterfaces(ctx context.Context, projectID domain.ProjectID, taskID string, limit int) ([]domain.TaskMessage, error) {
	if projectID == "" || taskID == "" || limit < 1 || limit > 9 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.SelectTaskContextInterfaces(ctx, gen.SelectTaskContextInterfacesParams{ProjectID: string(projectID), TargetTaskID: taskID, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return taskMessagesFromRows(rows)
}
