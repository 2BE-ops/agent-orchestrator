package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.ProjectFeedbackStore = (*Store)(nil)

// ListProjectFeedback derives the per-task facts the orchestrator loop reads:
// verified completion, cancellation, retry exhaustion or open work. It never
// writes, so restart changes nothing and no derived status is stored.
func (s *Store) ListProjectFeedback(ctx context.Context, projectID domain.ProjectID, afterTaskID string, limit int) ([]domain.ProjectFeedbackItem, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: feedback page limit must be between 1 and 100", ports.ErrGoalInvalid)
	}
	var items []domain.ProjectFeedbackItem
	err := s.inTxLocked(ctx, "derive project feedback", func(q *gen.Queries) error {
		ids, err := q.ListAdaptiveTaskIDs(ctx, gen.ListAdaptiveTaskIDsParams{ProjectID: string(projectID), ID: afterTaskID, Limit: int64(limit)})
		if err != nil {
			return err
		}
		items = make([]domain.ProjectFeedbackItem, 0, len(ids))
		for _, id := range ids {
			item, err := projectFeedbackItem(ctx, q, id, time.Now().UTC())
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return nil
	})
	return items, err
}

// inTxLocked runs a read-only derivation under the same single-writer lock and
// transaction the completion projection uses, without writing any rows.
func (s *Store) inTxLocked(ctx context.Context, what string, fn func(q *gen.Queries) error) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, what, fn)
}

func projectFeedbackItem(ctx context.Context, q *gen.Queries, id string, now time.Time) (domain.ProjectFeedbackItem, error) {
	task, err := q.GetAdaptiveTask(ctx, id)
	if err != nil {
		return domain.ProjectFeedbackItem{}, taskReadError(err)
	}
	revisionRow, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: id, Number: task.Revision})
	if err != nil {
		return domain.ProjectFeedbackItem{}, err
	}
	revision, err := taskRevisionFromRow(revisionRow)
	if err != nil {
		return domain.ProjectFeedbackItem{}, err
	}
	item := domain.ProjectFeedbackItem{TaskID: id, Title: revision.Definition.Title, Revision: task.Revision, State: "pending", Reason: "No attempt has started against the current revision"}
	cancelledBy, err := q.CancelledTaskAncestor(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return item, err
	}
	_, leaseErr := q.GetActiveTaskLease(ctx, id)
	if leaseErr != nil && !errors.Is(leaseErr, sql.ErrNoRows) {
		return item, leaseErr
	}
	leased := leaseErr == nil
	if cancelledBy != "" {
		item.State, item.Reason = "cancelled", "Cancellation intent blocks new work"
		if leased {
			item.State = "cancelling"
			item.Reason = "Cancellation requested; worker ownership is still reserved"
		}
		return item, nil
	}
	proof, err := currentTaskCompletion(ctx, q, id, task.Revision, now)
	if err != nil {
		return item, err
	}
	if proof.Verified {
		item.State, item.Reason, item.ResultID, item.EvaluationID = "completed", "Independent evidence passes the frozen criteria", proof.ResultID, proof.EvaluationID
		return item, nil
	}
	if leased {
		item.State, item.Reason = "working", "An exclusive attempt holds this task"
		return item, nil
	}
	attempts, err := q.ListTaskAttempts(ctx, gen.ListTaskAttemptsParams{TaskID: id, Number: 0, Limit: 100})
	if err != nil {
		return item, err
	}
	if len(attempts) >= revision.Definition.MaxAttempts {
		item.State, item.Reason = "failed", "Task attempt limit is exhausted"
		return item, nil
	}
	item.Reason = proof.Reason
	if proof.AttemptID != "" {
		item.State = "working"
	}
	return item, nil
}
