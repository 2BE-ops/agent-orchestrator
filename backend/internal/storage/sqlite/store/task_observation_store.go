package store

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func collectTaskObservations(ctx context.Context, q *gen.Queries, result domain.TaskResult, started, now time.Time) (domain.TaskObservationEvidence, error) {
	evidence := domain.TaskObservationEvidence{PRs: []domain.TaskPREvidence{}, Reviews: []domain.TaskReviewEvidence{}}
	prs, err := q.CollectTaskPRFacts(ctx, result.SessionID)
	if err != nil {
		return evidence, err
	}
	evidence.PRsTruncated = len(prs) > 16
	for _, pr := range prs[:min(len(prs), 16)] {
		evidence.PRs = append(evidence.PRs, domain.TaskPREvidence{URL: pr.URL, HeadCommit: pr.HeadSha, Mergeability: pr.Mergeability, Merged: pr.IsMerged != 0, Closed: pr.IsClosed != 0, Draft: pr.IsDraft != 0, ObservedAt: pr.ObservedAt.Time})
	}
	reviews, err := q.CollectTaskReviewFacts(ctx, gen.CollectTaskReviewFactsParams{SessionID: result.SessionID, TargetCommit: result.Definition.ClaimedCommit})
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
		evidence.Reviews = append(evidence.Reviews, domain.TaskReviewEvidence{RunID: review.ID, ReviewID: review.ReviewID, PRURL: review.PRURL, TargetCommit: review.TargetSha, Harness: review.Harness, Status: review.Status, Verdict: review.Verdict, BodyPreviewHash: domain.ContextTextHash(preview), BodyPreview: preview, BodyBytes: review.BodyBytes, BodyTruncated: int64(end) < review.BodyBytes, CreatedAt: review.CreatedAt})
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
