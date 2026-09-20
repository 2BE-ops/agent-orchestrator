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

func TestAgentManagerDeliveryRaceAndUncertainConversationRetention(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	native := managerNativeFixture(t, s, domain.SessionModeTUI, domain.ContextMission)
	input := managerContextRequest(t, s, native, "delivery", domain.ContextEngagement, "client-a")
	var wg sync.WaitGroup
	created := make(chan bool, 8)
	for range 8 {
		wg.Go(func() {
			_, added, err := s.BeginAgentManagerDelivery(ctx, input, "one-send")
			if err != nil {
				t.Error(err)
			}
			created <- added
		})
	}
	wg.Wait()
	close(created)
	winners := 0
	for added := range created {
		if added {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("reserved %d native writes", winners)
	}
	if _, _, err := s.BeginAgentManagerDelivery(ctx, input, "second-send"); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("duplicate native write permitted: %v", err)
	}
	contexts, err := s.ListAgentManagerContexts(ctx, "project", input.RequestID)
	if err != nil || len(contexts) != 1 {
		t.Fatalf("duplicate context on busy send: %+v %v", contexts, err)
	}
	other := managerContextRequest(t, s, native, "other", domain.ContextTechnical, "")
	if _, _, err := s.BeginAgentManagerDelivery(ctx, other, "other-send"); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("interleaved native input: %v", err)
	}
	mustNoError(t, s.SetAgentManagerDispatchCursor(ctx, 7))
	mustNoError(t, s.Close())
	s, err = sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	cursor, err := s.AgentManagerDispatchCursor(ctx)
	if err != nil || cursor != 7 {
		t.Fatalf("scan cursor lost: %d %v", cursor, err)
	}
	unresolved, err := s.ListUnresolvedAgentManagerDeliveries(ctx, "", 100)
	if err != nil || len(unresolved) != 1 || unresolved[0].ID != "one-send" {
		t.Fatalf("restart lost send intent: %+v %v", unresolved, err)
	}
	resolution := domain.AgentManagerDeliveryResolution{ID: "one-send", State: "uncertain", Reason: "Process restarted before send outcome was retained"}
	mustNoError(t, s.ResolveAgentManagerDelivery(ctx, resolution))
	mustNoError(t, s.ResolveAgentManagerDelivery(ctx, resolution))
	resolution.State = "not_sent"
	if err := s.ResolveAgentManagerDelivery(ctx, resolution); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("uncertain write became retryable: %v", err)
	}
	mustNoError(t, s.ResolveAgentManagerRequest(ctx, "project", domain.AgentManagerRequestResolution{RequestID: input.RequestID, Outcome: "needs_human", Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "test"}, Reason: "Inspect unknown native write", CreatedAt: time.Now().UTC()}))
	if _, _, err := s.BeginAgentManagerDelivery(ctx, other, "other-send"); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("routing resolution cleared uncertain composer: %v", err)
	}
	replay, added, err := s.BeginAgentManagerDelivery(ctx, input, "one-send")
	if err != nil || added || replay.State != "uncertain" {
		t.Fatalf("retry did not inspect retained uncertainty: %+v %v %v", replay, added, err)
	}
}

func TestAgentManagerDeliveryRetriesOnlyProvenNoSendAndBoundsAttempts(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	native := managerNativeFixture(t, s, domain.SessionModeChat)
	input := managerContextRequest(t, s, native, "retry", domain.ContextTechnical, "")
	for i := 1; i <= 4; i++ {
		input.ID, input.Now = fmt.Sprintf("context-%d", i), time.Now().UTC()
		delivery, created, err := s.BeginAgentManagerDelivery(ctx, input, fmt.Sprintf("send-%d", i))
		if err != nil || !created || delivery.Number != int64(i) || delivery.DeliveryKey != "adaptive-manager:"+input.RequestID {
			t.Fatalf("retry: %+v %v %v", delivery, created, err)
		}
		mustNoError(t, s.ResolveAgentManagerDelivery(ctx, domain.AgentManagerDeliveryResolution{ID: delivery.ID, State: "not_sent", Reason: "Guard refused before I/O"}))
	}
	input.ID, input.Now = "fifth-context", time.Now().UTC()
	if _, _, err := s.BeginAgentManagerDelivery(ctx, input, "fifth-send"); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("unbounded retries: %v", err)
	}
	contexts, err := s.ListAgentManagerContexts(ctx, "project", input.RequestID)
	if err != nil || len(contexts) != 4 {
		t.Fatalf("failed send budget left a context: %+v %v", contexts, err)
	}
	resolution, found, err := s.GetAgentManagerRequestResolution(ctx, "project", input.RequestID)
	if err != nil || !found || resolution.Outcome != "needs_human" {
		t.Fatalf("exhausted delivery silently stalled: %+v %v", resolution, err)
	}
	pending, err := s.ListDispatchableAgentManagerRequests(ctx, 0, 100)
	mustNoError(t, err)
	for _, request := range pending {
		if request.ID == input.RequestID {
			t.Fatal("exhausted request remains automatically dispatchable")
		}
	}
}

func TestAgentManagerDeliveryWaitsForRoutingResolution(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	native := managerProposalFixture(t, s, domain.SessionModeTUI)
	other := managerContextRequest(t, s, native, "next-routing", domain.ContextTechnical, "")
	if _, _, err := s.BeginAgentManagerDelivery(ctx, other, "next-send"); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("another routing request overlapped: %v", err)
	}
	native.Raw = `{"schemaVersion":1,"action":"needs_human","rationale":"No compatible candidate","candidates":[]}`
	native.Now = time.Now().UTC()
	if _, _, err := s.SubmitAgentManagerProposal(ctx, native); err != nil {
		t.Fatal(err)
	}
	other.Now = time.Now().UTC()
	if _, created, err := s.BeginAgentManagerDelivery(ctx, other, "next-send"); err != nil || !created {
		t.Fatalf("resolved routing starved unrelated work: %v %v", created, err)
	}
}

func TestAgentManagerDeliveryAndContextRollbackTogether(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	native := managerNativeFixture(t, s, domain.SessionModeTUI)
	input := managerContextRequest(t, s, native, "atomic-delivery", domain.ContextTechnical, "")
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TRIGGER fail_delivery_audit BEFORE INSERT ON adaptive_agent_manager_audit WHEN NEW.action='delivery_reserved' BEGIN SELECT RAISE(ABORT,'injected failure'); END`)
	mustNoError(t, err)
	var before, after int
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before))
	if _, _, err := s.BeginAgentManagerDelivery(ctx, input, "atomic-send"); err == nil {
		t.Fatal("audit failure ignored")
	}
	contexts, err := s.ListAgentManagerContexts(ctx, "project", input.RequestID)
	if err != nil || len(contexts) != 0 {
		t.Fatalf("orphan sealed input: %+v %v", contexts, err)
	}
	deliveries, err := s.ListAgentManagerDeliveries(ctx, "project", input.RequestID)
	if err != nil || len(deliveries) != 0 {
		t.Fatalf("orphan send reservation: %+v %v", deliveries, err)
	}
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after))
	if before != after {
		t.Fatal("failed reservation emitted CDC")
	}
	_, err = db.Exec(`DROP TRIGGER fail_delivery_audit`)
	mustNoError(t, err)
	_, _, err = s.BeginAgentManagerDelivery(ctx, input, "atomic-send")
	mustNoError(t, err)
	for _, statement := range []string{`UPDATE adaptive_agent_manager_deliveries SET context_id='changed',state='not_sent'`, `DELETE FROM adaptive_agent_manager_deliveries`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("SQL rewrote delivery attribution: %s", statement)
		}
	}
}

func TestAgentManagerProposalRequiresInputAndInheritsConversationClass(t *testing.T) {
	ctx := context.Background()
	for _, state := range []string{"missing", "not_sent", "dispatching", "handed_off", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			s := newTestStore(t)
			native := managerNativeFixture(t, s, domain.SessionModeTUI, domain.ContextMission)
			input := managerContextRequest(t, s, native, "private", domain.ContextMission, "client-a")
			private, _, err := s.SealAgentManagerContext(ctx, input)
			mustNoError(t, err)
			var sealed domain.AgentManagerContext
			if state != "missing" {
				seal := domain.AgentManagerContextSeal{ID: "received-context", ProjectID: native.ProjectID, RequestID: native.RequestID, SessionID: native.SessionID, SourceOwner: native.SourceOwner, Now: time.Now().UTC()}
				delivery, _, err := s.BeginAgentManagerDelivery(ctx, seal, "received-send")
				mustNoError(t, err)
				sealed, err = s.GetAgentManagerContext(ctx, "project", native.RequestID, seal.ID)
				mustNoError(t, err)
				if sealed.PreviousContextHash != private.ContentHash {
					t.Fatal("lost conversation history")
				}
				if state != "dispatching" {
					mustNoError(t, s.ResolveAgentManagerDelivery(ctx, domain.AgentManagerDeliveryResolution{ID: delivery.ID, State: state, Reason: "Observed transport state"}))
				}
			}
			native.Now = time.Now().UTC()
			proposal, created, err := s.SubmitAgentManagerProposal(ctx, native)
			if state == "missing" || state == "not_sent" {
				if !errors.Is(err, ports.ErrAgentManagerFenced) || created {
					t.Fatalf("unreceived input was attributed: %+v %v", proposal, err)
				}
				return
			}
			if err != nil || !created || proposal.ContextID != sealed.ID || proposal.ContextHash != sealed.ContentHash || proposal.ConversationContextHash != sealed.ContentHash || proposal.Classification != domain.ContextMission || proposal.EngagementID != "client-a" {
				t.Fatalf("proposal lost classified input provenance: %+v %v", proposal, err)
			}
		})
	}
}
