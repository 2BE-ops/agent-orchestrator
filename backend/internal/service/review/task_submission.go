package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (s *Service) submitTaskReview(ctx context.Context, workerID domain.SessionID, review SubmittedReview) (domain.ReviewRun, error) {
	store, ok := s.store.(interface {
		SubmitTaskReviewResult(context.Context, domain.TaskReviewSubmission) (domain.ReviewRun, bool, error)
	})
	if !ok {
		return domain.ReviewRun{}, fmt.Errorf("%w: task review submission is unavailable", ErrInvalid)
	}
	run, created, err := store.SubmitTaskReviewResult(ctx, domain.TaskReviewSubmission{RunID: review.RunID, SessionID: workerID, SourceGeneration: review.SourceGeneration, Verdict: review.Verdict, Body: review.Body, GithubReviewID: review.GithubReviewID})
	if errors.Is(err, ports.ErrTaskInvalid) || errors.Is(err, ports.ErrTaskForbidden) || errors.Is(err, ports.ErrTaskLeaseFenced) || errors.Is(err, ports.ErrTaskConflict) {
		return domain.ReviewRun{}, fmt.Errorf("%w: task review source generation or retained result does not match", ErrInvalid)
	}
	if err != nil {
		return domain.ReviewRun{}, err
	}
	if created {
		s.emitSubmittedReview(ctx, run)
	}
	return run, nil
}

// Both review paths emit only on the persisted running-to-complete transition.
// These are enums, durations and counts; no review text or provenance IDs leave.
func (s *Service) emitSubmittedReview(ctx context.Context, run domain.ReviewRun) {
	s.emit(ctx, "ao.review.submitted", run.SessionID, map[string]any{
		"harness":            string(run.Harness),
		"verdict":            string(run.Verdict),
		"duration_ms":        s.clock().Sub(run.CreatedAt).Milliseconds(),
		"posted_to_provider": run.GithubReviewID != "",
		"trigger":            string(run.TriggerSource),
		"body_bytes":         len(run.Body),
		"auto_inject":        run.AutoInjectReview,
	})
}
