package domain

import (
	"strings"
	"testing"
	"time"
)

func TestAgentManagerInboxAuthorityAndSealedReferences(t *testing.T) {
	input := AgentManagerEnqueue{ID: "request", ProjectID: "project", TaskID: "task", TaskRevision: 1, ConfigurationVersion: 1, Actor: AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Choose a suitable worker", Now: time.Now().UTC()}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []AdaptiveActor{{Kind: "WORKER", ID: "worker", SessionID: "worker"}, {Kind: "AGENT_MANAGER", ID: "manager", SessionID: "manager"}, {Kind: "ORCHESTRATOR", ID: "orchestrator"}, {Kind: "USER", ID: "human", SessionID: "worker"}, {Kind: "SYSTEM", ID: "daemon", SessionID: "manager"}, {Kind: "USER", ID: "bad\nidentity"}} {
		bad := input
		bad.Actor = actor
		if err := bad.Validate(); err == nil {
			t.Fatalf("accepted forged inbox authority: %+v", actor)
		}
	}
	for _, mutate := range []func(*AgentManagerEnqueue){func(r *AgentManagerEnqueue) { r.TaskRevision = 0 }, func(r *AgentManagerEnqueue) { r.ConfigurationVersion = 0 }, func(r *AgentManagerEnqueue) { r.ConfigurationVersion = 1001 }, func(r *AgentManagerEnqueue) { r.ID = strings.Repeat("x", 201) }, func(r *AgentManagerEnqueue) { r.Now = time.Time{} }} {
		bad := input
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Fatal("accepted unbounded or floating work")
		}
	}
	hash := strings.Repeat("a", 64)
	r := AgentManagerRequest{ID: input.ID, SchemaVersion: 1, ProjectID: input.ProjectID, Kind: "select_worker", TaskID: input.TaskID, TaskRevision: 1, TaskContentHash: hash, CriteriaVersion: 1, CriteriaContentHash: hash, ConfigurationVersion: 1, ConfigurationHash: hash, Actor: input.Actor, Reason: input.Reason, CreatedAt: input.Now}
	r.ContentHash = r.Hash()
	r.Sequence = 12
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.CriteriaContentHash = strings.Repeat("b", 64)
	if err := r.Validate(); err == nil {
		t.Fatal("criteria provenance changed without resealing")
	}
	r.CriteriaVersion = 0
	r.ContentHash = r.Hash()
	if err := r.Validate(); err == nil {
		t.Fatal("accepted unfrozen criteria")
	}
}

func TestAgentManagerRequestResolutionCannotClaimLaunch(t *testing.T) {
	r := AgentManagerRequestResolution{RequestID: "request", Actor: AdaptiveActor{Kind: "SYSTEM", ID: "daemon"}, Reason: "Bounded correction exhausted", CreatedAt: time.Now().UTC()}
	for _, outcome := range []string{"cancelled", "superseded", "needs_human"} {
		r.Outcome = outcome
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, outcome := range []string{"selected", "launched", "completed", ""} {
		r.Outcome = outcome
		if err := r.Validate(); err == nil {
			t.Fatalf("accepted unvalidated success: %s", outcome)
		}
	}
}
