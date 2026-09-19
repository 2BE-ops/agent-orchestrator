package domain

import (
	"encoding/hex"
	"fmt"
	"time"
)

// TaskCICheckEvidence is an independently collected SCM observation, not a
// worker test claim. Opaque logs are excluded; exact check/head/time facts remain.
type TaskCICheckEvidence struct {
	PRURL        string        `json:"prUrl"`
	URL          string        `json:"url"`
	HeadCommit   string        `json:"headCommit"`
	Name         string        `json:"name"`
	TargetCommit string        `json:"targetCommit"`
	Status       PRCheckStatus `json:"status"`
	Conclusion   string        `json:"conclusion"`
	ObservedAt   time.Time     `json:"observedAt"`
	SnapshotAt   time.Time     `json:"snapshotAt"`
}

// TaskCriterionEvaluation separates absent or stale evidence from a failed check.
type TaskCriterionEvaluation struct {
	CriterionID string `json:"criterionId"`
	Outcome     string `json:"outcome" enum:"passed,failed,inconclusive"`
	Reason      string `json:"reason"`
}

// TaskEvaluationDefinition retains deterministic decisions and their inputs.
// Other evidence collectors extend this envelope without rewriting old records.
type TaskEvaluationDefinition struct {
	SchemaVersion   int                       `json:"schemaVersion"`
	TargetCommit    string                    `json:"targetCommit"`
	CriteriaHash    string                    `json:"criteriaHash"`
	ResultHash      string                    `json:"resultHash"`
	Checks          []TaskCICheckEvidence     `json:"checks"`
	ChecksTruncated bool                      `json:"checksTruncated"`
	Criteria        []TaskCriterionEvaluation `json:"criteria"`
	Outcome         string                    `json:"outcome" enum:"passed,failed,inconclusive"`
	Reason          string                    `json:"reason"`
}

// TaskEvaluationAttribution pins the worker configuration at result submission.
// Skill association is provenance, never a claim that a Skill caused the outcome.
type TaskEvaluationAttribution struct {
	AgentType             WorkerDefinitionRef   `json:"agentType"`
	Skills                []WorkerDefinitionRef `json:"skills"`
	Harness               AgentHarness          `json:"harness"`
	Mode                  SessionMode           `json:"mode"`
	Model                 string                `json:"model"`
	Category              string                `json:"category"`
	AttemptNumber         int64                 `json:"attemptNumber"`
	ResultNumber          int64                 `json:"resultNumber"`
	ConfigurationHash     string                `json:"configurationHash"`
	ConfigurationSequence int64                 `json:"configurationSequence"`
}

// TaskEvaluation is immutable evidence; task display state remains derived.
type TaskEvaluation struct {
	ID              string                    `json:"id"`
	ProjectID       ProjectID                 `json:"projectId"`
	TaskID          string                    `json:"taskId"`
	AttemptID       string                    `json:"attemptId"`
	ResultID        string                    `json:"resultId"`
	Number          int64                     `json:"number"`
	TaskRevision    int64                     `json:"taskRevision"`
	CriteriaVersion int64                     `json:"criteriaVersion"`
	ContextHash     string                    `json:"contextHash"`
	Attribution     TaskEvaluationAttribution `json:"attribution"`
	Definition      TaskEvaluationDefinition  `json:"definition"`
	Actor           AdaptiveActor             `json:"actor"`
	Reason          string                    `json:"reason"`
	ContentHash     string                    `json:"contentHash"`
	CreatedAt       time.Time                 `json:"createdAt"`
}

// TaskEvaluationRequest requests independent collection; it contains no caller
// supplied verdict or evidence. Exact retries retain the original observation.
type TaskEvaluationRequest struct {
	ID              string
	ResultID        string
	ExpectedVersion int64
	IdempotencyKey  string
	Mutation        TaskMutation
}

// Hash seals evidence, attribution and observation time together.
func (e TaskEvaluation) Hash() string {
	e.ContentHash = ""
	_, hash, _ := TaskContent(e)
	return hash
}

// EvaluateTaskCriteria consumes only observed check facts, never worker claims.
// Check membership must belong to the retained complete SCM snapshot. Its time
// is provenance, not an assertion that an unchanged check was just fetched.
func EvaluateTaskCriteria(criteria AcceptanceCriteria, target string, checks []TaskCICheckEvidence, truncated bool, now time.Time) ([]TaskCriterionEvaluation, string, string) {
	decisions := make([]TaskCriterionEvaluation, 0, len(criteria.Criteria))
	outcome := "passed"
	for _, criterion := range criteria.Criteria {
		decision := TaskCriterionEvaluation{CriterionID: criterion.ID, Outcome: "inconclusive", Reason: "Independent evidence has not been collected for this criterion"}
		if criterion.EvidenceKind == "ci" && validEvaluationCommit(target) {
			decision.Outcome, decision.Reason = evaluateCICriterion(criterion, target, checks, now)
		}
		if decision.Outcome == "failed" {
			outcome = "failed"
		} else if decision.Outcome != "passed" && outcome != "failed" {
			outcome = "inconclusive"
		}
		decisions = append(decisions, decision)
	}
	reason := "All frozen criteria have independent passing evidence for the exact target commit"
	if outcome == "failed" {
		reason = "Independent evidence fails at least one frozen criterion"
	}
	if outcome == "inconclusive" {
		reason = "Missing, pending, stale or unsupported evidence prevents completion"
	}
	if len(decisions) == 0 || !validEvaluationCommit(target) || truncated {
		if outcome != "failed" {
			outcome = "inconclusive"
		}
		reason = "A complete evidence set and exact target commit are required"
	}
	return decisions, outcome, reason
}

func evaluateCICriterion(criterion AcceptanceCriterion, target string, checks []TaskCICheckEvidence, now time.Time) (string, string) {
	if len(criterion.CheckNames) == 0 {
		return "inconclusive", "No exact required CI check names were frozen"
	}
	incomplete := false
	for _, name := range criterion.CheckNames {
		found := false
		for _, check := range checks {
			if check.Name != name || check.TargetCommit != target || check.HeadCommit != target {
				continue
			}
			if check.ObservedAt.IsZero() || !check.ObservedAt.Equal(check.SnapshotAt) || check.ObservedAt.After(now.Add(30*time.Second)) {
				continue
			}
			found = true
			if check.Status == PRCheckFailed {
				return "failed", "A required check failed on the exact target commit"
			}
			if check.Status != PRCheckPassed || check.Conclusion != "success" {
				incomplete = true
			}
		}
		if !found {
			incomplete = true
		}
	}
	if incomplete {
		return "inconclusive", "Required checks are missing, pending, skipped, cancelled or stale"
	}
	return "passed", "Every named check passed on the exact target commit in the retained complete SCM snapshot"
}

func validEvaluationCommit(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Validate verifies bounded retained facts. The store additionally derives and
// compares decisions from the exact frozen criteria inside its transaction.
func (d TaskEvaluationDefinition) Validate() error {
	if d.SchemaVersion != 1 || len(d.Criteria) < 1 || len(d.Criteria) > 64 || len(d.Checks) > 128 || len(d.CriteriaHash) != 64 || len(d.ResultHash) != 64 || (d.TargetCommit != "" && !validEvaluationCommit(d.TargetCommit)) || !resultText(d.Reason, 2000, true) {
		return fmt.Errorf("invalid evaluation evidence bounds")
	}
	if d.Outcome != "passed" && d.Outcome != "failed" && d.Outcome != "inconclusive" {
		return fmt.Errorf("invalid evaluation outcome")
	}
	seen := map[string]bool{}
	for _, criterion := range d.Criteria {
		if !resultText(criterion.CriterionID, 100, true) || seen[criterion.CriterionID] || !resultText(criterion.Reason, 2000, true) || (criterion.Outcome != "passed" && criterion.Outcome != "failed" && criterion.Outcome != "inconclusive") {
			return fmt.Errorf("invalid criterion decision")
		}
		seen[criterion.CriterionID] = true
	}
	for _, check := range d.Checks {
		if !resultText(check.PRURL, 2000, true) || !resultText(check.URL, 2000, false) || !resultText(check.Name, 300, true) || len(check.HeadCommit) > 64 || len(check.TargetCommit) > 64 || len(check.Status) > 100 || len(check.Conclusion) > 100 {
			return fmt.Errorf("invalid collected CI evidence")
		}
	}
	content, _, err := TaskContent(d)
	if err != nil {
		return err
	}
	if len(content) > 256<<10 {
		return fmt.Errorf("evaluation exceeds 256 KiB")
	}
	return nil
}
