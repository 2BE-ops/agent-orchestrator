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

func taskMessageFixture(t *testing.T, s *sqlite.Store, startTarget bool) (domain.TaskMessageSubmission, domain.TaskMessageSubmission, domain.TaskLease) {
	t.Helper()
	ctx := context.Background()
	source, snapshot, _ := taskResultFixture(t, s, true)
	input := domain.TaskMessageSubmission{ID: "message", AttemptID: source.AttemptID, SessionID: source.SessionID, SourceOwner: source.SourceOwner, IdempotencyKey: "first", Definition: domain.TaskMessageDefinition{SchemaVersion: 1, Kind: "question", TargetTaskID: "target", Subject: "Shared interface", Body: "Which field is stable?", CorrelationID: "thread"}}
	createTask(t, s, "target", taskDefinition())
	if !startTarget {
		return input, domain.TaskMessageSubmission{}, domain.TaskLease{}
	}
	request := taskReservation("target-attempt", "target")
	request.Now, request.TTL = time.Now().UTC(), 5*time.Minute
	_, lease, err := s.ReserveTask(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := s.GetSession(ctx, source.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := s.GetWorkerConfiguration(ctx, source.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	rec.ID, rec.Metadata.RuntimeLaunchID = "", ""
	rec, _, err = s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, config, lease.HeartbeatAt)
	if err != nil {
		t.Fatal(err)
	}
	op := domain.TaskExecutionOperation{ID: "target-native", SessionID: rec.ID, Lease: lease.TaskLeaseToken, SourceOwner: rec.ControllerOwner(), Kind: "dispatch", CreatedAt: lease.HeartbeatAt}
	if _, err := s.BeginTaskExecution(ctx, op); err != nil {
		t.Fatal(err)
	}
	revision, err := s.GetTaskRevision(ctx, "target", 1)
	if err != nil {
		t.Fatal(err)
	}
	criteria, err := s.GetAcceptanceCriteria(ctx, "target", 1)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.AttemptID, snapshot.SessionID, snapshot.ExecutionOperationID, snapshot.CreatedAt = lease.AttemptID, rec.ID, op.ID, lease.HeartbeatAt
	snapshot.Task = domain.TaskRevisionRef{TaskID: "target", Revision: 1, ContentHash: revision.ContentHash}
	snapshot.Sources[0] = inlineContext(t, "task", "target", 1, revision.ContentHash, revision.Definition)
	snapshot.Sources[1] = inlineContext(t, "criteria", "target", 1, criteria.ContentHash, criteria.Definition)
	sealContext(t, &snapshot)
	if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); err != nil {
		t.Fatal(err)
	}
	rec.Metadata.RuntimeLaunchID = op.ID
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveTaskExecution(ctx, domain.TaskExecutionResolution{OperationID: op.ID, ObservedOwner: rec.ControllerOwner(), Outcome: "connected", Reason: "Fixture native worker connected", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	reply := domain.TaskMessageSubmission{ID: "answer", AttemptID: lease.AttemptID, SessionID: rec.ID, SourceOwner: rec.ControllerOwner(), IdempotencyKey: "answer", Definition: domain.TaskMessageDefinition{SchemaVersion: 1, Kind: "answer", TargetTaskID: "work", Subject: "Stable interface", Body: "Use the versioned id", CorrelationID: "thread", ReplyToID: input.ID}}
	return input, reply, lease
}

func TestTaskMessageThreadsPersistAndScopeWithoutChangingPlanning(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, reply, _ := taskMessageFixture(t, s, true)
	first, created, err := s.SubmitTaskMessage(ctx, input)
	if err != nil || !created || first.ConfigurationHash == "" || first.ContextHash == "" || first.CriteriaVersion != 1 {
		t.Fatalf("persisted attribution: %+v %v %v", first, created, err)
	}
	if _, created, err := s.SubmitTaskMessage(ctx, input); err != nil || created {
		t.Fatalf("duplicate submission: %v %v", created, err)
	}
	if _, _, err := s.SubmitTaskMessage(ctx, reply); err != nil {
		t.Fatalf("scoped answer: %v", err)
	}
	for _, filter := range []string{"", "work", "target"} {
		items, err := s.ListTaskMessages(ctx, "project", filter, 0, 100)
		if err != nil || len(items) != 2 || items[0].ID != first.ID || items[1].Definition.ReplyToID != first.ID {
			t.Fatalf("shared timeline %q: %+v %v", filter, items, err)
		}
	}
	page, err := s.ListTaskMessages(ctx, "project", "", first.Sequence, 1)
	if err != nil || len(page) != 1 || page[0].ID != reply.ID {
		t.Fatalf("cursor: %+v %v", page, err)
	}
	foreign, err := s.ListTaskMessages(ctx, "foreign", "", 0, 100)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign timeline: %+v %v", foreign, err)
	}
	conflict := input
	conflict.Definition.Body = "Rewritten"
	if _, _, err := s.SubmitTaskMessage(ctx, conflict); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("key conflict: %v", err)
	}
	rec, _, err := s.GetSession(ctx, input.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	rec.IsTerminated = true
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	retained, err := reopened.GetTaskMessage(ctx, first.ID)
	if err != nil || retained.ContentHash != first.ContentHash {
		t.Fatalf("restart: %+v %v", retained, err)
	}
	if _, created, err := reopened.SubmitTaskMessage(ctx, input); err != nil || created {
		t.Fatalf("historical acknowledgement: %v %v", created, err)
	}
	input.ID, input.IdempotencyKey = "late", "late"
	if _, _, err := reopened.SubmitTaskMessage(ctx, input); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("terminated source wrote: %v", err)
	}
	task, err := s.GetAdaptiveTask(ctx, "work")
	if err != nil || task.Revision != 1 {
		t.Fatalf("message changed planning: %+v %v", task, err)
	}
	if _, held, err := s.GetActiveTaskLease(ctx, "work"); err != nil || !held {
		t.Fatalf("message released ownership: %v %v", held, err)
	}
}

func TestTaskMessageRejectsForgedReferencesAndOwnership(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, reply, _ := taskMessageFixture(t, s, true)
	seedProject(t, s, "other")
	criteria := taskCriteria()
	if _, err := s.CreateAdaptiveTask(ctx, "foreign", "other", taskDefinition(), &criteria, taskMutation(0)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitTaskMessage(ctx, input); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*domain.TaskMessageSubmission)
	}{
		{"foreign", func(i *domain.TaskMessageSubmission) { i.Definition.TargetTaskID = "foreign" }},
		{"self", func(i *domain.TaskMessageSubmission) { i.Definition.TargetTaskID = "target" }},
		{"generation", func(i *domain.TaskMessageSubmission) { i.SourceOwner.RuntimeLaunchID = "stale" }},
		{"activation", func(i *domain.TaskMessageSubmission) { i.ExpectedActivation = 1 }},
		{"session", func(i *domain.TaskMessageSubmission) { i.SessionID = input.SessionID }},
		{"thread", func(i *domain.TaskMessageSubmission) { i.Definition.CorrelationID = "other-thread" }},
		{"missing_reply", func(i *domain.TaskMessageSubmission) { i.Definition.ReplyToID = "missing" }},
		{"missing_result", func(i *domain.TaskMessageSubmission) { i.Definition.ResultID = "missing" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempt := reply
			attempt.ID, attempt.IdempotencyKey = tc.name, tc.name
			tc.edit(&attempt)
			if _, _, err := s.SubmitTaskMessage(ctx, attempt); err == nil {
				t.Fatal("forged message accepted")
			}
		})
	}
	resultInput := domain.TaskResultSubmission{ID: "source-result", AttemptID: input.AttemptID, SessionID: input.SessionID, SourceOwner: input.SourceOwner, IdempotencyKey: "result", Definition: domain.TaskResultDefinition{SchemaVersion: 1, ClaimedOutcome: "partial", Summary: "In progress"}}
	if _, _, err := s.SubmitTaskResult(ctx, resultInput); err != nil {
		t.Fatal(err)
	}
	reply.Definition.ResultID = resultInput.ID
	if _, _, err := s.SubmitTaskMessage(ctx, reply); !errors.Is(err, ports.ErrTaskInvalid) {
		t.Fatalf("another worker's result attributed: %v", err)
	}
	input.ID, input.IdempotencyKey, input.Definition.ResultID = "with-result", "with-result", resultInput.ID
	if _, _, err := s.SubmitTaskMessage(ctx, input); err != nil {
		t.Fatalf("own result rejected: %v", err)
	}
}

func TestTaskMessageConcurrentRetriesCreateOneOutboxEntry(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, _, _ := taskMessageFixture(t, s, false)
	var wg sync.WaitGroup
	results := make(chan bool, 6)
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			retry := input
			retry.ID = fmt.Sprint(i)
			_, created, err := s.SubmitTaskMessage(ctx, retry)
			if err != nil {
				t.Errorf("retry: %v", err)
			}
			results <- created
		}()
	}
	wg.Wait()
	close(results)
	created := 0
	for won := range results {
		if won {
			created++
		}
	}
	pending, err := s.ListPendingTaskMessages(ctx, 0, 100)
	if err != nil || created != 1 || len(pending) != 1 {
		t.Fatalf("duplicate side effects: %d %+v %v", created, pending, err)
	}
	if _, _, err := s.BeginTaskMessageDelivery(ctx, pending[0].ID, "unstarted"); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("unstarted target: %v", err)
	}
	deliveries, err := s.ListTaskMessageDeliveries(ctx, pending[0].ID)
	if err != nil || len(deliveries) != 0 {
		t.Fatalf("failed reservation left intent: %+v %v", deliveries, err)
	}
}

func TestTaskMessageAuditRollbackImmutableHistoryAndBounds(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, reply, _ := taskMessageFixture(t, s, true)
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TRIGGER fail_message_audit BEFORE INSERT ON adaptive_task_audit WHEN NEW.action='message_submitted' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.SubmitTaskMessage(ctx, input); err == nil || created {
		t.Fatalf("audit failure ignored: %v %v", created, err)
	}
	if _, err := s.GetTaskMessage(ctx, input.ID); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("partial message persisted: %v", err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_message_audit`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM change_log WHERE event_type='adaptive_task_changed'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := range 256 {
		input.ID, input.IdempotencyKey = fmt.Sprint(i), fmt.Sprint(i)
		if _, _, err := s.SubmitTaskMessage(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	input.ID, input.IdempotencyKey = "overflow", "overflow"
	if _, _, err := s.SubmitTaskMessage(ctx, input); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("attempt message cap: %v", err)
	}
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM change_log WHERE event_type='adaptive_task_changed'`).Scan(&after); err != nil || after != before+256 {
		t.Fatalf("audit CDC: %d -> %d %v", before, after, err)
	}
	for _, statement := range []string{`UPDATE adaptive_task_messages SET definition='{}'`, `DELETE FROM adaptive_task_messages`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("mutable history: %s", statement)
		}
	}
	// Fill retained history without 9,744 service calls; sender two still has no
	// messages, proving the project cap independently from the attempt cap.
	if _, err := db.Exec(`WITH RECURSIVE n(x) AS (SELECT 256 UNION ALL SELECT x+1 FROM n WHERE x<9999)
INSERT INTO adaptive_task_messages(id,project_id,task_id,attempt_id,session_id,native_generation,source_owner,task_revision,criteria_version,configuration_hash,configuration_sequence,context_hash,target_task_id,correlation_id,idempotency_key,definition,content_hash,created_at)
SELECT 'bulk-'||n.x,m.project_id,m.task_id,m.attempt_id,m.session_id,m.native_generation,m.source_owner,m.task_revision,m.criteria_version,m.configuration_hash,m.configuration_sequence,m.context_hash,m.target_task_id,m.correlation_id,'bulk-'||n.x,m.definition,m.content_hash,m.created_at FROM n CROSS JOIN adaptive_task_messages m WHERE m.id='0'`); err != nil {
		t.Fatal(err)
	}
	reply.Definition.ReplyToID = "0"
	if _, _, err := s.SubmitTaskMessage(ctx, reply); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("project message cap: %v", err)
	}
}
