package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// SubmitTaskReviewResult checks source generation and records the result/audit
// atomically. Historical exact retries acknowledge facts without re-emitting them.
func (s *Store) SubmitTaskReviewResult(ctx context.Context, input domain.TaskReviewSubmission) (domain.ReviewRun, bool, error) {
	var run domain.ReviewRun
	if err := input.Validate(); err != nil {
		return run, false, fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return run, false, err
	}
	defer s.writeMu.Unlock()
	created := false
	err := s.inTx(ctx, "record pinned native review result", func(q *gen.Queries) error {
		row, err := q.GetReviewRun(ctx, input.RunID)
		if err != nil {
			return taskReadError(err)
		}
		run = reviewRunFromRow(row)
		if run.TaskScope == "" || run.SessionID != input.SessionID {
			return ports.ErrTaskForbidden
		}
		contextRow, err := q.GetTaskReviewContext(ctx, run.ID)
		if err != nil {
			return err
		}
		retained, err := taskReviewContextFromRow(contextRow)
		if err != nil {
			return err
		}
		frozen := retained.Context
		if frozen.LaunchID != input.SourceGeneration || run.TaskScope != frozen.ScopeHash() {
			return ports.ErrTaskLeaseFenced
		}
		if run.Status == domain.ReviewRunComplete || run.Status == domain.ReviewRunDelivered {
			if run.Verdict != input.Verdict || run.Body != input.Body || run.GithubReviewID != input.GithubReviewID {
				return ports.ErrTaskConflict
			}
			return nil
		}
		if run.Status != domain.ReviewRunRunning {
			return ports.ErrTaskLeaseFenced
		}
		worker, err := q.GetSession(ctx, run.SessionID)
		if err != nil {
			return err
		}
		changed, err := q.SubmitTaskReviewResult(ctx, gen.SubmitTaskReviewResultParams{RunID: run.ID, SessionID: run.SessionID, SourceGeneration: input.SourceGeneration, Verdict: input.Verdict, Body: input.Body, GithubReviewID: input.GithubReviewID, AutoInjectReview: worker.AutoInjectReview})
		if err != nil {
			return err
		}
		if changed != 1 {
			return ports.ErrTaskLeaseFenced
		}
		run.Status, run.Verdict, run.Body, run.GithubReviewID, run.AutoInjectReview = domain.ReviewRunComplete, input.Verdict, input.Body, input.GithubReviewID, worker.AutoInjectReview
		actor := domain.AdaptiveActor{Kind: "SYSTEM", ID: "native-reviewer"}
		if err := insertTaskAudit(ctx, q, frozen.TaskID, frozen.TaskRevision, "review_submitted", domain.TaskMutation{Actor: actor, Reason: "Recorded result for pinned native review " + run.ID}, time.Now().UTC()); err != nil {
			return err
		}
		created = true
		return nil
	})
	return run, created && err == nil, err
}
