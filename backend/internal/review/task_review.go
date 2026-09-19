package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ErrTaskReviewLaunchUncertain means native execution may have started. Retain
// the running pass until reconciliation; an error is not proof of no handoff.
var ErrTaskReviewLaunchUncertain = errors.New("task reviewer launch is uncertain")

// TriggerTask reuses the normal review lifecycle with exact adaptive task
// provenance. The shared task service supplies this sealed daemon-owned context.
func (e *Engine) TriggerTask(ctx context.Context, frozen domain.TaskReviewContext) (TriggerResult, error) {
	if err := frozen.Validate(); err != nil {
		return TriggerResult{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, ok := e.store.(ports.TaskReviewStore); !ok {
		return TriggerResult{}, fmt.Errorf("%w: task review storage is unavailable", ErrInvalid)
	}
	return e.triggerWithTask(ctx, frozen.SessionID, domain.ReviewerHarness(frozen.Reviewer.Effective.Harness), frozen.Reviewer.Effective.Config, domain.ReviewTriggerManual, &frozen)
}

func taskReviewScopeAvailable(runs []domain.ReviewRun, frozen *domain.TaskReviewContext) error {
	scope := ""
	if frozen != nil {
		scope = frozen.ScopeHash()
	}
	for _, run := range runs {
		if run.Status == domain.ReviewRunRunning && run.TaskScope != scope && (run.TaskScope != "" || frozen != nil) {
			return fmt.Errorf("%w: another review scope is still running; reconcile or cancel it first", ErrInvalid)
		}
	}
	return nil
}

func latestReviewHasTaskScope(runs []domain.ReviewRun, harness domain.ReviewerHarness) bool {
	var latest *domain.ReviewRun
	for i := range runs {
		run := &runs[i]
		if run.Harness == harness && (latest == nil || run.CreatedAt.After(latest.CreatedAt) || (run.CreatedAt.Equal(latest.CreatedAt) && run.ID > latest.ID)) {
			latest = run
		}
	}
	return latest != nil && latest.TaskScope != ""
}

func reviewRunsForTaskScope(runs []domain.ReviewRun, frozen *domain.TaskReviewContext) []domain.ReviewRun {
	scope := ""
	if frozen != nil {
		scope = frozen.ScopeHash()
	}
	filtered := make([]domain.ReviewRun, 0, len(runs))
	for _, run := range runs {
		if run.TaskScope == scope {
			filtered = append(filtered, run)
		}
	}
	return filtered
}

func (e *Engine) insertReviewWithTask(ctx context.Context, run domain.ReviewRun, frozen *domain.TaskReviewContext) error {
	if frozen == nil {
		return e.store.InsertReviewRun(ctx, run)
	}
	taskStore, ok := e.store.(ports.TaskReviewStore)
	if !ok {
		return fmt.Errorf("%w: task review storage is unavailable", ErrInvalid)
	}
	return taskStore.InsertTaskReviewRun(ctx, run, *frozen)
}

func (e *Engine) existingReviewWithTask(ctx context.Context, workerID domain.SessionID, prURL, target string, harness domain.ReviewerHarness, frozen *domain.TaskReviewContext) (domain.ReviewRun, bool, error) {
	if frozen == nil {
		return e.store.GetReviewRunBySessionPRSHAAndHarness(ctx, workerID, prURL, target, harness)
	}
	runs, err := e.store.ListReviewRunsBySession(ctx, workerID)
	if err != nil {
		return domain.ReviewRun{}, false, err
	}
	var latest domain.ReviewRun
	for _, run := range reviewRunsForTaskScope(runs, frozen) {
		if run.PRURL == prURL && run.TargetSHA == target && run.Harness == harness && (latest.ID == "" || run.CreatedAt.After(latest.CreatedAt) || (run.CreatedAt.Equal(latest.CreatedAt) && run.ID > latest.ID)) {
			latest = run
		}
	}
	return latest, latest.ID != "", nil
}
