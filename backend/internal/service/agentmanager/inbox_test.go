package agentmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type failingInboxStore struct {
	ports.AgentManagerStore
	ports.AgentManagerInboxStore
	err    error
	writes int
}

func (s *failingInboxStore) EnqueueAgentManagerRequest(context.Context, domain.AgentManagerEnqueue) (domain.AgentManagerRequest, bool, error) {
	s.writes++
	return domain.AgentManagerRequest{}, false, s.err
}

func (s *failingInboxStore) ResolveAgentManagerRequest(context.Context, domain.ProjectID, domain.AgentManagerRequestResolution) error {
	s.writes++
	return s.err
}

func TestInboxServiceRejectsAuthorityBeforeStorageAndPreservesFailure(t *testing.T) {
	ctx := context.Background()
	s := &failingInboxStore{err: errors.New("inbox storage unavailable")}
	m := New(s)
	input := EnqueueInput{ID: "request", TaskID: "task", TaskRevision: 1, ConfigurationVersion: 1, Reason: "Route exact work"}
	resolve := ResolveInput{Outcome: "cancelled", Reason: "Close only routing intent"}
	for _, actor := range []domain.AdaptiveActor{{Kind: "WORKER", ID: "worker", SessionID: "worker"}, {Kind: "AGENT_MANAGER", ID: "manager"}, {Kind: "USER", ID: "human", SessionID: "worker"}, {Kind: "ORCHESTRATOR", ID: "planner"}} {
		if _, err := m.Enqueue(ctx, actor, "project", input); err == nil {
			t.Fatalf("enqueue forged actor: %+v", actor)
		}
		if _, err := m.Resolve(ctx, actor, "project", "request", resolve); err == nil {
			t.Fatalf("resolution forged actor: %+v", actor)
		}
	}
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	for _, change := range []func(*EnqueueInput){func(i *EnqueueInput) { i.ID = "" }, func(i *EnqueueInput) { i.TaskRevision = 0 }, func(i *EnqueueInput) { i.ConfigurationVersion = 0 }, func(i *EnqueueInput) { i.Reason = "" }} {
		bad := input
		change(&bad)
		if _, err := m.Enqueue(ctx, actor, "project", bad); err == nil {
			t.Fatal("unbounded work accepted")
		}
	}
	if _, err := m.Resolve(ctx, actor, "project", "request", ResolveInput{Outcome: "launched", Reason: "Claim success"}); err == nil {
		t.Fatal("unvalidated launch accepted")
	}
	if _, err := m.Requests(ctx, "project", -1, 20, true); err == nil {
		t.Fatal("negative cursor")
	}
	if _, err := m.Requests(ctx, "project", 0, 101, false); err == nil {
		t.Fatal("unbounded page")
	}
	if _, err := m.Request(ctx, "project", "bad\nrequest"); err == nil {
		t.Fatal("invalid request ID")
	}
	if s.writes != 0 {
		t.Fatalf("invalid operations reached store: %d", s.writes)
	}
	if _, err := m.Enqueue(ctx, actor, "project", input); !errors.Is(err, s.err) {
		t.Fatalf("storage failure hidden: %v", err)
	}
	if _, err := m.Resolve(ctx, actor, "project", "request", resolve); !errors.Is(err, s.err) {
		t.Fatalf("resolution failure hidden: %v", err)
	}
	if s.writes != 2 {
		t.Fatalf("writes: %d", s.writes)
	}
	if _, err := New(&failingGovernanceStore{}).Enqueue(ctx, actor, "project", input); err == nil {
		t.Fatal("unavailable inbox ignored")
	}
}
