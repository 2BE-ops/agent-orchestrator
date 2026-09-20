package review

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
)

var _ ports.TaskReviewLauncher = (*Service)(nil)

// RequestTaskReview preserves the native review lifecycle and account-operation
// gate while admitting only a sealed, daemon-resolved task configuration.
func (s *Service) RequestTaskReview(ctx context.Context, frozen domain.TaskReviewContext) (domain.TaskReviewReceipt, error) {
	receipt := domain.TaskReviewReceipt{Runs: []domain.ReviewRun{}}
	if err := frozen.Validate(); err != nil {
		return receipt, apierr.Invalid("INVALID_TASK_REVIEW", err.Error(), nil)
	}
	if s.engine == nil {
		return receipt, apierr.NotImplemented("TASK_REVIEW_UNAVAILABLE", "Native review engine is unavailable")
	}
	release, err := s.acquireReviewerCodexAdmission(ctx, frozen.SessionID, domain.ReviewerHarness(frozen.Reviewer.Effective.Harness))
	if err != nil {
		return receipt, err
	}
	defer release()
	result, err := s.engine.TriggerTask(ctx, frozen)
	payload := map[string]any{"trigger": string(domain.ReviewTriggerManual), "harness": string(frozen.Reviewer.Effective.Harness), "created_runs": len(result.CreatedRuns), "reused": !result.Created && len(result.CreatedRuns) == 0}
	s.emit(ctx, "ao.review.triggered", frozen.SessionID, payload)
	if err != nil {
		s.emit(ctx, "ao.review.trigger_failed", frozen.SessionID, map[string]any{"trigger": string(domain.ReviewTriggerManual), "error_kind": reviewErrorKind(err)})
		switch {
		case errors.Is(err, reviewcore.ErrTaskReviewLaunchUncertain):
			return receipt, apierr.Conflict("TASK_REVIEW_RECONCILE_REQUIRED", "Native review may have started; inspect the retained pass before retrying", nil)
		case errors.Is(err, reviewcore.ErrInvalid):
			return receipt, apierr.Invalid("INVALID_TASK_REVIEW", err.Error(), nil)
		case errors.Is(err, reviewcore.ErrNotFound):
			return receipt, apierr.NotFound("TASK_REVIEW_NOT_FOUND", "The worker or review target was not found")
		case errors.Is(err, ports.ErrAgentBinaryNotFound):
			return receipt, apierr.Invalid("REVIEWER_BINARY_NOT_FOUND", "The selected native reviewer is unavailable", nil)
		default:
			return receipt, err
		}
	}
	receipt.Created, receipt.SkipReason = result.Created, result.SkipReason
	scope := frozen.ScopeHash()
	for _, run := range result.Runs {
		if run.TaskScope == scope {
			receipt.Runs = append(receipt.Runs, run)
		}
	}
	return receipt, nil
}
