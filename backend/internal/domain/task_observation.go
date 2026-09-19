package domain

import (
	"fmt"
	"time"
)

// TaskPREvidence records a bounded view of independently observed SCM facts.
type TaskPREvidence struct {
	URL          string       `json:"url"`
	HeadCommit   string       `json:"headCommit"`
	Mergeability Mergeability `json:"mergeability"`
	Merged       bool         `json:"merged"`
	Closed       bool         `json:"closed"`
	Draft        bool         `json:"draft"`
	ObservedAt   time.Time    `json:"observedAt"`
}

// TaskReviewEvidence points to an existing AO review run. Its verdict and body
// remain qualitative reviewer output; no Type identity is inferred from harness.
type TaskReviewEvidence struct {
	RunID           string          `json:"runId"`
	ReviewID        string          `json:"reviewId"`
	PRURL           string          `json:"prUrl"`
	TargetCommit    string          `json:"targetCommit"`
	Harness         ReviewerHarness `json:"harness"`
	Status          ReviewRunStatus `json:"status"`
	Verdict         ReviewVerdict   `json:"verdict"`
	BodyPreviewHash string          `json:"bodyPreviewHash"`
	BodyPreview     string          `json:"bodyPreview"`
	BodyBytes       int64           `json:"bodyBytes"`
	BodyTruncated   bool            `json:"bodyTruncated"`
	CreatedAt       time.Time       `json:"createdAt"`
}

// TaskWorkerEvidence retains observed lifecycle and reservation time. Exited or
// terminated is not proof of a crash, and elapsed reservation time is not CPU time.
type TaskWorkerEvidence struct {
	SessionID            SessionID     `json:"sessionId"`
	SessionRevision      int64         `json:"sessionRevision"`
	Activity             ActivityState `json:"activity"`
	ActivityAt           time.Time     `json:"activityAt"`
	Terminated           bool          `json:"terminated"`
	AttemptCreatedAt     time.Time     `json:"attemptCreatedAt"`
	LeaseHeartbeatAt     time.Time     `json:"leaseHeartbeatAt"`
	LeaseExpiresAt       time.Time     `json:"leaseExpiresAt"`
	LeaseReleasedAt      *time.Time    `json:"leaseReleasedAt,omitempty"`
	LeaseReleaseReason   string        `json:"leaseReleaseReason,omitempty"`
	ReservationElapsedMS int64         `json:"reservationElapsedMs"`
	ReservationOngoing   bool          `json:"reservationOngoing"`
}

// TaskObservationEvidence is copied in the evaluation transaction. Truncation
// and observation timestamps remain visible; historical reads never recollect it.
type TaskObservationEvidence struct {
	PRs                []TaskPREvidence       `json:"prs"`
	PRsTruncated       bool                   `json:"prsTruncated"`
	Reviews            []TaskReviewEvidence   `json:"reviews"`
	ReviewsTruncated   bool                   `json:"reviewsTruncated"`
	Worker             TaskWorkerEvidence     `json:"worker"`
	Artifacts          []TaskArtifactEvidence `json:"artifacts,omitempty"`
	ArtifactsTruncated bool                   `json:"artifactsTruncated,omitempty"`
}

// Validate bounds retained source references and review previews.
func (e TaskObservationEvidence) Validate() error {
	if len(e.PRs) > 16 || len(e.Reviews) > 32 || !resultText(string(e.Worker.SessionID), 200, true) || e.Worker.SessionRevision < 0 || e.Worker.ReservationElapsedMS < 0 || !resultText(e.Worker.LeaseReleaseReason, 2000, false) {
		return fmt.Errorf("invalid evaluation observation bounds")
	}
	for _, pr := range e.PRs {
		if !resultText(pr.URL, 2000, true) || len(pr.HeadCommit) > 64 || len(pr.Mergeability) > 100 {
			return fmt.Errorf("invalid PR evidence")
		}
	}
	for _, review := range e.Reviews {
		if !resultText(review.RunID, 200, true) || !resultText(review.ReviewID, 200, true) || !resultText(review.PRURL, 2000, true) || len(review.TargetCommit) > 64 || len(review.Harness) > 100 || len(review.Status) > 100 || len(review.Verdict) > 100 || review.BodyPreviewHash != ContextTextHash(review.BodyPreview) || len(review.BodyPreview) > 4096 || review.BodyBytes < 0 {
			return fmt.Errorf("invalid review evidence")
		}
	}
	if len(e.Artifacts) > 16 {
		return fmt.Errorf("too many artifact observations")
	}
	for _, artifact := range e.Artifacts {
		if err := artifact.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func evaluateTaskMergeability(target string, evidence *TaskObservationEvidence, now time.Time) (string, string) {
	if evidence == nil || evidence.PRsTruncated {
		return "inconclusive", "A complete PR observation is required"
	}
	found, incomplete := false, false
	for _, pr := range evidence.PRs {
		if pr.HeadCommit != target {
			continue
		}
		found = true
		if pr.ObservedAt.IsZero() || pr.ObservedAt.After(now.Add(30*time.Second)) {
			incomplete = true
			continue
		}
		if pr.Mergeability == MergeConflicting || (pr.Closed && !pr.Merged) {
			return "failed", "The exact target PR has a merge conflict or was closed without merging"
		}
		if pr.Draft || pr.Mergeability != MergeMergeable {
			incomplete = true
		}
	}
	if !found || incomplete {
		return "inconclusive", "PR head, observation or mergeability is missing, draft or unknown"
	}
	return "passed", "Observed PRs at the exact target commit are mergeable"
}
