package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestTaskArtifactRequiresFrozenExpectedHashAndExactIndependentSource(t *testing.T) {
	now := time.Now().UTC()
	commit, hash := strings.Repeat("a", 40), domain.ContextTextHash("expected artifact")
	criterion := domain.AcceptanceCriterion{ID: "artifact", Requirement: "Artifact has expected content", EvidenceKind: "artifact", ArtifactPath: "result.txt", ArtifactSHA256: hash}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{criterion}}
	if err := criteria.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*domain.TaskArtifactEvidence)
		want   string
	}{
		{"passed", func(*domain.TaskArtifactEvidence) {}, "passed"},
		{"mismatch", func(e *domain.TaskArtifactEvidence) { e.SHA256 = domain.ContextTextHash("different") }, "failed"},
		{"missing", func(e *domain.TaskArtifactEvidence) { e.State = "missing"; e.SHA256 = "" }, "failed"},
		{"unavailable", func(e *domain.TaskArtifactEvidence) { e.State = "unavailable"; e.SHA256 = "" }, "inconclusive"},
		{"wrong_commit", func(e *domain.TaskArtifactEvidence) { e.TargetCommit = strings.Repeat("b", 40) }, "inconclusive"},
		{"wrong_path", func(e *domain.TaskArtifactEvidence) { e.Path = "other.txt" }, "inconclusive"},
		{"future", func(e *domain.TaskArtifactEvidence) { e.ObservedAt = now.Add(time.Minute) }, "inconclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := domain.TaskArtifactEvidence{Collector: "git-blob/v1", CriterionID: "artifact", TargetCommit: commit, Path: "result.txt", GitBlobID: strings.Repeat("b", 40), SHA256: hash, State: "observed", Reason: "raw_git_blob_read", ObservedAt: now}
			tc.mutate(&evidence)
			_, outcome, _ := domain.EvaluateTaskEvidence(criteria, commit, nil, false, &domain.TaskObservationEvidence{Artifacts: []domain.TaskArtifactEvidence{evidence}}, now)
			if outcome != tc.want {
				t.Fatalf("artifact outcome: %s want %s", outcome, tc.want)
			}
		})
	}
	criteria.Criteria[0].ArtifactSHA256 = "not-a-hash"
	if err := criteria.Validate(); err == nil {
		t.Fatal("invalid frozen hash accepted")
	}
	criteria.Criteria[0].ArtifactSHA256 = hash
	criteria.Criteria[0].ArtifactPath = "../outside"
	if err := criteria.Validate(); err == nil {
		t.Fatal("nonportable artifact path accepted")
	}
}
