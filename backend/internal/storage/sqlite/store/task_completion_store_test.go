package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestTaskCompletionDerivesCurrentEvidenceWithoutReleasingOwnership(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, result := taskEvaluationFixture(t, s, evaluationCriteria())
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	assert := func(want bool) domain.TaskCompletion {
		t.Helper()
		proof, err := s.GetTaskCompletion(ctx, result.TaskID, result.TaskRevision)
		if err != nil || proof.Verified != want {
			t.Fatalf("completion=%+v want=%v err=%v", proof, want, err)
		}
		return proof
	}
	assert(false)
	passed, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "passed", 0))
	if err != nil {
		t.Fatal(err)
	}
	if proof := assert(true); proof.EvaluationID != passed.ID || proof.ResultID != result.ID {
		t.Fatalf("lost completion evidence: %+v", proof)
	}
	view, err := tasksvc.New(s).Get(ctx, result.TaskID)
	if err != nil || view.State.Phase != "completed" || view.Lease == nil || !view.Completion.Verified {
		t.Fatalf("completion released ownership or lost proof: %+v %v", view, err)
	}
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckFailed)
	assert(false)
	historical, err := s.GetTaskEvaluation(ctx, passed.ID)
	if err != nil || historical.ContentHash != passed.ContentHash || historical.Definition.Outcome != "passed" {
		t.Fatalf("current failure rewrote history: %+v %v", historical, err)
	}
	evaluationChecks(t, s, result, strings.Repeat("b", 40), domain.PRCheckPassed)
	assert(false)
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	assert(true)
	mutation := domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, ExpectedRevision: result.TaskRevision, Reason: "Cancel current work"}
	if _, err := s.ChangeTaskIntent(ctx, result.TaskID, domain.TaskIntentChange{Intent: "cancel", Mutation: mutation}); err != nil {
		t.Fatal(err)
	}
	assert(false)
	view, err = tasksvc.New(s).Get(ctx, result.TaskID)
	if err != nil || view.State.Phase != "cancelling" || view.Lease == nil {
		t.Fatalf("completion bypassed cancellation: %+v %v", view, err)
	}
	if _, err := s.ChangeTaskIntent(ctx, result.TaskID, domain.TaskIntentChange{Intent: "run", ExpectedVersion: 1, Mutation: mutation}); err != nil {
		t.Fatal(err)
	}
	assert(true)
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "correction", "correction", 1
	input.Definition.Summary = "Corrected implementation claims at the same commit"
	result, _, err = s.SubmitTaskResult(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	assert(false)
	if _, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "reassessed", 1)); err != nil {
		t.Fatal(err)
	}
	assert(true)
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if proof, err := reopened.GetTaskCompletion(ctx, result.TaskID, result.TaskRevision); err != nil || !proof.Verified {
		t.Fatalf("completion after reopen: %+v %v", proof, err)
	}
	criteria := evaluationCriteria()
	criteria.Criteria[0].Requirement = "Revised acceptance intent"
	if _, err := s.ReviseAcceptanceCriteria(ctx, result.TaskID, criteria, domain.TaskMutation{Actor: mutation.Actor, ExpectedRevision: result.TaskRevision, Reason: "Revise criteria"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTaskCompletion(ctx, result.TaskID, result.TaskRevision); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("stale view revision accepted: %v", err)
	}
	if proof, err := s.GetTaskCompletion(ctx, result.TaskID, result.TaskRevision+1); err != nil || proof.Verified {
		t.Fatalf("new task revision inherited completion: %+v %v", proof, err)
	}
}

func TestTaskCompletionRetainsImmutableArtifactButFencesChangedPRHead(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	hash := strings.Repeat("a", 64)
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "artifact", Requirement: "Published contract matches", EvidenceKind: "artifact", ArtifactPath: "contract.json", ArtifactSHA256: hash}}}
	_, result := taskEvaluationFixture(t, s, criteria)
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	input := evaluationRequest(result, "artifact", 0)
	input.Artifacts = []domain.TaskArtifactEvidence{{Collector: "git-blob/v1", CriterionID: "artifact", TargetCommit: result.Definition.ClaimedCommit, Path: "contract.json", GitBlobID: strings.Repeat("b", 40), SHA256: hash, Bytes: 20, State: "observed", Reason: "Read immutable Git blob", ObservedAt: time.Now().UTC()}}
	if _, _, err := s.EvaluateTaskResult(ctx, input); err != nil {
		t.Fatal(err)
	}
	if proof, err := s.GetTaskCompletion(ctx, result.TaskID, result.TaskRevision); err != nil || !proof.Verified {
		t.Fatalf("immutable artifact lost verification: %+v %v", proof, err)
	}
	evaluationChecks(t, s, result, strings.Repeat("c", 40), domain.PRCheckPassed)
	if proof, err := s.GetTaskCompletion(ctx, result.TaskID, result.TaskRevision); err != nil || proof.Verified {
		t.Fatalf("changed PR retained old completion: %+v %v", proof, err)
	}
}

func TestTaskCompletionNewAttemptDoesNotReusePriorAssessment(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, result := taskEvaluationFixture(t, s, evaluationCriteria())
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	if _, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "passed", 0)); err != nil {
		t.Fatal(err)
	}
	rec, _, err := s.GetSession(ctx, result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	rec.IsTerminated = true
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	lease, err := s.GetTaskLease(ctx, result.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	owner := rec.ControllerOwner()
	if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, SessionID: rec.ID, ObservedOwner: &owner, Now: time.Now().UTC(), Reason: "Confirmed native exit"}); err != nil {
		t.Fatal(err)
	}
	view, err := tasksvc.New(s).Get(ctx, result.TaskID)
	if err != nil || view.State.Phase != "completed" || view.Lease != nil {
		t.Fatalf("completion after native exit: %+v %v", view, err)
	}
	request := taskReservation("retry-after-assessment", result.TaskID)
	request.Mutation.ExpectedRevision = result.TaskRevision
	if _, _, err := s.ReserveTask(ctx, request); err != nil {
		t.Fatal(err)
	}
	if proof, err := s.GetTaskCompletion(ctx, result.TaskID, result.TaskRevision); err != nil || proof.Verified || proof.AttemptID != request.ID || proof.ResultID != "" {
		t.Fatalf("new attempt reused previous assessment: %+v %v", proof, err)
	}
}
