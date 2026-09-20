package task

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ReviewInput selects an existing result. The frozen acceptance policy, not the
// caller, determines the reviewing Type, Skills and independence requirements.
type ReviewInput struct {
	ResultID string `json:"resultId"`
}

// ReviewView pairs a lifecycle result with exactly what its native reviewer saw.
type ReviewView struct {
	Run      domain.ReviewRun          `json:"run"`
	Snapshot domain.TaskReviewSnapshot `json:"snapshot"`
}

// WithNativeReviews connects task requests to the existing registry and native
// reviewer service. Neither the CLI nor the HTTP controller launches processes.
func WithNativeReviews(resolver ports.WorkerConfigurationResolver, launcher ports.TaskReviewLauncher) Option {
	return func(m *Manager) { m.reviewerResolver, m.reviewer = resolver, launcher }
}

// RequestReview seals exact result/criteria/configuration before the existing
// review engine records and launches native work. Storage repeats mutable guards.
func (m *Manager) RequestReview(ctx context.Context, actor domain.AdaptiveActor, taskID, attemptID string, input ReviewInput) (domain.TaskReviewReceipt, error) {
	var receipt domain.TaskReviewReceipt
	if strings.TrimSpace(input.ResultID) == "" || len(input.ResultID) > 200 || strings.IndexFunc(input.ResultID, unicode.IsControl) >= 0 {
		return receipt, apierr.Invalid("INVALID_TASK_REVIEW", "A bounded resultId is required", nil)
	}
	if err := (domain.TaskMutation{Actor: actor, Reason: "Request independent review"}).ValidatePlanning(); err != nil {
		return receipt, mapError(ports.ErrTaskForbidden)
	}
	result, err := m.Result(ctx, taskID, attemptID, input.ResultID)
	if err != nil {
		return receipt, err
	}
	prepared, err := m.store.PrepareTaskReview(ctx, result.ID, actor)
	if err != nil {
		return receipt, mapError(err)
	}
	if m.reviewerResolver == nil || m.reviewer == nil {
		return receipt, apierr.NotImplemented("TASK_REVIEW_UNAVAILABLE", "Native task review is unavailable")
	}
	project, found, err := m.store.GetProject(ctx, string(prepared.ProjectID))
	if err != nil {
		return receipt, err
	}
	if !found {
		return receipt, mapError(ports.ErrTaskNotFound)
	}
	policy := prepared.Criteria.Definition.ReviewPolicy
	if policy == nil {
		return receipt, apierr.Invalid("INVALID_TASK_REVIEW", "The attempt requires a frozen reviewer policy", nil)
	}
	origin := domain.RegistrySystem
	if actor.Kind == "USER" {
		origin = domain.RegistryUser
	}
	// Orchestrator selection is automated and must respect the same Type/Skill
	// select policy as the Agent Manager. It cannot impersonate a human.
	if actor.Kind == "ORCHESTRATOR" {
		origin = domain.RegistryManager
	}
	reviewer, err := m.reviewerResolver.ResolveWorker(ctx, domain.WorkerSelection{AgentTypeID: policy.AgentTypeID, Version: policy.Version}, project, domain.SessionModeTUI, domain.RegistryActor{Origin: origin, ID: actor.ID})
	if err != nil {
		return receipt, err
	}
	reviewer.SystemPrompt = "Independently assess the exact commit against the frozen task acceptance criteria. Report qualitative findings with the pinned review submission protocol."
	reviewer.ContentHash = reviewer.Hash()
	frozen := domain.TaskReviewContext{SchemaVersion: 1, TaskID: result.TaskID, AttemptID: result.AttemptID, SessionID: result.SessionID, ResultID: result.ID, ResultHash: result.ContentHash, TaskRevision: result.TaskRevision, CriteriaVersion: result.CriteriaVersion, Criteria: prepared.Criteria.Definition, CriteriaHash: prepared.Criteria.ContentHash, TargetCommit: result.Definition.ClaimedCommit, ImplementingType: prepared.Implementer.AgentType, ImplementingHarness: prepared.Implementer.Effective.Harness, ImplementingConfigurationHash: result.ConfigurationHash, Reviewer: reviewer, LaunchID: uuid.NewString(), Actor: actor, CreatedAt: time.Now().UTC()}
	frozen.ContentHash = frozen.Hash()
	if err := frozen.Validate(); err != nil {
		return receipt, apierr.Invalid("INVALID_TASK_REVIEW", err.Error(), nil)
	}
	receipt, err = m.reviewer.RequestTaskReview(ctx, frozen)
	switch {
	case errors.Is(err, ports.ErrRegistryForbidden):
		return receipt, apierr.Forbidden("TASK_REVIEW_SELECTION_FORBIDDEN", "Automated review cannot select this Type or Skill")
	case errors.Is(err, ports.ErrRegistryConflict):
		return receipt, apierr.Conflict("TASK_REVIEW_CONFIGURATION_CHANGED", "Reviewer configuration changed during admission; inspect and retry", nil)
	case errors.Is(err, ports.ErrRegistryInvalid):
		return receipt, apierr.Invalid("TASK_REVIEW_CONFIGURATION_UNAVAILABLE", "The pinned reviewer Type or Skill is unavailable", nil)
	}
	return receipt, mapError(err)
}

// Reviews lists at most 64 retained passes for an exact result. Large native
// context snapshots are loaded individually through Review.
func (m *Manager) Reviews(ctx context.Context, taskID, attemptID, resultID string) ([]domain.ReviewRun, error) {
	if _, err := m.Result(ctx, taskID, attemptID, resultID); err != nil {
		return nil, err
	}
	runs, err := m.store.ListTaskReviewRuns(ctx, resultID)
	return runs, mapError(err)
}

// Review inspects historical sealed context without consulting live Type state.
func (m *Manager) Review(ctx context.Context, taskID, attemptID, runID string) (ReviewView, error) {
	var view ReviewView
	snapshot, found, err := m.store.GetTaskReviewContext(ctx, runID)
	if err != nil {
		return view, err
	}
	if !found || snapshot.Context.TaskID != taskID || snapshot.Context.AttemptID != attemptID {
		return view, mapError(ports.ErrTaskNotFound)
	}
	run, found, err := m.store.GetReviewRun(ctx, runID)
	if err != nil {
		return view, err
	}
	if !found || run.TaskScope != snapshot.Context.ScopeHash() {
		return view, mapError(ports.ErrTaskNotFound)
	}
	return ReviewView{Run: run, Snapshot: snapshot}, nil
}
