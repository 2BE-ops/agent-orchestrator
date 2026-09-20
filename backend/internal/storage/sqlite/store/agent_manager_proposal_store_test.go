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

const managerSelectionProposal = `{"schemaVersion":1,"action":"select_existing","agentTypeId":"unvalidated-type","agentTypeVersion":1,"rationale":"Native semantic selection, awaiting deterministic validation","candidates":[]}`

func managerNativeFixture(t *testing.T, s *sqlite.Store, mode domain.SessionMode, clearance ...domain.ContextClass) domain.AgentManagerProposalSubmission {
	t.Helper()
	ctx := context.Background()
	reservation, seed, snapshot := managerControllerFixture(t, s, clearance...)
	if mode == domain.SessionModeChat {
		seed.Mode, snapshot.Effective.SessionMode = mode, mode
		snapshot.ContentHash = snapshot.Hash()
	}
	controller, _, err := s.ReserveAgentManagerController(ctx, reservation)
	mustNoError(t, err)
	seed, _, err = s.CreateAgentManagerSession(ctx, controller.AgentManagerControllerToken, seed, snapshot, time.Now().UTC())
	mustNoError(t, err)
	op := domain.AgentManagerExecutionOperation{ID: "proposal-generation", ControllerID: controller.ID, SessionID: seed.ID, SourceOwner: seed.ControllerOwner(), Kind: "dispatch", CreatedAt: time.Now().UTC()}
	_, err = s.BeginAgentManagerExecution(ctx, op)
	mustNoError(t, err)
	if mode == domain.SessionModeChat {
		seed.Metadata.ControllerGeneration = op.ID
	} else {
		seed.Metadata.RuntimeLaunchID = op.ID
	}
	mustNoError(t, s.UpdateSession(ctx, seed))
	mustNoError(t, s.ResolveAgentManagerExecution(ctx, domain.AgentManagerExecutionResolution{OperationID: op.ID, ObservedOwner: seed.ControllerOwner(), Outcome: "connected", Reason: "Observed native connection", CreatedAt: time.Now().UTC()}))
	createTask(t, s, "proposal-task", taskDefinition())
	_, _, err = s.EnqueueAgentManagerRequest(ctx, domain.AgentManagerEnqueue{ID: "proposal-request", ProjectID: "project", TaskID: "proposal-task", TaskRevision: 1, ConfigurationVersion: 1, Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Route bounded work", Now: time.Now().UTC()})
	mustNoError(t, err)
	return domain.AgentManagerProposalSubmission{ID: "proposal", ProjectID: "project", RequestID: "proposal-request", SessionID: seed.ID, SourceOwner: seed.ControllerOwner(), IdempotencyKey: "native-key", Raw: managerSelectionProposal, Now: time.Now().UTC()}
}

func managerProposalFixture(t *testing.T, s *sqlite.Store, mode domain.SessionMode, clearance ...domain.ContextClass) domain.AgentManagerProposalSubmission {
	t.Helper()
	input := managerNativeFixture(t, s, mode, clearance...)
	ctx := context.Background()
	seal := domain.AgentManagerContextSeal{ID: "proposal-context", ProjectID: input.ProjectID, RequestID: input.RequestID, SessionID: input.SessionID, SourceOwner: input.SourceOwner, Now: time.Now().UTC()}
	delivery, _, err := s.BeginAgentManagerDelivery(ctx, seal, "proposal-delivery")
	mustNoError(t, err)
	mustNoError(t, s.ResolveAgentManagerDelivery(ctx, domain.AgentManagerDeliveryResolution{ID: delivery.ID, State: "handed_off", Reason: "Native controller accepted routing input"}))
	input.Now = time.Now().UTC()
	return input
}

func TestAgentManagerProposalRacesExactReplayAndNativeAttribution(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			input := managerProposalFixture(t, s, mode)
			var wg sync.WaitGroup
			created := make(chan bool, 8)
			errorsFound := make(chan error, 8)
			for i := range 8 {
				wg.Go(func() {
					r := input
					r.ID = fmt.Sprintf("proposal-%d", i)
					_, inserted, err := s.SubmitAgentManagerProposal(ctx, r)
					created <- inserted
					errorsFound <- err
				})
			}
			wg.Wait()
			close(created)
			close(errorsFound)
			winners := 0
			for inserted := range created {
				if inserted {
					winners++
				}
			}
			for err := range errorsFound {
				mustNoError(t, err)
			}
			if winners != 1 {
				t.Fatalf("duplicate native output inserted %d records", winners)
			}
			items, err := s.ListAgentManagerProposals(ctx, "project", input.RequestID)
			if err != nil || len(items) != 1 || items[0].Definition == nil || items[0].NativeGeneration != "proposal-generation" || items[0].SessionID != input.SessionID || items[0].Raw != managerSelectionProposal {
				t.Fatalf("native proposal: %+v %v", items, err)
			}
			if _, found, err := s.GetAgentManagerRequestResolution(ctx, "project", input.RequestID); err != nil || found {
				t.Fatalf("parsed selection claimed completion: %v %v", found, err)
			}
			bad := input
			bad.Raw = managerSelectionProposal + " "
			if _, _, err := s.SubmitAgentManagerProposal(ctx, bad); !errors.Is(err, ports.ErrAgentManagerConflict) {
				t.Fatalf("changed exact output: %v", err)
			}
			bad = input
			bad.IdempotencyKey = "second-key"
			if _, _, err := s.SubmitAgentManagerProposal(ctx, bad); !errors.Is(err, ports.ErrAgentManagerConflict) {
				t.Fatalf("replaced unassessed selection: %v", err)
			}
			if _, err := s.GetAgentManagerProposal(ctx, "other", input.RequestID, items[0].ID); !errors.Is(err, ports.ErrAgentManagerNotFound) {
				t.Fatalf("cross-project history: %v", err)
			}
		})
	}
}

func TestAgentManagerProposalMalformedBudgetAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input := managerProposalFixture(t, s, domain.SessionModeTUI)
	input.Raw = ""
	first, _, err := s.SubmitAgentManagerProposal(ctx, input)
	if err != nil || first.Definition != nil || first.ValidationError == "" {
		t.Fatalf("empty output not retained as rejection: %+v %v", first, err)
	}
	mustNoError(t, s.Close())
	s, err = sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	input.Now = time.Now().UTC()
	replay, created, err := s.SubmitAgentManagerProposal(ctx, input)
	if err != nil || created || replay.ContentHash != first.ContentHash {
		t.Fatalf("restart replay: %+v %v %v", replay, created, err)
	}
	for n := 2; n <= 3; n++ {
		input.ID, input.IdempotencyKey, input.Raw, input.Now = fmt.Sprintf("malformed-%d", n), fmt.Sprintf("key-%d", n), "not structured JSON", time.Now().UTC()
		proposal, created, err := s.SubmitAgentManagerProposal(ctx, input)
		if err != nil || !created || proposal.Number != int64(n) || proposal.ValidationError == "" {
			t.Fatalf("malformed correction: %+v %v %v", proposal, created, err)
		}
	}
	resolution, found, err := s.GetAgentManagerRequestResolution(ctx, "project", input.RequestID)
	if err != nil || !found || resolution.Outcome != "needs_human" || resolution.Actor.Kind != "SYSTEM" {
		t.Fatalf("exhaustion did not escalate: %+v %v %v", resolution, found, err)
	}
	input.ID, input.IdempotencyKey, input.Now = "fourth", "fourth-key", time.Now().UTC()
	if _, _, err := s.SubmitAgentManagerProposal(ctx, input); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("unbounded correction: %v", err)
	}
	items, err := s.ListAgentManagerProposals(ctx, "project", input.RequestID)
	if err != nil || len(items) != 3 {
		t.Fatalf("malformed history lost: %+v %v", items, err)
	}
}

func TestAgentManagerProposalConcurrentMalformedBudget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	input := managerProposalFixture(t, s, domain.SessionModeTUI)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for n := range 8 {
		wg.Go(func() {
			r := input
			r.ID, r.IdempotencyKey, r.Raw, r.Now = fmt.Sprintf("bad-%d", n), fmt.Sprintf("bad-key-%d", n), "invalid JSON", time.Now().UTC()
			_, _, err := s.SubmitAgentManagerProposal(ctx, r)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, ports.ErrAgentManagerFenced) && !errors.Is(err, ports.ErrAgentManagerInvalid) {
			t.Fatal(err)
		}
	}
	// Native inputs carry observation times; a later observation can win the
	// write lock first, so older concurrent observations can be refused. Fill
	// remaining budget sequentially without weakening the temporal fence.
	for accepted < 3 {
		input.ID, input.IdempotencyKey, input.Raw, input.Now = fmt.Sprintf("fill-%d", accepted), fmt.Sprintf("fill-key-%d", accepted), "bad JSON", time.Now().UTC()
		_, _, err := s.SubmitAgentManagerProposal(ctx, input)
		mustNoError(t, err)
		accepted++
	}
	if accepted != 3 {
		t.Fatalf("concurrent correction exceeded quota: %d", accepted)
	}
	items, err := s.ListAgentManagerProposals(ctx, "project", input.RequestID)
	if err != nil || len(items) != 3 {
		t.Fatalf("quota history: %d %v", len(items), err)
	}
	audit, err := s.ListAgentManagerAudit(ctx, "project", 0, 100)
	mustNoError(t, err)
	escalations := 0
	for _, event := range audit {
		if event.Action == "request_needs_human" {
			escalations++
		}
	}
	if escalations != 1 {
		t.Fatalf("concurrent escalation count: %d", escalations)
	}
}

func TestAgentManagerProposalFencesStaleOwnersAndMutableIntent(t *testing.T) {
	for _, name := range []string{"stale owner", "terminated", "unconfirmed generation", "pending restore", "task revision", "cancelled task", "policy revision", "disabled policy", "wrong project"} {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()
			input := managerProposalFixture(t, s, domain.SessionModeTUI)
			switch name {
			case "stale owner":
				input.SourceOwner.RuntimeLaunchID = "stale"
			case "terminated", "unconfirmed generation":
				rec, found, err := s.GetSession(ctx, input.SessionID)
				mustNoError(t, err)
				if !found {
					t.Fatal("Manager seed missing")
				}
				if name == "terminated" {
					rec.IsTerminated = true
				} else {
					rec.Metadata.RuntimeLaunchID = "unconfirmed"
				}
				mustNoError(t, s.UpdateSession(ctx, rec))
				input.SourceOwner = rec.ControllerOwner()
			case "pending restore":
				_, err := s.BeginAgentManagerExecution(ctx, domain.AgentManagerExecutionOperation{ID: "unresolved", ControllerID: "controller", SessionID: input.SessionID, SourceOwner: input.SourceOwner, Kind: "restore", CreatedAt: time.Now().UTC()})
				mustNoError(t, err)
			case "task revision":
				_, err := s.ReviseAdaptiveTask(ctx, "proposal-task", taskDefinition(), taskMutation(1))
				mustNoError(t, err)
			case "cancelled task":
				_, err := s.ChangeTaskIntent(ctx, "proposal-task", domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)})
				mustNoError(t, err)
			case "policy revision", "disabled policy":
				configuration, err := s.GetAgentManager(ctx, "project")
				mustNoError(t, err)
				configuration.Definition.Enabled = name != "disabled policy"
				_, err = s.ConfigureAgentManager(ctx, "project", configuration.Definition, taskMutation(1))
				mustNoError(t, err)
			case "wrong project":
				input.ProjectID = "other"
			}
			input.Now = time.Now().UTC()
			if _, _, err := s.SubmitAgentManagerProposal(ctx, input); err == nil {
				t.Fatal("unsafe native submission accepted")
			}
			seal := domain.AgentManagerContextSeal{ID: "fenced-context", ProjectID: input.ProjectID, RequestID: input.RequestID, SessionID: input.SessionID, SourceOwner: input.SourceOwner, Now: input.Now}
			if _, _, err := s.SealAgentManagerContext(ctx, seal); err == nil {
				t.Fatal("unsafe native context accepted")
			}
			contexts, err := s.ListAgentManagerContexts(ctx, "project", input.RequestID)
			if err != nil || len(contexts) != 1 || contexts[0].ID != "proposal-context" {
				t.Fatalf("fenced input left partial state: %+v %v", contexts, err)
			}
			items, err := s.ListAgentManagerProposals(ctx, "project", input.RequestID)
			if err != nil || len(items) != 0 {
				t.Fatalf("fenced output left partial state: %+v %v", items, err)
			}
		})
	}
}

func TestAgentManagerProposalEscalationAndAuditRollback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input := managerProposalFixture(t, s, domain.SessionModeTUI)
	input.Raw = `{"schemaVersion":1,"action":"needs_human","rationale":"No compatible permitted worker","candidates":[]}`
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TRIGGER fail_proposal_audit BEFORE INSERT ON adaptive_agent_manager_audit WHEN NEW.action='request_needs_human' BEGIN SELECT RAISE(ABORT,'injected escalation audit failure'); END`)
	mustNoError(t, err)
	var before, after int
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before))
	if _, _, err := s.SubmitAgentManagerProposal(ctx, input); err == nil {
		t.Fatal("proposal and escalation survived failed audit")
	}
	items, err := s.ListAgentManagerProposals(ctx, "project", input.RequestID)
	if err != nil || len(items) != 0 {
		t.Fatalf("partial proposal: %+v %v", items, err)
	}
	if _, found, err := s.GetAgentManagerRequestResolution(ctx, "project", input.RequestID); err != nil || found {
		t.Fatalf("partial escalation: %v %v", found, err)
	}
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after))
	if after != before {
		t.Fatal("failed proposal leaked CDC")
	}
	_, err = db.Exec(`DROP TRIGGER fail_proposal_audit`)
	mustNoError(t, err)
	proposal, created, err := s.SubmitAgentManagerProposal(ctx, input)
	if err != nil || !created || proposal.Number != 1 {
		t.Fatalf("recovery: %+v %v %v", proposal, created, err)
	}
	if _, found, err := s.GetAgentManagerRequestResolution(ctx, "project", input.RequestID); err != nil || !found {
		t.Fatalf("escalation missing: %v %v", found, err)
	}
	for _, statement := range []string{`UPDATE adaptive_agent_manager_proposals SET snapshot='{}'`, `DELETE FROM adaptive_agent_manager_proposals`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("mutated proposal history: %s", statement)
		}
	}
	if _, err := db.Exec(`INSERT INTO adaptive_agent_manager_proposals(id,request_id,number,idempotency_key,controller_id,session_id,source_owner,snapshot,content_hash,created_at) SELECT 'wrong-session',request_id,2,'other-key',controller_id,'unrelated-worker',source_owner,snapshot,content_hash,created_at FROM adaptive_agent_manager_proposals`); err == nil {
		t.Fatal("SQL accepted unrelated native session")
	}
}
