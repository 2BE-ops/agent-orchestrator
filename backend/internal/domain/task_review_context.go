package domain

import (
	"fmt"
	"time"
)

// TaskReviewPreparation is trusted store output used to resolve a native
// reviewer. Revalidation at insertion closes races with a replacement result.
type TaskReviewPreparation struct {
	ProjectID   ProjectID
	Result      TaskResult
	Criteria    AcceptanceCriteriaVersion
	Implementer WorkerConfiguration
}

// TaskReviewContext seals what a native review pass was asked to assess. The
// review lifecycle and verdict still belong to the existing ReviewRun entity.
type TaskReviewContext struct {
	SchemaVersion                 int                 `json:"schemaVersion"`
	TaskID                        string              `json:"taskId"`
	AttemptID                     string              `json:"attemptId"`
	SessionID                     SessionID           `json:"sessionId"`
	ResultID                      string              `json:"resultId"`
	ResultHash                    string              `json:"resultHash"`
	TaskRevision                  int64               `json:"taskRevision"`
	CriteriaVersion               int64               `json:"criteriaVersion"`
	Criteria                      AcceptanceCriteria  `json:"criteria"`
	CriteriaHash                  string              `json:"criteriaHash"`
	TargetCommit                  string              `json:"targetCommit"`
	ImplementingType              WorkerDefinitionRef `json:"implementingType"`
	ImplementingHarness           AgentHarness        `json:"implementingHarness"`
	ImplementingConfigurationHash string              `json:"implementingConfigurationHash"`
	Reviewer                      WorkerConfiguration `json:"reviewer"`
	LaunchID                      string              `json:"launchId"`
	Actor                         AdaptiveActor       `json:"actor"`
	CreatedAt                     time.Time           `json:"createdAt"`
	ContentHash                   string              `json:"contentHash"`
}

// Hash seals attribution, policy and native configuration together.
func (c TaskReviewContext) Hash() string {
	c.ContentHash = ""
	_, hash, _ := TaskContent(c)
	return hash
}

// ScopeHash deduplicates repeated requests for the same effective review. The
// invocation identity, observer and timestamps do not change the work requested.
func (c TaskReviewContext) ScopeHash() string {
	c.ContentHash, c.LaunchID = "", ""
	c.Actor, c.CreatedAt = AdaptiveActor{}, time.Time{}
	c.Reviewer.ContentHash, c.Reviewer.ActorID, c.Reviewer.CatalogFingerprint = "", "", ""
	c.Reviewer.Origin, c.Reviewer.CreatedAt = "", time.Time{}
	return c.Hash()
}

// Validate verifies a bounded sealed snapshot; storage separately compares it
// with the actual result, frozen criteria and registry versions in one write.
func (c TaskReviewContext) Validate() error {
	if c.SchemaVersion != 1 || c.CreatedAt.IsZero() || c.ContentHash != c.Hash() || !validEvaluationCommit(c.TargetCommit) || c.TaskRevision < 1 || c.CriteriaVersion < 1 {
		return fmt.Errorf("invalid sealed task review context")
	}
	for _, id := range []string{c.TaskID, c.AttemptID, string(c.SessionID), c.ResultID, c.LaunchID} {
		if !resultText(id, 200, true) {
			return fmt.Errorf("invalid task review identity")
		}
	}
	for _, hash := range []string{c.CriteriaHash, c.ResultHash, c.ImplementingConfigurationHash} {
		if !validArtifactHash(hash) {
			return fmt.Errorf("invalid task review reference hash")
		}
	}
	if err := (TaskMutation{Actor: c.Actor, Reason: "Review frozen task criteria"}).ValidatePlanning(); err != nil {
		return err
	}
	if err := c.Criteria.Validate(); err != nil {
		return err
	}
	_, criteriaHash, _ := TaskContent(c.Criteria)
	if criteriaHash != c.CriteriaHash || c.Criteria.ReviewPolicy == nil {
		return fmt.Errorf("task review requires exact frozen criteria and reviewer policy")
	}
	if err := c.Reviewer.Validate(); err != nil {
		return err
	}
	if !c.Criteria.ReviewPolicy.Allows(c.ImplementingType, c.Reviewer.AgentType, c.ImplementingHarness, c.Reviewer.Effective.Harness) {
		return fmt.Errorf("reviewer violates frozen independence policy")
	}
	encoded, _, _ := TaskContent(c)
	if len(encoded) > 2<<20 {
		return fmt.Errorf("task review context exceeds 2 MiB")
	}
	return nil
}

// TaskReviewSnapshot exposes the sealed context separately from normal review
// history. StartedAt is a daemon-observed launch, never a reviewer claim.
type TaskReviewSnapshot struct {
	RunID     string            `json:"runId"`
	Context   TaskReviewContext `json:"context"`
	StartedAt *time.Time        `json:"startedAt,omitempty"`
}
