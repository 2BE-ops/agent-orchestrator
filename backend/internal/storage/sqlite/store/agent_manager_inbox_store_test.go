package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func managerInboxFixture(t *testing.T, s *sqlite.Store) domain.AgentManagerEnqueue {
	t.Helper()
	managerControllerFixture(t, s)
	createTask(t, s, "routing-task", taskDefinition())
	return domain.AgentManagerEnqueue{ID: "routing-request", ProjectID: "project", TaskID: "routing-task", TaskRevision: 1, ConfigurationVersion: 1, Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Route this exact task", Now: time.Now().UTC()}
}

func TestAgentManagerInboxRacesBoundsAndTerminalHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	input := managerInboxFixture(t, s)
	configuration, err := s.GetAgentManager(ctx, "project")
	mustNoError(t, err)
	configuration.Definition.Policy.MaxPendingRequests = 1
	_, err = s.ConfigureAgentManager(ctx, "project", configuration.Definition, taskMutation(1))
	mustNoError(t, err)
	input.ConfigurationVersion, input.Now = 2, time.Now().UTC()
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := range 8 {
		createTask(t, s, fmt.Sprintf("queue-task-%d", i), taskDefinition())
	}
	for i := range 8 {
		wg.Go(func() {
			r := input
			r.ID, r.TaskID, r.Now = fmt.Sprintf("queue-%d", i), fmt.Sprintf("queue-task-%d", i), time.Now().UTC()
			_, _, err := s.EnqueueAgentManagerRequest(ctx, r)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ports.ErrAgentManagerConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("queue limit admitted %d competing tasks", winners)
	}
	items, err := s.ListAgentManagerRequests(ctx, "project", 0, 10, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("pending: %+v %v", items, err)
	}
	winner := items[0]
	resolution := domain.AgentManagerRequestResolution{RequestID: winner.ID, Outcome: "needs_human", Actor: input.Actor, Reason: "No suitable permitted configuration", CreatedAt: time.Now().UTC()}
	mustNoError(t, s.ResolveAgentManagerRequest(ctx, "project", resolution))
	input.Now = time.Now().UTC()
	second, created, err := s.EnqueueAgentManagerRequest(ctx, input)
	if err != nil || !created || second.Sequence <= winner.Sequence {
		t.Fatalf("released inbox capacity: %+v %v %v", second, created, err)
	}
	items, err = s.ListAgentManagerRequests(ctx, "project", 0, 10, true)
	if err != nil || len(items) != 1 || items[0].ID != second.ID {
		t.Fatalf("terminal request still pending: %+v %v", items, err)
	}
	page, err := s.ListAgentManagerRequests(ctx, "project", 0, 1, false)
	if err != nil || len(page) != 1 || page[0].ID != winner.ID {
		t.Fatalf("history page: %+v %v", page, err)
	}
	page, err = s.ListAgentManagerRequests(ctx, "project", page[0].Sequence, 1, false)
	if err != nil || len(page) != 1 || page[0].ID != second.ID {
		t.Fatalf("history cursor: %+v %v", page, err)
	}
	resolution.CreatedAt = resolution.CreatedAt.Add(time.Hour)
	mustNoError(t, s.ResolveAgentManagerRequest(ctx, "project", resolution))
	resolution.Reason = "Replace retained outcome"
	if err := s.ResolveAgentManagerRequest(ctx, "project", resolution); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("rewrote terminal outcome: %v", err)
	}
	for _, page := range []struct {
		after int64
		limit int
	}{{-1, 10}, {0, 0}, {0, 101}} {
		if _, err := s.ListAgentManagerRequests(ctx, "project", page.after, page.limit, true); !errors.Is(err, ports.ErrAgentManagerInvalid) {
			t.Fatalf("invalid page accepted: %v", err)
		}
	}
}

func TestAgentManagerInboxExactReplayPinsAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input := managerInboxFixture(t, s)
	var wg sync.WaitGroup
	createdCount := make(chan bool, 8)
	errorsFound := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, created, err := s.EnqueueAgentManagerRequest(ctx, input)
			createdCount <- created
			errorsFound <- err
		})
	}
	wg.Wait()
	close(createdCount)
	close(errorsFound)
	count := 0
	for created := range createdCount {
		if created {
			count++
		}
	}
	for err := range errorsFound {
		mustNoError(t, err)
	}
	if count != 1 {
		t.Fatalf("exact enqueue created %d requests", count)
	}
	first, err := s.GetAgentManagerRequest(ctx, "project", input.ID)
	mustNoError(t, err)
	if first.TaskRevision != 1 || first.CriteriaVersion != 1 || first.ConfigurationVersion != 1 || first.ContentHash == "" {
		t.Fatalf("unpinned work: %+v", first)
	}
	changed := taskDefinition()
	changed.Brief = "Later brief must not rewrite routing evidence"
	_, err = s.ReviseAdaptiveTask(ctx, input.TaskID, changed, taskMutation(1))
	mustNoError(t, err)
	configuration, err := s.GetAgentManager(ctx, "project")
	mustNoError(t, err)
	configuration.Definition.Enabled = false
	_, err = s.ConfigureAgentManager(ctx, "project", configuration.Definition, taskMutation(1))
	mustNoError(t, err)
	mustNoError(t, s.Close())
	s, err = sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	input.Now = time.Now().UTC()
	replay, created, err := s.EnqueueAgentManagerRequest(ctx, input)
	if err != nil || created || replay.ContentHash != first.ContentHash || !replay.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("replay drifted after restart: %+v %v %v", replay, created, err)
	}
	bad := input
	bad.TaskRevision = 2
	if _, _, err := s.EnqueueAgentManagerRequest(ctx, bad); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("changed retry: %v", err)
	}
	resolution := domain.AgentManagerRequestResolution{RequestID: input.ID, Outcome: "superseded", Actor: input.Actor, Reason: "Task planning changed", CreatedAt: time.Now().UTC()}
	mustNoError(t, s.ResolveAgentManagerRequest(ctx, "project", resolution))
	got, found, err := s.GetAgentManagerRequestResolution(ctx, "project", input.ID)
	if err != nil || !found || got.Outcome != "superseded" {
		t.Fatalf("retained resolution: %+v %v %v", got, found, err)
	}
	if _, _, err := s.EnqueueAgentManagerRequest(ctx, input); err != nil {
		t.Fatalf("terminal exact replay: %v", err)
	}
	audit, err := s.ListAgentManagerAudit(ctx, "project", 0, 100)
	mustNoError(t, err)
	enqueues, resolutions := 0, 0
	for _, event := range audit {
		if event.Action == "request_enqueued" {
			enqueues++
		}
		if event.Action == "request_superseded" {
			resolutions++
		}
	}
	if enqueues != 1 || resolutions != 1 {
		t.Fatalf("duplicate audit: %+v", audit)
	}
}

func TestAgentManagerInboxRejectsInvalidScopeIntentAndAuthority(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	input := managerInboxFixture(t, s)
	seedProject(t, s, "other")
	for _, mutate := range []func(*domain.AgentManagerEnqueue){
		func(r *domain.AgentManagerEnqueue) { r.ConfigurationVersion = 2 },
		func(r *domain.AgentManagerEnqueue) { r.TaskRevision = 2 },
		func(r *domain.AgentManagerEnqueue) { r.Now = time.Unix(1, 0) },
		func(r *domain.AgentManagerEnqueue) {
			r.Actor = domain.AdaptiveActor{Kind: "AGENT_MANAGER", ID: "manager", SessionID: "manager"}
		},
		func(r *domain.AgentManagerEnqueue) {
			r.Actor = domain.AdaptiveActor{Kind: "WORKER", ID: "worker", SessionID: "worker"}
		},
		func(r *domain.AgentManagerEnqueue) {
			r.Actor = domain.AdaptiveActor{Kind: "ORCHESTRATOR", ID: "missing", SessionID: "missing"}
		},
	} {
		bad := input
		mutate(&bad)
		if _, _, err := s.EnqueueAgentManagerRequest(ctx, bad); err == nil {
			t.Fatalf("accepted invalid request: %+v", bad)
		}
	}
	criteria := taskCriteria()
	_, err := s.CreateAdaptiveTask(ctx, "other-task", "other", taskDefinition(), &criteria, taskMutation(0))
	mustNoError(t, err)
	bad := input
	bad.TaskID, bad.Now = "other-task", time.Now().UTC()
	if _, _, err := s.EnqueueAgentManagerRequest(ctx, bad); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("cross-project task: %v", err)
	}
	_, err = s.CreateAdaptiveTask(ctx, "no-criteria", "project", taskDefinition(), nil, taskMutation(0))
	mustNoError(t, err)
	bad.TaskID, bad.Now = "no-criteria", time.Now().UTC()
	if _, _, err := s.EnqueueAgentManagerRequest(ctx, bad); !errors.Is(err, ports.ErrAgentManagerInvalid) {
		t.Fatalf("unfrozen criteria: %v", err)
	}
	request, _, err := s.EnqueueAgentManagerRequest(ctx, input)
	mustNoError(t, err)
	if _, err := s.GetAgentManagerRequest(ctx, "other", request.ID); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("cross-project read: %v", err)
	}
	resolution := domain.AgentManagerRequestResolution{RequestID: request.ID, Outcome: "cancelled", Actor: input.Actor, Reason: "Cancel routing", CreatedAt: time.Now().UTC()}
	if err := s.ResolveAgentManagerRequest(ctx, "other", resolution); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("cross-project resolution: %v", err)
	}
	if _, _, err := s.GetAgentManagerRequestResolution(ctx, "other", request.ID); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("cross-project resolution read: %v", err)
	}
	mustNoError(t, s.ResolveAgentManagerRequest(ctx, "project", resolution))
	_, err = s.ChangeTaskIntent(ctx, input.TaskID, domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)})
	mustNoError(t, err)
	input.ID, input.Now = "after-cancel", time.Now().UTC()
	if _, _, err := s.EnqueueAgentManagerRequest(ctx, input); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("cancelled work routed: %v", err)
	}
}

func TestAgentManagerInboxAuditRollbackAndSQLHistoryGuards(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input := managerInboxFixture(t, s)
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TRIGGER fail_inbox_audit BEFORE INSERT ON adaptive_agent_manager_audit WHEN NEW.action LIKE 'request_%' BEGIN SELECT RAISE(ABORT,'injected inbox audit failure'); END`)
	mustNoError(t, err)
	var before, after int
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before))
	if _, _, err := s.EnqueueAgentManagerRequest(ctx, input); err == nil {
		t.Fatal("enqueue survived audit failure")
	}
	items, err := s.ListAgentManagerRequests(ctx, "project", 0, 10, false)
	if err != nil || len(items) != 0 {
		t.Fatalf("partial enqueue: %+v %v", items, err)
	}
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after))
	if after != before {
		t.Fatal("rolled-back enqueue leaked CDC")
	}
	_, err = db.Exec(`DROP TRIGGER fail_inbox_audit`)
	mustNoError(t, err)
	_, _, err = s.EnqueueAgentManagerRequest(ctx, input)
	mustNoError(t, err)
	_, err = db.Exec(`CREATE TRIGGER fail_inbox_audit BEFORE INSERT ON adaptive_agent_manager_audit WHEN NEW.action LIKE 'request_%' BEGIN SELECT RAISE(ABORT,'injected inbox audit failure'); END`)
	mustNoError(t, err)
	resolution := domain.AgentManagerRequestResolution{RequestID: input.ID, Outcome: "cancelled", Actor: input.Actor, Reason: "Cancel routing", CreatedAt: time.Now().UTC()}
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before))
	if err := s.ResolveAgentManagerRequest(ctx, "project", resolution); err == nil {
		t.Fatal("resolution survived audit failure")
	}
	if _, found, err := s.GetAgentManagerRequestResolution(ctx, "project", input.ID); err != nil || found {
		t.Fatalf("partial resolution: %v %v", found, err)
	}
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after))
	if after != before {
		t.Fatal("rolled-back resolution leaked CDC")
	}
	_, err = db.Exec(`DROP TRIGGER fail_inbox_audit`)
	mustNoError(t, err)
	mustNoError(t, s.ResolveAgentManagerRequest(ctx, "project", resolution))
	for _, statement := range []string{`UPDATE adaptive_agent_manager_requests SET content_hash=printf('%064d',0)`, `DELETE FROM adaptive_agent_manager_requests`, `UPDATE adaptive_agent_manager_request_resolutions SET outcome='needs_human'`, `DELETE FROM adaptive_agent_manager_request_resolutions`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("changed retained history: %s", statement)
		}
	}
	// A different retry ID for the same task cannot add a second pending entry.
	input.ID, input.Now = "pending-again", time.Now().UTC()
	_, _, err = s.EnqueueAgentManagerRequest(ctx, input)
	mustNoError(t, err)
	input.ID = "duplicate-pending"
	if _, _, err := s.EnqueueAgentManagerRequest(ctx, input); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("duplicate pending task: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO adaptive_agent_manager_requests(id,project_id,task_id,task_revision,criteria_version,configuration_version,snapshot,content_hash,created_at) SELECT 'raw-duplicate',project_id,task_id,task_revision,criteria_version,configuration_version,snapshot,content_hash,created_at FROM adaptive_agent_manager_requests WHERE id='pending-again'`); err == nil {
		t.Fatal("SQL bypassed pending exclusivity")
	}
}
