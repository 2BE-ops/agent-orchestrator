package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func goalActor() domain.AdaptiveActor {
	return domain.AdaptiveActor{Kind: "USER", ID: "local-user"}
}

func setGoal(t *testing.T, s *sqlite.Store, project, goal string) domain.ProjectGoalVersion {
	t.Helper()
	version, err := s.SetProjectGoal(context.Background(), domain.ProjectID(project), goal, "Record the project goal", goalActor())
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func orchestratorSession(t *testing.T, s *sqlite.Store, project string) domain.SessionRecord {
	t.Helper()
	rec := sampleRecord(project)
	rec.Kind = domain.KindOrchestrator
	rec.Metadata.RuntimeLaunchID = "launch-1"
	created, err := s.CreateSession(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func planSubmission(session domain.SessionID, key string, action domain.OrchestratorPlanAction) domain.OrchestratorPlanSubmission {
	newTaskID := ""
	if action.Action == "create_task" {
		newTaskID = "task-" + key
	}
	return domain.OrchestratorPlanSubmission{ReceiptID: "receipt-" + key, ProjectID: "project", SessionID: session, IdempotencyKey: key, Action: action, NewTaskID: newTaskID, Now: time.Now().UTC()}
}

func TestProjectGoalVersioningAndAuthority(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	first := setGoal(t, s, "project", "Ship the adaptive scheduling milestone")
	if first.Number != 1 || first.ContentHash == "" {
		t.Fatalf("first goal version: %+v", first)
	}
	second := setGoal(t, s, "project", "Ship the milestone plus documentation")
	if second.Number != 2 {
		t.Fatalf("goal versioning is not append-only: %+v", second)
	}
	current, err := s.GetProjectGoal(ctx, "project")
	if err != nil || current.Number != 2 || current.Goal != "Ship the milestone plus documentation" {
		t.Fatalf("current goal: %+v %v", current, err)
	}
	versions, err := s.ListProjectGoalVersions(ctx, "project", 0, 100)
	if err != nil || len(versions) != 2 || versions[0].Number != 1 || versions[1].Number != 2 {
		t.Fatalf("goal history: %+v %v", versions, err)
	}
	if _, err := s.GetProjectGoal(ctx, "missing"); !errors.Is(err, ports.ErrGoalNotFound) {
		t.Fatalf("unknown project goal: %v", err)
	}
	if _, err := s.SetProjectGoal(ctx, "project", "Rewrite user intent", "self-serving edit", domain.AdaptiveActor{Kind: "ORCHESTRATOR", ID: "orch", SessionID: "orch"}); !errors.Is(err, ports.ErrGoalInvalid) {
		t.Fatalf("orchestrator rewrote the user goal: %v", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `UPDATE adaptive_project_goal_versions SET goal='forged' WHERE project_id='project' AND number=1`); err == nil {
		t.Fatal("goal version history is mutable")
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if current, err := reopened.GetProjectGoal(ctx, "project"); err != nil || current.Number != 2 {
		t.Fatalf("goal after reopen: %+v %v", current, err)
	}
}

func TestProjectGoalCompletionVerifiesDurableFacts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	goal := setGoal(t, s, "project", "Deliver the verified worker pipeline")
	_, result := taskEvaluationFixture(t, s, evaluationCriteria())
	evaluationChecks(t, s, result, result.Definition.ClaimedCommit, domain.PRCheckPassed)
	if _, _, err := s.EvaluateTaskResult(ctx, evaluationRequest(result, "passed", 0)); err != nil {
		t.Fatal(err)
	}
	createTask(t, s, "unverified", taskDefinition())
	request := domain.ProjectGoalCompletionRequest{ID: "completion-1", ProjectID: "project", GoalVersion: goal.Number, Summary: "Pipeline delivered and independently verified", Actor: domain.AdaptiveActor{Kind: "ORCHESTRATOR", ID: "orch", SessionID: "orch"}, Reason: "All goal work is terminal", Now: time.Now().UTC()}
	completion, created, err := s.CompleteProjectGoal(ctx, request)
	if err == nil || created || !errors.Is(err, ports.ErrGoalIncomplete) {
		t.Fatalf("unverified task did not block completion: %+v %v", completion, err)
	}
	var blockers domain.ProjectGoalBlockers
	if !errors.As(err, &blockers) || len(blockers.Items) != 1 || blockers.Items[0].TaskID != "unverified" {
		t.Fatalf("blockers not derived: %v", err)
	}
	definition := taskDefinition()
	definition.Title = "Cancelled follow-up"
	if _, err := s.CreateAdaptiveTask(ctx, "cancelled", "project", definition, nil, taskMutation(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeTaskIntent(ctx, "cancelled", domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeTaskIntent(ctx, "unverified", domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)}); err != nil {
		t.Fatal(err)
	}
	completion, created, err = s.CompleteProjectGoal(ctx, request)
	if err != nil || !created {
		t.Fatalf("verified goal completion refused: %+v %v", completion, err)
	}
	states := map[string]string{}
	for _, task := range completion.Evidence.VerifiedTasks {
		states[task.TaskID] = task.State
	}
	if states["work"] != "completed" || states["cancelled"] != "cancelled" || states["unverified"] != "cancelled" {
		t.Fatalf("completion evidence: %+v", states)
	}
	replay, created, err := s.CompleteProjectGoal(ctx, request)
	if err != nil || created || replay.ID != completion.ID {
		t.Fatalf("completion replay is not idempotent: %+v %v", replay, err)
	}
	setGoal(t, s, "project", "Extend the pipeline with reporting")
	if _, _, err := s.CompleteProjectGoal(ctx, request); !errors.Is(err, ports.ErrGoalConflict) {
		t.Fatalf("superseded goal version completed: %v", err)
	}
}

func TestProjectGoalCompletionRefusesEmptyProjects(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpenAt(t, t.TempDir())
	seedProject(t, s, "project")
	goal := setGoal(t, s, "project", "Empty milestone")
	request := domain.ProjectGoalCompletionRequest{ID: "completion-empty", ProjectID: "project", GoalVersion: goal.Number, Summary: "Nothing to verify", Actor: goalActor(), Reason: "Vacuous completion attempt", Now: time.Now().UTC()}
	if _, _, err := s.CompleteProjectGoal(ctx, request); !errors.Is(err, ports.ErrGoalIncomplete) {
		t.Fatalf("empty project completed its goal: %v", err)
	}
}

func TestOrchestratorPlanReceiptsSealAndReplay(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	orch := orchestratorSession(t, s, "project")
	definition := taskDefinition()
	definition.Title = "Orchestrator planned follow-up"
	submission := planSubmission(orch.ID, "plan-1", domain.OrchestratorPlanAction{Action: "create_task", Definition: &definition, Reason: "Decompose the verified goal"})
	receipt, created, err := s.ApplyOrchestratorPlan(ctx, submission)
	if err != nil || !created {
		t.Fatalf("planning action rejected: %+v %v", receipt, err)
	}
	if receipt.Outcome.TaskID != "task-plan-1" || receipt.Outcome.Revision != 1 || receipt.Outcome.CriteriaVersion != 0 || receipt.Outcome.RequestHash == "" {
		t.Fatalf("planning receipt: %+v", receipt.Outcome)
	}
	if _, err := s.GetAdaptiveTask(ctx, "task-plan-1"); err != nil {
		t.Fatalf("planned task missing: %v", err)
	}
	replay, created, err := s.ApplyOrchestratorPlan(ctx, submission)
	if err != nil || created || replay.Outcome.ReceiptID != receipt.Outcome.ReceiptID {
		t.Fatalf("planning replay is not idempotent: %+v %v", replay, err)
	}
	changed := submission
	changed.Action.Reason = "Different planning intent"
	if _, _, err := s.ApplyOrchestratorPlan(ctx, changed); !errors.Is(err, ports.ErrOrchestratorPlanConflict) {
		t.Fatalf("changed payload under the same key accepted: %v", err)
	}
	if _, err := s.GetAdaptiveTask(ctx, "task-plan-1"); err != nil {
		t.Fatalf("conflict mutated planning: %v", err)
	}
	revise := planSubmission(orch.ID, "plan-2", domain.OrchestratorPlanAction{Action: "revise_task", TaskID: "task-plan-1", ExpectedRevision: 1, Definition: &definition, Reason: "Refine the follow-up scope"})
	receipt, created, err = s.ApplyOrchestratorPlan(ctx, revise)
	if err != nil || !created || receipt.Outcome.Revision != 2 {
		t.Fatalf("planning revision: %+v %v", receipt, err)
	}
	criteria := taskCriteria()
	freeze := planSubmission(orch.ID, "plan-3", domain.OrchestratorPlanAction{Action: "freeze_criteria", TaskID: "task-plan-1", ExpectedRevision: 2, Criteria: &criteria, Reason: "Freeze acceptance before dispatch"})
	receipt, created, err = s.ApplyOrchestratorPlan(ctx, freeze)
	if err != nil || !created || receipt.Outcome.CriteriaVersion != 1 {
		t.Fatalf("criteria freeze: %+v %v", receipt, err)
	}
	if _, _, err := s.ApplyOrchestratorPlan(ctx, planSubmission(orch.ID, "plan-4", domain.OrchestratorPlanAction{Action: "revise_task", TaskID: "task-plan-1", ExpectedRevision: 1, Definition: &definition, Reason: "Stale planning"})); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("stale planning revision accepted: %v", err)
	}
	history, err := s.ListOrchestratorPlanReceipts(ctx, "project", "", 100)
	if err != nil || len(history) != 3 {
		t.Fatalf("planning history: %d %v", len(history), err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if history, err := reopened.ListOrchestratorPlanReceipts(ctx, "project", "", 100); err != nil || len(history) != 3 {
		t.Fatalf("planning history after restart: %d %v", len(history), err)
	}
}

func TestOrchestratorPlanRequiresLiveProjectOrchestrator(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpenAt(t, t.TempDir())
	seedProject(t, s, "project")
	definition := taskDefinition()
	submission := planSubmission("session-project-1", "plan-orphan", domain.OrchestratorPlanAction{Action: "create_task", Definition: &definition, Reason: "No orchestrator session exists"})
	if _, _, err := s.ApplyOrchestratorPlan(ctx, submission); !errors.Is(err, ports.ErrTaskForbidden) {
		t.Fatalf("plan action without an orchestrator session accepted: %v", err)
	}
	orch := orchestratorSession(t, s, "project")
	orch.IsTerminated = true
	if err := s.UpdateSession(ctx, orch); err != nil {
		t.Fatal(err)
	}
	dead := planSubmission(orch.ID, "plan-dead", domain.OrchestratorPlanAction{Action: "create_task", Definition: &definition, Reason: "Terminated orchestrator"})
	dead.NewTaskID = "task-dead"
	if _, _, err := s.ApplyOrchestratorPlan(ctx, dead); !errors.Is(err, ports.ErrTaskForbidden) {
		t.Fatalf("terminated orchestrator planned work: %v", err)
	}
}
