package domain

import (
	"fmt"
	"time"
)

// TaskReviewTarget identifies the exact immutable result and implementing
// configuration being assessed, independently of any reviewer's claims.
type TaskReviewTarget struct {
	ResultID                      string              `json:"resultId"`
	ResultHash                    string              `json:"resultHash"`
	CriteriaHash                  string              `json:"criteriaHash"`
	ImplementingType              WorkerDefinitionRef `json:"implementingType"`
	ImplementingHarness           AgentHarness        `json:"implementingHarness"`
	ImplementingConfigurationHash string              `json:"implementingConfigurationHash"`
}

// TaskReviewAttribution is a compact reference to immutable native review
// context. Full Skill content stays in that inspectable snapshot, not repeated
// once per PR in every evaluation. StartedAt is observed by the daemon.
type TaskReviewAttribution struct {
	Target            TaskReviewTarget    `json:"target"`
	ContextHash       string              `json:"contextHash"`
	ConfigurationHash string              `json:"configurationHash"`
	ReviewerType      WorkerDefinitionRef `json:"reviewerType"`
	Model             string              `json:"model"`
	StartedAt         *time.Time          `json:"startedAt,omitempty"`
}

func validReviewType(ref WorkerDefinitionRef) bool {
	return resultText(ref.ID, 200, true) && ref.Version > 0 && resultText(ref.Name, 200, true) && validArtifactHash(ref.ContentHash)
}

func (t TaskReviewTarget) validate() error {
	if !resultText(t.ResultID, 200, true) || !validArtifactHash(t.ResultHash) || !validArtifactHash(t.CriteriaHash) || !validArtifactHash(t.ImplementingConfigurationHash) || !validReviewType(t.ImplementingType) || !t.ImplementingHarness.IsKnown() {
		return fmt.Errorf("invalid task review target provenance")
	}
	return nil
}

func (a TaskReviewAttribution) validate() error {
	if err := a.Target.validate(); err != nil {
		return err
	}
	if !validArtifactHash(a.ContextHash) || !validArtifactHash(a.ConfigurationHash) || !validReviewType(a.ReviewerType) || !resultText(a.Model, 300, false) {
		return fmt.Errorf("invalid native review attribution")
	}
	return nil
}

func evaluateTaskReview(criteria AcceptanceCriteria, target string, evidence *TaskObservationEvidence, now time.Time) (string, string) {
	if criteria.ReviewPolicy == nil || evidence == nil || evidence.ReviewTarget == nil || evidence.PRsTruncated || evidence.ReviewsTruncated {
		return "inconclusive", "Complete native review evidence and a frozen reviewer policy are required"
	}
	_, criteriaHash, _ := TaskContent(criteria)
	if evidence.ReviewTarget.validate() != nil || evidence.ReviewTarget.CriteriaHash != criteriaHash {
		return "inconclusive", "Review target does not match the frozen criteria"
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
		var latest *TaskReviewEvidence
		for i := range evidence.Reviews {
			r := &evidence.Reviews[i]
			if r.PRURL == pr.URL && r.TargetCommit == target && r.Attribution != nil && (latest == nil || r.CreatedAt.After(latest.CreatedAt) || (r.CreatedAt.Equal(latest.CreatedAt) && r.RunID > latest.RunID)) {
				latest = r
			}
		}
		if latest == nil {
			incomplete = true
			continue
		}
		a := latest.Attribution
		if a.validate() != nil || a.Target != *evidence.ReviewTarget || a.StartedAt == nil || a.StartedAt.IsZero() || a.StartedAt.After(now.Add(30*time.Second)) || latest.CreatedAt.IsZero() || latest.CreatedAt.After(now.Add(30*time.Second)) || !criteria.ReviewPolicy.Allows(a.Target.ImplementingType, a.ReviewerType, a.Target.ImplementingHarness, AgentHarness(latest.Harness)) {
			incomplete = true
			continue
		}
		if latest.Status != ReviewRunComplete && latest.Status != ReviewRunDelivered {
			incomplete = true
			continue
		}
		if latest.Verdict == VerdictChangesRequested {
			return "failed", "The pinned native reviewer requested changes on the exact result and commit (qualitative evidence)"
		}
		if latest.Verdict != VerdictApproved {
			incomplete = true
		}
	}
	if !found || incomplete {
		return "inconclusive", "Review is missing, incomplete, unlaunched, stale or incompatible with the frozen policy"
	}
	return "passed", "Pinned native review approved the exact result and current PR heads (qualitative evidence)"
}
