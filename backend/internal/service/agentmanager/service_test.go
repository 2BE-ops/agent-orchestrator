package agentmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type failingGovernanceStore struct {
	ports.AgentManagerStore
	err    error
	writes int
}

func (s *failingGovernanceStore) ConfigureAgentManager(context.Context, domain.ProjectID, domain.AgentManagerDefinition, domain.TaskMutation) (domain.AgentManagerConfiguration, error) {
	s.writes++
	return domain.AgentManagerConfiguration{}, s.err
}

func TestGovernanceRejectsEscalationBeforePersistenceAndPreservesFailure(t *testing.T) {
	s := &failingGovernanceStore{err: errors.New("governance database unavailable")}
	m := New(s)
	ctx := context.Background()
	input := ConfigureInput{Definition: domain.AgentManagerDefinition{SchemaVersion: 1, AgentTypeID: "controller", AgentTypeVersion: 1, Policy: domain.DefaultAgentManagerPolicy()}, Reason: "Set policy"}
	for _, actor := range []domain.AdaptiveActor{{Kind: "AGENT_MANAGER", ID: "manager"}, {Kind: "ORCHESTRATOR", ID: "planner", SessionID: "planner"}, {Kind: "WORKER", ID: "worker"}, {Kind: "SYSTEM", ID: "daemon"}, {Kind: "USER", ID: "human", SessionID: "worker"}} {
		if _, err := m.Configure(ctx, actor, "project", input); err == nil {
			t.Fatalf("actor escaped: %+v", actor)
		}
	}
	actor := domain.AdaptiveActor{Kind: "USER", ID: "human"}
	for _, change := range []func(*ConfigureInput){func(i *ConfigureInput) { i.ExpectedRevision = -1 }, func(i *ConfigureInput) { i.ExpectedRevision = 1000 }, func(i *ConfigureInput) { i.Definition.Policy.MaxProposalAttempts = 99 }, func(i *ConfigureInput) { i.Reason = "" }} {
		bad := input
		change(&bad)
		if _, err := m.Configure(ctx, actor, "project", bad); err == nil {
			t.Fatal("invalid governance accepted")
		}
	}
	if _, err := m.Configurations(ctx, "project", -1, 20); err == nil {
		t.Fatal("negative cursor")
	}
	if _, err := m.Audit(ctx, "project", 0, 101); err == nil {
		t.Fatal("unbounded page")
	}
	if _, err := m.Configuration(ctx, "project", 0); err == nil {
		t.Fatal("floating config")
	}
	if _, err := m.Get(ctx, "bad\nproject"); err == nil {
		t.Fatal("invalid project")
	}
	if s.writes != 0 {
		t.Fatalf("denied writes reached persistence: %d", s.writes)
	}
	if _, err := m.Configure(ctx, actor, "project", input); !errors.Is(err, s.err) || s.writes != 1 {
		t.Fatalf("operational failure hidden: %v", err)
	}
}
