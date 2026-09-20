package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func dryRunDefinition(title string) domain.TaskDefinition {
	return domain.TaskDefinition{Title: title, Brief: "Rehearsed work", Category: "chore", MaxAttempts: 3}
}

// dryRunSnapshot captures every durable surface a dry run must leave
// untouched, read through the store's own read APIs: tasks, revisions,
// intents, audit, registry entries and versions, sessions, attempts, CDC
// sequence and pending needs human requests.
func dryRunSnapshot(t *testing.T, s *sqlite.Store) []any {
	t.Helper()
	ctx := context.Background()
	tasks, err := s.ListAdaptiveTasks(ctx, "project", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var revisions []domain.TaskRevision
	var intents []domain.TaskIntent
	var audit []domain.TaskAudit
	var attempts []domain.TaskAttempt
	for _, task := range tasks {
		item, err := s.ListTaskRevisions(ctx, task.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		revisions = append(revisions, item...)
		intentList, err := s.ListTaskIntents(ctx, task.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		intents = append(intents, intentList...)
		auditList, err := s.ListTaskAudit(ctx, task.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		audit = append(audit, auditList...)
		attemptList, err := s.ListTaskAttempts(ctx, task.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		attempts = append(attempts, attemptList...)
	}
	agentTypes, err := s.ListRegistryEntries(ctx, domain.RegistryAgentType, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	var versions int
	for _, entry := range agentTypes {
		list, err := s.ListRegistryVersions(ctx, entry.ID, 0, 200)
		if err != nil {
			t.Fatal(err)
		}
		versions += len(list)
	}
	skills, err := s.ListRegistryEntries(ctx, domain.RegistrySkill, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := s.ListAllSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seq, err := s.LatestSeq(ctx)
	if err != nil {
		t.Fatal(err)
	}
	needsHuman, err := s.ListProjectNeedsHuman(ctx, "project", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	return []any{tasks, revisions, intents, audit, agentTypes, skills, versions, sessions, attempts, seq, needsHuman}
}

// TestDryRunPlanMutatesNothing proves the read-only contract across every
// surface the plan's execution path would write.
func TestDryRunPlanMutatesNothing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "existing", taskDefinition())
	if _, err := s.CreateRegistryEntry(ctx, "dry-run-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Bounded", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, Instructions: "Do bounded work", MaxParallelWorkers: 2}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Configure"}); err != nil {
		t.Fatal(err)
	}
	request := domain.DryRunRequest{Actions: []domain.OrchestratorPlanAction{
		{Action: "create_task", Definition: func() *domain.TaskDefinition { d := dryRunDefinition("New work"); return &d }(), Reason: "Add work"},
		{Action: "revise_task", TaskID: "existing", ExpectedRevision: 1, Definition: func() *domain.TaskDefinition { d := taskDefinition(); d.Title = "Narrower"; return &d }(), Reason: "Narrow"},
		{Action: "freeze_criteria", TaskID: "existing", ExpectedRevision: 2, Criteria: &domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "tests", Requirement: "Tests pass", EvidenceKind: "test"}}}, Reason: "Freeze"},
	}}
	before := dryRunSnapshot(t, s)
	verdict, err := s.DryRunPlan(ctx, "project", request)
	if err != nil {
		t.Fatal(err)
	}
	after := dryRunSnapshot(t, s)
	for i := range before {
		if fmt.Sprintf("%v", before[i]) != fmt.Sprintf("%v", after[i]) {
			t.Fatalf("dry run mutated durable surface %d: %v -> %v", i, before[i], after[i])
		}
	}
	if !verdict.GraphValid {
		t.Fatalf("clean plan not valid: %+v", verdict.Actions)
	}
	for _, action := range verdict.Actions {
		if !action.WouldApply {
			t.Fatalf("clean action not applicable: %+v", action)
		}
	}
	// Replays see the same world: nothing the first rehearsal did changed it.
	replay, err := s.DryRunPlan(ctx, "project", request)
	if err != nil || !replay.GraphValid || len(replay.Actions) != 3 {
		t.Fatalf("replay drifted: %+v %v", replay, err)
	}
}

func TestDryRunPlanReportsBlockersHonestly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "existing", taskDefinition())
	createTask(t, s, "dependency", taskDefinition())
	missing := dryRunDefinition("Missing task")
	missing.RequestedWorker = &domain.WorkerSelection{AgentTypeID: "ghost-type"}
	stale := dryRunDefinition("Stale fence")
	request := domain.DryRunRequest{Actions: []domain.OrchestratorPlanAction{
		{Action: "revise_task", TaskID: "ghost", ExpectedRevision: 1, Definition: &missing, Reason: "Unknown target"},
		{Action: "revise_task", TaskID: "existing", ExpectedRevision: 7, Definition: &stale, Reason: "Stale fence"},
		{Action: "create_task", Definition: &missing, Reason: "Selection fails"},
	}}
	verdict, err := s.DryRunPlan(ctx, "project", request)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Actions[0].WouldApply || verdict.Actions[0].Findings[0] != "task does not exist in this project" {
		t.Fatalf("unknown task not reported: %+v", verdict.Actions[0])
	}
	if verdict.Actions[1].WouldApply || verdict.Actions[1].Findings[0] != "expected revision 7 but the task is at revision 1" {
		t.Fatalf("stale revision not reported: %+v", verdict.Actions[1])
	}
	if verdict.Actions[2].WouldApply {
		t.Fatalf("unresolvable selection still applicable: %+v", verdict.Actions[2])
	}
	if len(verdict.Selections) != 2 {
		t.Fatalf("selections not collected: %+v", verdict.Selections)
	}
	for _, selection := range verdict.Selections {
		if selection.Resolves || selection.AgentTypeID != "ghost-type" {
			t.Fatalf("ghost selection resolved: %+v", selection)
		}
	}
	// The per-action blockers are the honest output; the overlay itself is
	// valid, so the graph is not blamed for a selection failure.
	if !verdict.GraphValid {
		t.Fatalf("overlay wrongly invalid: %+v", verdict.Actions)
	}
}

func TestDryRunPlanRefusesCyclicOverlay(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "left", taskDefinition())
	createTask(t, s, "right", taskDefinition())
	// Sequential execution would refuse the second rewrite; the overlay
	// rehearses the combined effect and refuses the whole plan up front.
	mutualA := taskDefinition()
	mutualA.ParentID = "right"
	mutualB := taskDefinition()
	mutualB.ParentID = "left"
	verdict, err := s.DryRunPlan(ctx, "project", domain.DryRunRequest{Actions: []domain.OrchestratorPlanAction{
		{Action: "revise_task", TaskID: "left", ExpectedRevision: 1, Definition: &mutualA, Reason: "Point at right"},
		{Action: "revise_task", TaskID: "right", ExpectedRevision: 1, Definition: &mutualB, Reason: "Point at left"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.GraphValid {
		t.Fatalf("cyclic overlay accepted: %+v", verdict.Actions)
	}
	for _, action := range verdict.Actions {
		if action.WouldApply || len(action.Findings) == 0 {
			t.Fatalf("cyclic action applicable: %+v", action)
		}
	}
}

func TestDryRunPlanReportsControlAndCapacity(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	request := domain.DryRunRequest{Actions: []domain.OrchestratorPlanAction{
		{Action: "freeze_criteria", TaskID: "work", ExpectedRevision: 1, Criteria: &domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "tests", Requirement: "Tests pass", EvidenceKind: "test"}}}, Reason: "Freeze"},
	}}
	controlChange(t, s, "project", domain.ProjectPaused, "Rehearse under pause")
	verdict, err := s.DryRunPlan(ctx, "project", request)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Admission.ControlState != "paused" || verdict.Admission.WouldAdmitDispatch {
		t.Fatalf("paused admission facts wrong: %+v", verdict.Admission)
	}
	if !verdict.Actions[0].WouldApply {
		t.Fatalf("planning action wrongly blocked by control state: %+v", verdict.Actions[0])
	}
	// Unknown projects and malformed requests refuse before any simulation.
	if _, err := s.DryRunPlan(ctx, "ghost", request); !errors.Is(err, ports.ErrProjectControlNotFound) {
		t.Fatalf("unknown project accepted: %v", err)
	}
	if _, err := s.DryRunPlan(ctx, "project", domain.DryRunRequest{}); !errors.Is(err, ports.ErrDryRunInvalid) {
		t.Fatalf("empty request accepted: %v", err)
	}
}
