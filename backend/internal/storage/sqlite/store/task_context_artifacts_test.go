package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/taskcontext"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func artifactResult(t *testing.T, s *sqlite.Store, source domain.TaskMessageSubmission, id string, version int64) domain.TaskResult {
	t.Helper()
	result, _, err := s.SubmitTaskResult(context.Background(), domain.TaskResultSubmission{ID: id, AttemptID: source.AttemptID, SessionID: source.SessionID, SourceOwner: source.SourceOwner, ExpectedActivation: source.ExpectedActivation, ExpectedVersion: version, IdempotencyKey: id, Definition: domain.TaskResultDefinition{SchemaVersion: 1, ClaimedOutcome: "partial", Summary: id, Findings: []string{"Avoid the stale cache"}, UnresolvedIssues: []string{"Verify the exported interface"}, Interfaces: []domain.TaskInterfaceClaim{{Name: "API", Contract: "Versioned identifier"}}, Tests: []domain.TaskTestClaim{{Command: []string{"NEVER_EXECUTE_CLAIM"}, Outcome: "passed", Details: "Unverified worker report"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func artifactContract(t *testing.T, s *sqlite.Store, source domain.TaskMessageSubmission, id string) domain.TaskMessage {
	t.Helper()
	source.ID, source.IdempotencyKey = id, id
	source.Definition.Kind, source.Definition.ReplyToID = "interface_contract", ""
	source.Definition.Interface = &domain.TaskInterfaceClaim{Name: "API", Contract: "Proposed contract " + id}
	message, _, err := s.SubmitTaskMessage(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func nextArtifactContext(t *testing.T, s *sqlite.Store, source domain.TaskMessageSubmission) (domain.TaskContextSnapshot, domain.TaskLease, string) {
	t.Helper()
	ctx := context.Background()
	previous, found, err := s.GetTaskContext(ctx, source.AttemptID)
	if err != nil || !found {
		t.Fatalf("prior context: %v %v", found, err)
	}
	rec, _, err := s.GetSession(ctx, source.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	rec.IsTerminated = true
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	oldLease, err := s.GetTaskLease(ctx, source.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	owner := rec.ControllerOwner()
	if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: oldLease.TaskLeaseToken, SessionID: rec.ID, ObservedOwner: &owner, Now: time.Now().UTC(), Reason: "Fixture confirmed prior worker exit"}); err != nil {
		t.Fatal(err)
	}
	task, err := s.GetAdaptiveTask(ctx, previous.Task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	request := taskReservation("next-attempt", task.ID)
	request.Now, request.TTL, request.Mutation.ExpectedRevision = time.Now().UTC(), 5*time.Minute, task.Revision
	attempt, lease, err := s.ReserveTask(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := s.GetWorkerConfiguration(ctx, source.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	rec.ID, rec.IsTerminated = "", false
	rec.Metadata = domain.SessionMetadata{Permissions: config.Effective.Config.Permissions}
	rec, _, err = s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, config, lease.HeartbeatAt)
	if err != nil {
		t.Fatal(err)
	}
	op := domain.TaskExecutionOperation{ID: "next-native", SessionID: rec.ID, Lease: lease.TaskLeaseToken, SourceOwner: rec.ControllerOwner(), Kind: "dispatch", CreatedAt: lease.HeartbeatAt}
	if _, err := s.BeginTaskExecution(ctx, op); err != nil {
		t.Fatal(err)
	}
	revision, err := s.GetTaskRevision(ctx, task.ID, attempt.TaskRevision)
	if err != nil {
		t.Fatal(err)
	}
	criteria, err := s.GetAcceptanceCriteria(ctx, task.ID, attempt.CriteriaVersion)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := previous
	snapshot.AttemptID, snapshot.SessionID, snapshot.ExecutionOperationID, snapshot.CreatedAt = attempt.ID, rec.ID, op.ID, lease.HeartbeatAt
	snapshot.Task = domain.TaskRevisionRef{TaskID: task.ID, Revision: revision.Number, ContentHash: revision.ContentHash}
	snapshot.CriteriaVersion = criteria.Number
	snapshot.Sources = append([]domain.ContextSource(nil), previous.Sources...)
	snapshot.Sources[0] = inlineContext(t, "task", task.ID, revision.Number, revision.ContentHash, revision.Definition)
	snapshot.Sources[1] = inlineContext(t, "criteria", task.ID, criteria.Number, criteria.ContentHash, criteria.Definition)
	sealContext(t, &snapshot)
	return snapshot, lease, config.SystemPrompt
}

func TestTaskContextIncludesLatestPriorFindingsAndBoundedIncomingContracts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	source, other, _ := taskMessageFixture(t, s, true)
	first := artifactResult(t, s, source, "first-claims", 0)
	latest := artifactResult(t, s, source, "corrected-claims", 1)
	for i := range 10 {
		artifactContract(t, s, other, fmt.Sprintf("contract-%02d", i))
	}
	// Planning revision changes do not rewrite old result attribution.
	if _, err := s.ReviseAcceptanceCriteria(ctx, "work", taskCriteria(), taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	old, err := s.SelectTaskContextResults(ctx, "work", 1, "new-attempt", 4)
	if err != nil || len(old) != 1 || old[0].ID != latest.ID {
		t.Fatalf("latest correction by attempt: %+v %v", old, err)
	}
	if rows, err := s.SelectTaskContextResults(ctx, "work", 2, "new-attempt", 4); err != nil || len(rows) != 0 {
		t.Fatalf("stale revision matched: %+v %v", rows, err)
	}
	if rows, err := s.SelectTaskContextResults(ctx, "work", 0, source.AttemptID, 4); err != nil || len(rows) != 0 {
		t.Fatalf("current attempt included: %+v %v", rows, err)
	}
	seed, lease, systemPrompt := nextArtifactContext(t, s, source)
	snapshot, err := taskcontext.New(s).Build(ctx, ports.TaskContextRequest{Lease: lease.TaskLeaseToken, SessionID: seed.SessionID, ExecutionOperationID: seed.ExecutionOperationID, WorkspacePath: t.TempDir(), Prompt: "Retry the bounded task", SystemPrompt: systemPrompt})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.Prompt, "corrected-claims") || strings.Contains(snapshot.Prompt, "first-claims") || strings.Contains(snapshot.Prompt, "NEVER_EXECUTE_CLAIM") || !strings.Contains(snapshot.Prompt, "Avoid the stale cache") {
		t.Fatalf("wrong prior evidence: %s", snapshot.Prompt)
	}
	results, contracts, omissions := 0, 0, 0
	for _, item := range snapshot.Sources {
		switch item.Kind {
		case "result":
			results++
			if item.ID != latest.ID || item.Version != 2 || item.SourceHash != latest.ContentHash {
				t.Fatalf("lost original result provenance: %+v", item)
			}
		case "interface_contract":
			contracts++
		case "selection":
			if item.ID == "interface-candidate-limit" && item.Disposition == "omitted" {
				omissions++
			}
		}
	}
	if results != 1 || contracts != 8 || omissions != 1 {
		t.Fatalf("unbounded/hidden selection: results=%d contracts=%d omissions=%d", results, contracts, omissions)
	}
	artifactContract(t, s, other, "new-contract-after-launch")
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	retained, found, err := reopened.GetTaskContext(ctx, lease.AttemptID)
	if err != nil || !found || retained.ContentHash != snapshot.ContentHash || strings.Contains(retained.Prompt, "new-contract-after-launch") {
		t.Fatalf("historical context changed: %v %v", found, err)
	}
	if original, err := reopened.GetTaskResult(ctx, first.ID); err != nil || original.ContentHash != first.ContentHash {
		t.Fatalf("original claims lost: %+v %v", original, err)
	}
}

func TestTaskContextSealRejectsForgedAndUnrelatedWorkerArtifacts(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	source, other, _ := taskMessageFixture(t, s, true)
	result := artifactResult(t, s, source, "prior", 0)
	foreign := artifactResult(t, s, other, "unrelated", 0)
	incoming := artifactContract(t, s, other, "incoming")
	outgoing := artifactContract(t, s, source, "outgoing")
	seed, lease, _ := nextArtifactContext(t, s, source)
	valid := inlineContext(t, "result", result.ID, result.Number, result.ContentHash, result.ContextFacts())
	for _, tc := range []struct {
		name     string
		artifact domain.ContextSource
	}{
		{"unrelated_result", inlineContext(t, "result", foreign.ID, foreign.Number, foreign.ContentHash, foreign.ContextFacts())},
		{"wrong_target", inlineContext(t, "interface_contract", outgoing.ID, 1, outgoing.ContentHash, outgoing)},
		{"wrong_version", inlineContext(t, "interface_contract", incoming.ID, 2, incoming.ContentHash, incoming)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := seed
			snapshot.Sources = append(append([]domain.ContextSource(nil), seed.Sources...), tc.artifact)
			sealContext(t, &snapshot)
			if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); !errors.Is(err, ports.ErrTaskConflict) {
				t.Fatalf("forged artifact sealed: %v", err)
			}
		})
	}
	forged := result.ContextFacts()
	forged.Summary = "Worker claims changed into verified completion"
	snapshot := seed
	snapshot.Sources = append(append([]domain.ContextSource(nil), seed.Sources...), inlineContext(t, "result", result.ID, result.Number, result.ContentHash, forged))
	sealContext(t, &snapshot)
	if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("projection rewritten: %v", err)
	}
	snapshot.Sources = append(append([]domain.ContextSource(nil), seed.Sources...), valid, inlineContext(t, "interface_contract", incoming.ID, 1, incoming.ContentHash, incoming))
	sealContext(t, &snapshot)
	if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); err != nil {
		t.Fatalf("exact prior artifacts rejected: %v", err)
	}
}

func TestTaskContextDependencyResultsMatchReservedRevision(t *testing.T) {
	for _, replanBeforeReservation := range []bool{false, true} {
		t.Run(fmt.Sprintf("replan_before_reservation_%v", replanBeforeReservation), func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			source, other, _ := taskMessageFixture(t, s, true)
			result := artifactResult(t, s, other, "dependency-claims", 0)
			definition := taskDefinition()
			definition.Dependencies = []string{"target"}
			if _, err := s.ReviseAdaptiveTask(ctx, "work", definition, taskMutation(1)); err != nil {
				t.Fatal(err)
			}
			replan := func() {
				t.Helper()
				if _, err := s.ReviseAdaptiveTask(ctx, "target", taskDefinition(), taskMutation(1)); err != nil {
					t.Fatal(err)
				}
			}
			if replanBeforeReservation {
				replan()
			}
			seed, lease, systemPrompt := nextArtifactContext(t, s, source)
			if !replanBeforeReservation {
				replan()
			} else {
				forged := seed
				forged.Sources = append(append([]domain.ContextSource(nil), seed.Sources...), inlineContext(t, "result", result.ID, result.Number, result.ContentHash, result.ContextFacts()))
				sealContext(t, &forged)
				if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, forged); !errors.Is(err, ports.ErrTaskConflict) {
					t.Fatalf("result from wrong dependency revision sealed: %v", err)
				}
			}
			snapshot, err := taskcontext.New(s).Build(ctx, ports.TaskContextRequest{Lease: lease.TaskLeaseToken, SessionID: seed.SessionID, ExecutionOperationID: seed.ExecutionOperationID, WorkspacePath: t.TempDir(), Prompt: "Consume the pinned dependency", SystemPrompt: systemPrompt})
			if err != nil {
				t.Fatal(err)
			}
			included := false
			for _, item := range snapshot.Sources {
				if item.Kind == "result" && item.ID == result.ID && item.SourceHash == result.ContentHash {
					included = true
				}
			}
			if included == replanBeforeReservation {
				t.Fatalf("dependency result selection ignored reserved revision: included=%v", included)
			}
		})
	}
}
