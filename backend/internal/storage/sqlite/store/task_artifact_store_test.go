package store_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/taskverify"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/process"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

func TestTaskArtifactServiceCollectsAfterAuthorizationAndRetainsRetry(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := process.Command("git", append([]string{"-C", repo}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "artifact.txt"), []byte("expected artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "artifact")
	commit := git("rev-parse", "HEAD")
	input, _ := taskEvaluationFixture(t, s, domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "artifact", Requirement: "Exact artifact", EvidenceKind: "artifact", ArtifactPath: "artifact.txt", ArtifactSHA256: domain.ContextTextHash("expected artifact")}}})
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "artifact-result", "artifact-result", 1
	input.Definition.ClaimedCommit = commit
	result, _, err := s.SubmitTaskResult(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, err := s.GetSession(ctx, result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Metadata.WorkspacePath = repo
	if err := s.UpdateSession(ctx, worker); err != nil {
		t.Fatal(err)
	}
	collector := &countingArtifactCollector{}
	service := tasksvc.New(s, tasksvc.WithArtifactCollector(collector))
	request := tasksvc.EvaluationInput{ResultID: result.ID, IdempotencyKey: "artifact-evaluation", Reason: "Verify artifact independently"}
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	if _, err := service.Evaluate(ctx, domain.AdaptiveActor{Kind: "WORKER", ID: string(worker.ID), SessionID: worker.ID}, result.TaskID, result.AttemptID, request); err == nil || collector.calls != 0 {
		t.Fatalf("unauthorized request reached local Git: %d %v", collector.calls, err)
	}
	first, err := service.Evaluate(ctx, actor, result.TaskID, result.AttemptID, request)
	if err != nil || !first.Created || first.Evaluation.Definition.Outcome != "passed" || collector.calls != 1 {
		t.Fatalf("independent artifact: %+v %v", first, err)
	}
	collector.fail = true
	replay, err := service.Evaluate(ctx, actor, result.TaskID, result.AttemptID, request)
	if err != nil || replay.Created || replay.Evaluation.ContentHash != first.Evaluation.ContentHash || collector.calls != 1 {
		t.Fatalf("retry accessed unavailable source: %+v %v", replay, err)
	}
	request.ExpectedVersion, request.IdempotencyKey = 1, "fresh"
	if _, err := service.Evaluate(ctx, actor, result.TaskID, result.AttemptID, request); err == nil {
		t.Fatal("failed collector persisted assessment")
	}
	latest, _, err := s.LatestTaskEvaluation(ctx, result.AttemptID)
	if err != nil || latest.ID != first.Evaluation.ID {
		t.Fatalf("failed collection changed history: %+v %v", latest, err)
	}
	collector.fail = false
	collector.during = func() error {
		input.ID, input.IdempotencyKey, input.ExpectedVersion = "racing-result", "racing-result", 2
		_, _, err := s.SubmitTaskResult(ctx, input)
		return err
	}
	if _, err := service.Evaluate(ctx, actor, result.TaskID, result.AttemptID, request); err == nil {
		t.Fatal("result changed during collection but stale assessment committed")
	}
	latest, _, err = s.LatestTaskEvaluation(ctx, result.AttemptID)
	if err != nil || latest.ID != first.Evaluation.ID {
		t.Fatalf("racing collector changed assessment history: %+v %v", latest, err)
	}
}

type countingArtifactCollector struct {
	calls  int
	fail   bool
	during func() error
}

func (c *countingArtifactCollector) Collect(ctx context.Context, plan domain.TaskEvaluationPreparation) ([]domain.TaskArtifactEvidence, error) {
	c.calls++
	if c.fail {
		return nil, os.ErrNotExist
	}
	if c.during != nil {
		if err := c.during(); err != nil {
			return nil, err
		}
	}
	return (taskverify.Artifacts{}).Collect(ctx, plan)
}
