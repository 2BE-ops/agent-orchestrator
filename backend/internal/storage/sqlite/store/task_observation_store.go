package store

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func collectTaskObservations(ctx context.Context, q *gen.Queries, result domain.TaskResult, reviewTarget *domain.TaskReviewTarget, started, now time.Time) (domain.TaskObservationEvidence, error) {
	evidence := domain.TaskObservationEvidence{PRs: []domain.TaskPREvidence{}, Reviews: []domain.TaskReviewEvidence{}, ReviewTarget: reviewTarget}
	prs, err := q.CollectTaskPRFacts(ctx, result.SessionID)
	if err != nil {
		return evidence, err
	}
	evidence.PRsTruncated = len(prs) > 16
	for _, pr := range prs[:min(len(prs), 16)] {
		evidence.PRs = append(evidence.PRs, domain.TaskPREvidence{URL: pr.URL, HeadCommit: pr.HeadSha, Mergeability: pr.Mergeability, Merged: pr.IsMerged != 0, Closed: pr.IsClosed != 0, Draft: pr.IsDraft != 0, ObservedAt: pr.ObservedAt.Time})
	}
	reviews, err := q.CollectTaskReviewFacts(ctx, gen.CollectTaskReviewFactsParams{SessionID: result.SessionID, TargetCommit: result.Definition.ClaimedCommit, ResultID: result.ID})
	if err != nil {
		return evidence, err
	}
	evidence.ReviewsTruncated = len(reviews) > 32
	for _, review := range reviews[:min(len(reviews), 32)] {
		preview := strings.ToValidUTF8(review.BodyPreview, "�")
		end := min(len(preview), 4096)
		for end > 0 && end < len(preview) && !utf8.RuneStart(preview[end]) {
			end--
		}
		preview = preview[:end]
		item := domain.TaskReviewEvidence{RunID: review.ID, ReviewID: review.ReviewID, PRURL: review.PRURL, TargetCommit: review.TargetSha, Harness: review.Harness, Status: review.Status, Verdict: review.Verdict, BodyPreviewHash: domain.ContextTextHash(preview), BodyPreview: preview, BodyBytes: review.BodyBytes, BodyTruncated: int64(end) < review.BodyBytes, CreatedAt: review.CreatedAt}
		if review.TaskScope != "" {
			row, err := q.GetTaskReviewContext(ctx, review.ID)
			if err != nil {
				return evidence, err
			}
			retained, err := taskReviewContextFromRow(row)
			if err != nil {
				return evidence, err
			}
			c := retained.Context
			subject := domain.TaskReviewTarget{ResultID: c.ResultID, ResultHash: c.ResultHash, CriteriaHash: c.CriteriaHash, ImplementingType: c.ImplementingType, ImplementingHarness: c.ImplementingHarness, ImplementingConfigurationHash: c.ImplementingConfigurationHash}
			if reviewTarget == nil || subject != *reviewTarget || c.TaskID != result.TaskID || c.AttemptID != result.AttemptID || c.SessionID != result.SessionID || c.TargetCommit != review.TargetSha || c.ScopeHash() != review.TaskScope || domain.ReviewerHarness(c.Reviewer.Effective.Harness) != review.Harness {
				return evidence, fmt.Errorf("retained native review does not match evaluation provenance")
			}
			item.Attribution = &domain.TaskReviewAttribution{Target: subject, ContextHash: c.ContentHash, ConfigurationHash: c.Reviewer.ContentHash, ReviewerType: c.Reviewer.AgentType, Model: c.Reviewer.Effective.Config.Model, StartedAt: retained.StartedAt}
		}
		evidence.Reviews = append(evidence.Reviews, item)
	}
	session, err := q.GetSession(ctx, result.SessionID)
	if err != nil {
		return evidence, err
	}
	lease, err := q.GetTaskLease(ctx, result.AttemptID)
	if err != nil {
		return evidence, err
	}
	worker := domain.TaskWorkerEvidence{SessionID: result.SessionID, SessionRevision: session.Revision, Activity: session.ActivityState, ActivityAt: session.ActivityLastAt, Terminated: session.IsTerminated, AttemptCreatedAt: started, LeaseHeartbeatAt: lease.HeartbeatAt, LeaseExpiresAt: lease.ExpiresAt, LeaseReleaseReason: lease.ReleaseReason, ReservationOngoing: !lease.ReleasedAt.Valid}
	end := now
	if lease.ReleasedAt.Valid {
		worker.LeaseReleasedAt = &lease.ReleasedAt.Time
		end = lease.ReleasedAt.Time
	}
	worker.ReservationElapsedMS = max(0, end.Sub(started).Milliseconds())
	evidence.Worker = worker
	return evidence, nil
}
