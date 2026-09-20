package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func taskDefinition() domain.TaskDefinition {
	return domain.TaskDefinition{Title: "Implement bounded work", Brief: "Follow the fixed acceptance criteria", MaxAttempts: 3}
}

func taskCriteria() domain.AcceptanceCriteria {
	return domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "tests", Requirement: "Regression tests pass", EvidenceKind: "test", Command: []string{"go", "test", "./..."}}}}
}

func taskMutation(revision int64) domain.TaskMutation {
	return domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Plan bounded work", ExpectedRevision: revision}
}

func createTask(t *testing.T, s *sqlite.Store, id string, definition domain.TaskDefinition) {
	t.Helper()
	criteria := taskCriteria()
	if _, err := s.CreateAdaptiveTask(context.Background(), id, "project", definition, &criteria, taskMutation(0)); err != nil {
		t.Fatal(err)
	}
}

func TestTaskImmutablePlanningAndCriteriaHistory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	createTask(t, s, "first", taskDefinition())
	definition := taskDefinition()
	definition.Title = "Refined intent"
	definition.RequestedWorker = &domain.WorkerSelection{AgentTypeID: "future-type", Version: 2}
	second, err := s.ReviseAdaptiveTask(ctx, "first", definition, taskMutation(1))
	if err != nil || second.Number != 2 || second.CriteriaVersion != 1 {
		t.Fatalf("second revision: %+v %v", second, err)
	}
	criteria := taskCriteria()
	criteria.Criteria[0].Requirement = "Tests and the added regression pass"
	third, err := s.ReviseAcceptanceCriteria(ctx, "first", criteria, taskMutation(2))
	if err != nil || third.Number != 3 || third.CriteriaVersion != 2 || third.Definition.Title != definition.Title {
		t.Fatalf("criteria revision: %+v %v", third, err)
	}
	if _, err := s.ReviseAcceptanceCriteria(ctx, "first", criteria, taskMutation(2)); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("stale change accepted: %v", err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	task, err := reopened.GetAdaptiveTask(ctx, "first")
	if err != nil || task.Revision != 3 || task.CreatedBy.ID != "human" {
		t.Fatalf("identity: %+v %v", task, err)
	}
	old, err := reopened.GetTaskRevision(ctx, "first", 1)
	if err != nil || old.Definition.Title != taskDefinition().Title || old.CriteriaVersion != 1 || old.ContentHash == third.ContentHash {
		t.Fatalf("old revision: %+v %v", old, err)
	}
	oldCriteria, err := reopened.GetAcceptanceCriteria(ctx, "first", 1)
	if err != nil || oldCriteria.Definition.Criteria[0].Requirement != taskCriteria().Criteria[0].Requirement {
		t.Fatalf("old criteria: %+v %v", oldCriteria, err)
	}
	newCriteria, err := reopened.GetAcceptanceCriteria(ctx, "first", 2)
	if err != nil || newCriteria.PreviousVersion != 1 || newCriteria.ContentHash == oldCriteria.ContentHash {
		t.Fatalf("new criteria: %+v %v", newCriteria, err)
	}
	revisions, err := reopened.ListTaskRevisions(ctx, "first", 1, 1)
	if err != nil || len(revisions) != 1 || revisions[0].Number != 2 {
		t.Fatalf("revision pagination: %+v %v", revisions, err)
	}
	audit, err := reopened.ListTaskAudit(ctx, "first", 0, 2)
	if err != nil || len(audit) != 2 || audit[0].Action != "created" || audit[1].Action != "revised" {
		t.Fatalf("audit: %+v %v", audit, err)
	}
	audit, err = reopened.ListTaskAudit(ctx, "first", audit[1].Sequence, 2)
	if err != nil || len(audit) != 1 || audit[0].Action != "criteria_revised" {
		t.Fatalf("audit pagination: %+v %v", audit, err)
	}
}

func TestTaskAuthorityAndProjectScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	seedProject(t, s, "other")
	createTask(t, s, "first", taskDefinition())
	for _, kind := range []string{"WORKER", "AGENT_MANAGER", "unknown"} {
		m := taskMutation(1)
		m.Actor.Kind = kind
		if _, err := s.ReviseAcceptanceCriteria(ctx, "first", taskCriteria(), m); !errors.Is(err, ports.ErrTaskForbidden) {
			t.Fatalf("%s rewrote success: %v", kind, err)
		}
		m.ExpectedRevision = 0
		if _, err := s.CreateAdaptiveTask(ctx, "denied", "project", taskDefinition(), nil, m); !errors.Is(err, ports.ErrTaskForbidden) {
			t.Fatalf("%s created work: %v", kind, err)
		}
	}
	for _, tc := range []struct {
		name, project string
		kind          domain.SessionKind
		terminated    bool
		allowed       bool
	}{
		{"worker", "project", domain.KindWorker, false, false},
		{"other orchestrator", "other", domain.KindOrchestrator, false, false},
		{"terminated orchestrator", "project", domain.KindOrchestrator, true, false},
		{"current orchestrator", "project", domain.KindOrchestrator, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := sampleRecord(tc.project)
			rec.Kind, rec.IsTerminated = tc.kind, tc.terminated
			session, err := s.CreateSession(ctx, rec)
			if err != nil {
				t.Fatal(err)
			}
			m := taskMutation(1)
			m.Actor = domain.AdaptiveActor{Kind: "ORCHESTRATOR", ID: string(session.ID), SessionID: session.ID}
			_, err = s.ReviseAdaptiveTask(ctx, "first", taskDefinition(), m)
			if tc.allowed && err != nil {
				t.Fatal(err)
			}
			if !tc.allowed && !errors.Is(err, ports.ErrTaskForbidden) {
				t.Fatalf("unexpected authorization: %v", err)
			}
		})
	}
	missing := taskMutation(2)
	missing.Actor = domain.AdaptiveActor{Kind: "ORCHESTRATOR", ID: "missing", SessionID: "missing"}
	if _, err := s.ReviseAdaptiveTask(ctx, "first", taskDefinition(), missing); !errors.Is(err, ports.ErrTaskForbidden) {
		t.Fatalf("missing controller accepted: %v", err)
	}
}

func TestTaskGraphRejectsCyclesAndCrossProjectReferences(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	seedProject(t, s, "other")
	createTask(t, s, "a", taskDefinition())
	definition := taskDefinition()
	definition.Dependencies = []string{"a"}
	createTask(t, s, "b", definition)
	definition.Dependencies = []string{"b"}
	createTask(t, s, "c", definition)
	if _, err := s.CreateAdaptiveTask(ctx, "foreign", "other", taskDefinition(), nil, taskMutation(0)); err != nil {
		t.Fatal(err)
	}
	for _, dependency := range []string{"a", "c", "foreign", "missing"} {
		definition.Dependencies = []string{dependency}
		if _, err := s.ReviseAdaptiveTask(ctx, "a", definition, taskMutation(1)); !errors.Is(err, ports.ErrTaskInvalid) {
			t.Fatalf("invalid dependency %s: %v", dependency, err)
		}
	}
	definition.Dependencies = nil
	for _, parent := range []string{"a", "foreign", "missing"} {
		definition.ParentID = parent
		if _, err := s.ReviseAdaptiveTask(ctx, "a", definition, taskMutation(1)); !errors.Is(err, ports.ErrTaskInvalid) {
			t.Fatalf("invalid parent %s: %v", parent, err)
		}
	}
	definition.ParentID = "a"
	if _, err := s.ReviseAdaptiveTask(ctx, "b", definition, taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	definition.ParentID = "b"
	if _, err := s.ReviseAdaptiveTask(ctx, "a", definition, taskMutation(1)); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("parent cycle: %v", err)
	}
	// Parent hierarchy and dependency edges are distinct: an epic may depend on its child.
	definition.ParentID, definition.Dependencies = "", []string{"b"}
	if _, err := s.ReviseAdaptiveTask(ctx, "a", definition, taskMutation(1)); err != nil {
		t.Fatalf("epic waits for child: %v", err)
	}
}

func TestTaskHierarchyDepthIncludesReparentedDescendants(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	definition := taskDefinition()
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("level-%d", i)
		createTask(t, s, id, definition)
		definition.ParentID = id
	}
	if _, err := s.CreateAdaptiveTask(ctx, "too-deep", "project", definition, nil, taskMutation(0)); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("unbounded hierarchy: %v", err)
	}
	createTask(t, s, "root", taskDefinition())
	definition.ParentID = "root"
	if _, err := s.ReviseAdaptiveTask(ctx, "level-0", definition, taskMutation(1)); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("reparent deepened descendant beyond bound: %v", err)
	}
}

func TestTaskConcurrentEdgesCannotCommitCycle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "a", taskDefinition())
	createTask(t, s, "b", taskDefinition())
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for id, dependency := range map[string]string{"a": "b", "b": "a"} {
		wg.Go(func() {
			d := taskDefinition()
			d.Dependencies = []string{dependency}
			_, err := s.ReviseAdaptiveTask(ctx, id, d, taskMutation(1))
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	success, invalid := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ports.ErrTaskInvalid):
			invalid++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || invalid != 1 {
		t.Fatalf("concurrent cycle results: success=%d invalid=%d", success, invalid)
	}
}

func TestTaskAuditFailureRollsBackIdentityGraphAndCDC(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	createTask(t, s, "dependency", taskDefinition())
	createTask(t, s, "first", taskDefinition())
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, table := range []string{"adaptive_task_revisions", "adaptive_task_criteria", "adaptive_task_audit"} {
		for _, statement := range []string{"UPDATE " + table + " SET task_id=task_id", "DELETE FROM " + table} {
			if _, err := db.ExecContext(ctx, statement); err == nil {
				t.Fatalf("mutable task history: %s", statement)
			}
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_task_audit BEFORE INSERT ON adaptive_task_audit BEGIN SELECT RAISE(ABORT,'injected audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	before, err := s.EventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	definition := taskDefinition()
	definition.Dependencies = []string{"dependency"}
	criteria := taskCriteria()
	if _, err := s.CreateAdaptiveTask(ctx, "orphan", "project", definition, &criteria, taskMutation(0)); err == nil {
		t.Fatal("injected failure accepted")
	}
	if _, err := s.GetAdaptiveTask(ctx, "orphan"); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("orphan identity: %v", err)
	}
	if _, err := s.ReviseAdaptiveTask(ctx, "first", definition, taskMutation(1)); err == nil {
		t.Fatal("revision failure accepted")
	}
	if _, err := s.ReviseAcceptanceCriteria(ctx, "first", criteria, taskMutation(1)); err == nil {
		t.Fatal("criteria failure accepted")
	}
	if _, err := s.GetTaskRevision(ctx, "first", 2); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("orphan revision: %v", err)
	}
	if _, err := s.GetAcceptanceCriteria(ctx, "first", 2); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("orphan criteria: %v", err)
	}
	task, err := s.GetAdaptiveTask(ctx, "first")
	if err != nil || task.Revision != 1 {
		t.Fatalf("pointer changed: %+v %v", task, err)
	}
	var edgeCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM adaptive_task_dependencies`).Scan(&edgeCount); err != nil || edgeCount != 0 {
		t.Fatalf("orphan edges: %d %v", edgeCount, err)
	}
	after, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(before) != len(after) {
		t.Fatalf("rolled-back CDC: %d -> %d %v", len(before), len(after), err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_task_audit`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReviseAdaptiveTask(ctx, "first", definition, taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	// Once existing CDC retention is cleared, project deletion cascades task history.
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	events, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(events) != 3 || events[2].Type != cdc.EventAdaptiveTaskChanged || events[2].ProjectID != "project" {
		t.Fatalf("task CDC: %+v %v", events, err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM change_log WHERE project_id='project'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM projects WHERE id='project'`); err != nil {
		t.Fatalf("project deletion failed: %v", err)
	}
	if _, err := s.GetAdaptiveTask(ctx, "first"); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("project task retained: %v", err)
	}
}

func TestTaskPlannedWithoutCriteriaAndBoundedPagination(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	if _, err := s.CreateAdaptiveTask(ctx, "planned", "project", taskDefinition(), nil, taskMutation(0)); err != nil {
		t.Fatal(err)
	}
	revision, err := s.GetTaskRevision(ctx, "planned", 1)
	if err != nil || revision.CriteriaVersion != 0 {
		t.Fatalf("unfrozen work: %+v %v", revision, err)
	}
	if _, err := s.ReviseAcceptanceCriteria(ctx, "planned", taskCriteria(), taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	createTask(t, s, "second", taskDefinition())
	page, err := s.ListAdaptiveTasks(ctx, "project", "", 1)
	if err != nil || len(page) != 1 || page[0].ID != "planned" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	page, err = s.ListAdaptiveTasks(ctx, "project", "planned", 1)
	if err != nil || len(page) != 1 || page[0].ID != "second" {
		t.Fatalf("second page: %+v %v", page, err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := s.ListAdaptiveTasks(ctx, "project", "", limit); !errors.Is(err, ports.ErrTaskInvalid) {
			t.Fatalf("unbounded task page: %v", err)
		}
		if _, err := s.ListTaskRevisions(ctx, "planned", 0, limit); !errors.Is(err, ports.ErrTaskInvalid) {
			t.Fatalf("unbounded revision page: %v", err)
		}
		if _, err := s.ListTaskAudit(ctx, "planned", 0, limit); !errors.Is(err, ports.ErrTaskInvalid) {
			t.Fatalf("unbounded audit page: %v", err)
		}
	}
}
