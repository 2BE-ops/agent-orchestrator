package agentmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type proposalServiceStore struct {
	ports.AgentManagerStore
	ports.AgentManagerProposalStore
	rec               domain.SessionRecord
	found             bool
	readErr, writeErr error
	captured          *domain.AgentManagerProposalSubmission
}

func (s *proposalServiceStore) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return s.rec, s.found, s.readErr
}
func (s *proposalServiceStore) SubmitAgentManagerProposal(_ context.Context, input domain.AgentManagerProposalSubmission) (domain.AgentManagerProposal, bool, error) {
	s.captured = &input
	return domain.AgentManagerProposal{ID: input.ID}, s.writeErr == nil, s.writeErr
}

func TestNativeManagerProposalServiceDerivesOwnerAndPreservesFailure(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			store := &proposalServiceStore{rec: domain.SessionRecord{ID: "manager", ProjectID: "project", Kind: domain.KindAgentManager, Harness: domain.HarnessCodex, Mode: mode, Metadata: domain.SessionMetadata{RuntimeLaunchID: "generation", ControllerGeneration: "generation"}}, found: true}
			manager := New(store)
			input := ProposalInput{SourceGeneration: "generation", IdempotencyKey: "key", Raw: ""}
			receipt, err := manager.Propose(ctx, "manager", "request", input)
			if err != nil || !receipt.Created || store.captured == nil || store.captured.ID == "" || store.captured.ProjectID != "project" || store.captured.SourceOwner != store.rec.ControllerOwner() || store.captured.RequestID != "request" || store.captured.Raw != "" {
				t.Fatalf("native owner not derived: %+v %v", store.captured, err)
			}
			store.captured = nil
			for _, change := range []func(*ProposalInput){func(i *ProposalInput) { i.SourceGeneration = "stale" }, func(i *ProposalInput) { i.SourceGeneration = "" }, func(i *ProposalInput) { i.IdempotencyKey = "" }, func(i *ProposalInput) { i.Raw = strings.Repeat("x", (64<<10)+1) }} {
				bad := input
				change(&bad)
				if _, err := manager.Propose(ctx, "manager", "request", bad); err == nil || store.captured != nil {
					t.Fatalf("invalid native claim reached store: %v", err)
				}
			}
			store.rec.Kind = domain.KindWorker
			if _, err := manager.Propose(ctx, "manager", "request", input); err == nil || store.captured != nil {
				t.Fatal("worker impersonated Manager")
			}
			store.rec.Kind, store.rec.IsTerminated = domain.KindAgentManager, true
			if _, err := manager.Propose(ctx, "manager", "request", input); err == nil || store.captured != nil {
				t.Fatal("terminated Manager submitted")
			}
			store.rec.IsTerminated = false
			failure := errors.New("native proposal database unavailable")
			store.readErr = failure
			if _, err := manager.Propose(ctx, "manager", "request", input); !errors.Is(err, failure) {
				t.Fatalf("read failure hidden: %v", err)
			}
			store.readErr, store.writeErr = nil, failure
			if _, err := manager.Propose(ctx, "manager", "request", input); !errors.Is(err, failure) {
				t.Fatalf("write failure hidden: %v", err)
			}
		})
	}
}
