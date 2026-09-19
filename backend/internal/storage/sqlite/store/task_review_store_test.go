package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func taskReviewFixture(t *testing.T, s *sqlite.Store) (domain.TaskResultSubmission, domain.ReviewRun, domain.TaskReviewContext) {
	t.Helper()
	ctx := context.Background()
	definition := registryAgentDefinition()
	definition.AgentType.Harness = domain.HarnessClaudeCode
	definition.AgentType.SessionMode = domain.SessionModeTUI
	entry, err := s.CreateRegistryEntry(ctx, "task-reviewer", domain.RegistryAgentType, registryMetadata("Independent reviewer"), definition, registryMutation(domain.RegistryUser, 0))
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.GetRegistryVersion(ctx, entry.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	criteria := evaluationCriteria()
	criteria.Criteria = append(criteria.Criteria, domain.AcceptanceCriterion{ID: "review", Requirement: "No blocking review findings", EvidenceKind: "review"})
	criteria.ReviewPolicy = &domain.TaskReviewPolicy{AgentTypeID: entry.ID, Version: 1, DifferentAgentType: true, DifferentHarness: true}
	input, result := taskEvaluationFixture(t, s, criteria)
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	prepared, err := s.PrepareTaskReview(ctx, result.ID, actor)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	reviewer := domain.WorkerConfiguration{SchemaVersion: 1, AgentType: domain.WorkerDefinitionRef{ID: entry.ID, Version: 1, Name: entry.Metadata.Name, ContentHash: version.ContentHash}, Selection: domain.WorkerSelection{AgentTypeID: entry.ID, Version: 1}, Effective: *version.Definition.AgentType, Origin: domain.RegistryUser, ActorID: "human", SystemPrompt: "Review independently", CreatedAt: now}
	reviewer.ContentHash = reviewer.Hash()
	snapshot := domain.TaskReviewContext{SchemaVersion: 1, TaskID: result.TaskID, AttemptID: result.AttemptID, SessionID: result.SessionID, ResultID: result.ID, ResultHash: result.ContentHash, TaskRevision: result.TaskRevision, CriteriaVersion: result.CriteriaVersion, Criteria: prepared.Criteria.Definition, CriteriaHash: prepared.Criteria.ContentHash, TargetCommit: result.Definition.ClaimedCommit, ImplementingType: prepared.Implementer.AgentType, ImplementingHarness: prepared.Implementer.Effective.Harness, ImplementingConfigurationHash: result.ConfigurationHash, Reviewer: reviewer, LaunchID: "review-launch", Actor: actor, CreatedAt: now}
	snapshot.ContentHash = snapshot.Hash()
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	run := domain.ReviewRun{ID: "task-review-run", ReviewID: "task-review", SessionID: result.SessionID, Harness: domain.ReviewerHarness(reviewer.Effective.Harness), PRURL: "https://example.test/pr/1", TargetSHA: result.Definition.ClaimedCommit, Status: domain.ReviewRunRunning, CreatedAt: now, TaskScope: snapshot.ScopeHash()}
	if err := s.UpsertReview(ctx, domain.Review{ID: run.ReviewID, SessionID: result.SessionID, ProjectID: "project", Harness: run.Harness, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return input, run, snapshot
}

func TestTaskReviewContextAtomicProvenanceAndLaunchWitness(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, run, snapshot := taskReviewFixture(t, s)
	if err := s.InsertReviewRun(ctx, run); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("generic path bypassed provenance: %v", err)
	}
	for _, actor := range []domain.AdaptiveActor{{Kind: "WORKER", ID: "worker"}, {Kind: "ORCHESTRATOR", ID: "missing", SessionID: "missing"}} {
		if _, err := s.PrepareTaskReview(ctx, snapshot.ResultID, actor); !errors.Is(err, ports.ErrTaskForbidden) {
			t.Fatalf("unowned review preparation: %v", err)
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*domain.TaskReviewContext)
	}{
		{"wrong result hash", func(c *domain.TaskReviewContext) { c.ResultHash = c.CriteriaHash }},
		{"wrong implementing type", func(c *domain.TaskReviewContext) { c.ImplementingType.ID = "other" }},
		{"wrong task", func(c *domain.TaskReviewContext) { c.TaskID = "other" }},
		{"wrong instructions", func(c *domain.TaskReviewContext) { c.Reviewer.Effective.Instructions = "Different from Type" }},
		{"worker provenance", func(c *domain.TaskReviewContext) { c.Actor.Kind = "WORKER" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := snapshot
			tc.change(&bad)
			bad.Reviewer.ContentHash = bad.Reviewer.Hash()
			bad.ContentHash = bad.Hash()
			badRun := run
			badRun.TaskScope = bad.ScopeHash()
			if err := s.InsertTaskReviewRun(ctx, badRun, bad); err == nil {
				t.Fatal("forged provenance accepted")
			}
			if _, found, err := s.GetReviewRun(ctx, run.ID); err != nil || found {
				t.Fatalf("partial run survived rejection: %v %v", found, err)
			}
		})
	}
	if err := s.InsertTaskReviewRun(ctx, run, snapshot); err != nil {
		t.Fatal(err)
	}
	if started, err := s.MarkTaskReviewStarted(ctx, run.ID, snapshot.LaunchID); err != nil || started {
		t.Fatalf("unlaunched review claimed start: %v %v", started, err)
	}
	if err := s.UpsertReview(ctx, domain.Review{ID: run.ReviewID, SessionID: run.SessionID, ProjectID: "project", Harness: run.Harness, ReviewerHandleID: "native-handle", ReviewerLaunchID: snapshot.LaunchID, CreatedAt: run.CreatedAt, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if started, err := s.MarkTaskReviewStarted(ctx, run.ID, "stale-launch"); err != nil || started {
		t.Fatalf("stale launch accepted: %v %v", started, err)
	}
	if started, err := s.MarkTaskReviewStarted(ctx, run.ID, snapshot.LaunchID); err != nil || !started {
		t.Fatalf("observed native launch rejected: %v %v", started, err)
	}
	if started, err := s.MarkTaskReviewStarted(ctx, run.ID, snapshot.LaunchID); err != nil || started {
		t.Fatalf("launch witness rewritten: %v %v", started, err)
	}
	duplicate := run
	duplicate.ID = "duplicate"
	if err := s.InsertTaskReviewRun(ctx, duplicate, snapshot); !errors.Is(err, domain.ErrDuplicateReviewRun) {
		t.Fatalf("duplicate scope accepted: %v", err)
	}
	if _, found, err := s.GetTaskReviewContext(ctx, duplicate.ID); err != nil || found {
		t.Fatalf("partial duplicate context: %v %v", found, err)
	}
	if _, _, err := s.SubmitTaskReviewResult(ctx, domain.TaskReviewSubmission{RunID: run.ID, SessionID: run.SessionID, SourceGeneration: snapshot.LaunchID, Verdict: domain.VerdictApproved, Body: "No blocking findings"}); err != nil {
		t.Fatal(err)
	}
	// The legacy scope remains independent, even for the same PR/head/harness.
	legacy := run
	legacy.ID, legacy.TaskScope = "generic", ""
	if err := s.InsertReviewRun(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "replacement-result", "replacement", 1
	input.Definition.Summary = "Corrected findings at the same commit"
	if _, _, err := s.SubmitTaskResult(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareTaskReview(ctx, snapshot.ResultID, snapshot.Actor); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("obsolete result prepared for new review: %v", err)
	}
	metadata := registryMetadata("Disabled after launch")
	metadata.Enabled = false
	if _, err := s.UpdateRegistryMetadata(ctx, snapshot.Reviewer.AgentType.ID, metadata, registryMutation(domain.RegistryUser, 1)); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	retained, found, err := reopened.GetTaskReviewContext(ctx, run.ID)
	if err != nil || !found || retained.Context.ContentHash != snapshot.ContentHash || retained.StartedAt == nil {
		t.Fatalf("review provenance lost: %+v %v %v", retained, found, err)
	}
	retainedRun, found, err := reopened.GetReviewRun(ctx, run.ID)
	if err != nil || !found || retainedRun.TaskScope != run.TaskScope || retainedRun.Verdict != domain.VerdictApproved {
		t.Fatalf("review lifecycle changed: %+v %v %v", retainedRun, found, err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		"UPDATE adaptive_task_review_contexts SET snapshot='{}' WHERE run_id=?",
		"UPDATE adaptive_task_review_contexts SET started_at=NULL WHERE run_id=?",
		"DELETE FROM adaptive_task_review_contexts WHERE run_id=?",
		"UPDATE review_run SET task_scope='' WHERE id=?",
		"UPDATE review_run SET target_sha='other' WHERE id=?",
	} {
		if _, err := db.ExecContext(ctx, statement, run.ID); err == nil {
			t.Fatalf("retained review context mutated: %s", statement)
		}
	}
}

func TestTaskReviewContextScopeAndReplacementFence(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, run, snapshot := taskReviewFixture(t, s)
	retry := snapshot
	retry.CreatedAt = retry.CreatedAt.Add(time.Second)
	retry.LaunchID, retry.Actor.ID = "retry-launch", "another-observer"
	retry.Reviewer.CreatedAt = retry.CreatedAt
	retry.Reviewer.ContentHash = retry.Reviewer.Hash()
	retry.ContentHash = retry.Hash()
	if retry.Validate() != nil || snapshot.ContentHash == retry.ContentHash || snapshot.ScopeHash() != retry.ScopeHash() {
		t.Fatal("retry changed scope or failed to retain distinct provenance")
	}
	input.ID, input.IdempotencyKey, input.ExpectedVersion = "new-result", "new-result", 1
	input.Definition.Summary = "Replacement while reviewer resolution was in progress"
	if _, _, err := s.SubmitTaskResult(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertTaskReviewRun(ctx, run, snapshot); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("replacement race accepted stale review: %v", err)
	}
	if _, found, err := s.GetReviewRun(ctx, run.ID); err != nil || found {
		t.Fatalf("stale run survived: %v %v", found, err)
	}
}

func TestTaskReviewContextBoundsAndAtomicAudit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	_, run, snapshot := taskReviewFixture(t, s)
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE TRIGGER reject_task_review_audit BEFORE INSERT ON adaptive_task_audit WHEN NEW.action='review_requested' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertTaskReviewRun(ctx, run, snapshot); err == nil {
		t.Fatal("missing audit accepted")
	}
	if _, found, err := s.GetReviewRun(ctx, run.ID); err != nil || found {
		t.Fatalf("run survived audit rollback: %v %v", found, err)
	}
	if _, found, err := s.GetTaskReviewContext(ctx, run.ID); err != nil || found {
		t.Fatalf("context survived audit rollback: %v %v", found, err)
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_task_review_audit"); err != nil {
		t.Fatal(err)
	}
	// Failed review retries remain inspectable and count toward the bound.
	for i := 0; i < 64; i++ {
		run.ID = fmt.Sprintf("review-%d", i)
		if err := s.InsertTaskReviewRun(ctx, run, snapshot); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateReviewRunResult(ctx, run.ID, domain.ReviewRunFailed, domain.VerdictNone, "Provider unavailable", "", false); err != nil {
			t.Fatal(err)
		}
	}
	run.ID = "over-limit"
	if err := s.InsertTaskReviewRun(ctx, run, snapshot); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("unbounded review retries: %v", err)
	}
}
