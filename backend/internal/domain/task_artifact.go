package domain

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// TaskArtifactEvidence is a daemon-collected Git blob observation. Local dirty
// files, worker-supplied hashes and Git attribute filters are not its source.
type TaskArtifactEvidence struct {
	Collector    string    `json:"collector"`
	CriterionID  string    `json:"criterionId"`
	TargetCommit string    `json:"targetCommit"`
	Path         string    `json:"path"`
	GitBlobID    string    `json:"gitBlobId,omitempty"`
	SHA256       string    `json:"sha256,omitempty"`
	Bytes        int64     `json:"bytes"`
	State        string    `json:"state" enum:"observed,missing,unavailable,unsupported,oversized"`
	Reason       string    `json:"reason"`
	ObservedAt   time.Time `json:"observedAt"`
}

func validArtifactHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Validate bounds independently collected blob facts without inferring success.
func (e TaskArtifactEvidence) Validate() error {
	if e.Collector != "git-blob/v1" || !resultText(e.CriterionID, 100, true) || !validEvaluationCommit(e.TargetCommit) || !ValidContextFilePath(e.Path) || e.Bytes < 0 || !resultText(e.Reason, 300, true) || e.ObservedAt.IsZero() {
		return fmt.Errorf("invalid artifact observation")
	}
	switch e.State {
	case "observed":
		if !validEvaluationCommit(e.GitBlobID) || !validArtifactHash(e.SHA256) || e.Bytes > 1<<20 {
			return fmt.Errorf("invalid observed artifact hash or size")
		}
	case "missing", "unavailable", "unsupported", "oversized":
		if e.SHA256 != "" {
			return fmt.Errorf("unobserved artifact cannot carry a hash")
		}
	default:
		return fmt.Errorf("invalid artifact observation state")
	}
	return nil
}

func evaluateTaskArtifact(criterion AcceptanceCriterion, target string, evidence *TaskObservationEvidence, now time.Time) (string, string) {
	if criterion.ArtifactSHA256 == "" || evidence == nil {
		return "inconclusive", "A frozen expected artifact hash and independent Git observation are required"
	}
	for _, artifact := range evidence.Artifacts {
		if artifact.Validate() != nil || artifact.CriterionID != criterion.ID || artifact.Path != criterion.ArtifactPath || artifact.TargetCommit != target || artifact.ObservedAt.After(now.Add(30*time.Second)) {
			continue
		}
		if artifact.State == "missing" {
			return "failed", "Expected artifact is absent from the exact result commit"
		}
		if artifact.State != "observed" {
			return "inconclusive", "Artifact could not be independently read within verification limits"
		}
		if !strings.EqualFold(artifact.SHA256, criterion.ArtifactSHA256) {
			return "failed", "Git artifact content differs from the frozen expected SHA-256"
		}
		return "passed", "Git artifact at the exact result commit matches the frozen expected SHA-256"
	}
	return "inconclusive", "No independent artifact observation matches the frozen criterion"
}

// TaskEvaluationPreparation contains server-owned input for read-only collectors.
// It is never serialized as a public request or used to launch worker processes.
type TaskEvaluationPreparation struct {
	Result        TaskResult
	Criteria      AcceptanceCriteriaVersion
	WorkspacePath string
	Existing      *TaskEvaluation
}
