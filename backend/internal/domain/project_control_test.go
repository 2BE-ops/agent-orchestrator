package domain_test

import (
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func controlFixture(state domain.ProjectControlState) domain.ProjectControl {
	return domain.ProjectControl{
		ProjectID: "proj-1",
		State:     state,
		Actor:     domain.AdaptiveActor{Kind: "USER", ID: "local-user"},
		Reason:    "evening maintenance",
		UpdatedAt: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	}
}

func TestProjectControlValidate(t *testing.T) {
	if err := controlFixture(domain.ProjectPaused).Validate(); err != nil {
		t.Fatalf("valid control rejected: %v", err)
	}
	for _, mutate := range []func(*domain.ProjectControl){
		func(c *domain.ProjectControl) { c.State = "hibernating" },
		func(c *domain.ProjectControl) { c.ProjectID = "" },
		func(c *domain.ProjectControl) { c.Actor = domain.AdaptiveActor{Kind: "AGENT_MANAGER", ID: "mgr-1"} },
		func(c *domain.ProjectControl) { c.Actor = domain.AdaptiveActor{Kind: "WORKER", ID: "w-1"} },
		func(c *domain.ProjectControl) {
			c.Actor = domain.AdaptiveActor{Kind: "ORCHESTRATOR", ID: "orch-1", SessionID: "orch-1"}
		},
		func(c *domain.ProjectControl) { c.Reason = "  " },
		func(c *domain.ProjectControl) { c.Reason = "" },
		func(c *domain.ProjectControl) { c.UpdatedAt = time.Time{} },
	} {
		control := controlFixture(domain.ProjectRunning)
		mutate(&control)
		if err := control.Validate(); err == nil {
			t.Fatalf("invalid control accepted: %+v", control)
		}
	}
}

func TestProjectControlTransitions(t *testing.T) {
	legal := []struct{ from, to domain.ProjectControlState }{
		{domain.ProjectRunning, domain.ProjectRunning},
		{domain.ProjectRunning, domain.ProjectPaused},
		{domain.ProjectRunning, domain.ProjectDraining},
		{domain.ProjectRunning, domain.ProjectStopped},
		{domain.ProjectPaused, domain.ProjectPaused},
		{domain.ProjectPaused, domain.ProjectRunning},
		{domain.ProjectPaused, domain.ProjectStopped},
		{domain.ProjectDraining, domain.ProjectDraining},
		{domain.ProjectDraining, domain.ProjectRunning},
		{domain.ProjectDraining, domain.ProjectPaused},
		{domain.ProjectDraining, domain.ProjectStopped},
		{domain.ProjectStopped, domain.ProjectStopped},
		{domain.ProjectStopped, domain.ProjectRunning},
	}
	for _, step := range legal {
		if !domain.ValidProjectControlTransition(step.from, step.to) {
			t.Fatalf("legal transition refused: %s -> %s", step.from, step.to)
		}
	}
	illegal := []struct{ from, to domain.ProjectControlState }{
		{domain.ProjectPaused, domain.ProjectDraining},
		{domain.ProjectStopped, domain.ProjectPaused},
		{domain.ProjectStopped, domain.ProjectDraining},
		{domain.ProjectRunning, "hibernating"},
		{"hibernating", domain.ProjectRunning},
	}
	for _, step := range illegal {
		if domain.ValidProjectControlTransition(step.from, step.to) {
			t.Fatalf("illegal transition accepted: %s -> %s", step.from, step.to)
		}
	}
}

func needsHumanFixture() domain.TaskNeedsHuman {
	return domain.TaskNeedsHuman{
		ID:         "nh-1",
		TaskID:     "task-1",
		ProjectID:  "proj-1",
		ReasonCode: "credential_missing",
		Detail:     "The provider credential expired mid-attempt",
		Actor:      domain.AdaptiveActor{Kind: "WORKER", ID: "worker-1"},
		CreatedAt:  time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	}
}

func TestTaskNeedsHumanValidate(t *testing.T) {
	pending := needsHumanFixture()
	if err := pending.Validate(); err != nil {
		t.Fatalf("valid needs human rejected: %v", err)
	}
	resolved := needsHumanFixture()
	resolved.Resolution = &domain.TaskNeedsHumanResolution{
		Resolution: "Rotated the credential; retry is unblocked",
		Actor:      domain.AdaptiveActor{Kind: "USER", ID: "local-user"},
		ResolvedAt: time.Date(2026, 9, 19, 13, 0, 0, 0, time.UTC),
	}
	if err := resolved.Validate(); err != nil {
		t.Fatalf("valid resolution rejected: %v", err)
	}
	for _, mutate := range []func(*domain.TaskNeedsHuman){
		func(n *domain.TaskNeedsHuman) { n.ReasonCode = "vibes" },
		func(n *domain.TaskNeedsHuman) { n.Detail = "" },
		func(n *domain.TaskNeedsHuman) { n.TaskID = "" },
		func(n *domain.TaskNeedsHuman) { n.Actor = domain.AdaptiveActor{Kind: "USER", ID: ""} },
		func(n *domain.TaskNeedsHuman) { n.Actor = domain.AdaptiveActor{Kind: "LLM", ID: "model-1"} },
		func(n *domain.TaskNeedsHuman) { n.Actor = domain.AdaptiveActor{Kind: "ORCHESTRATOR", ID: "orch-1"} },
		func(n *domain.TaskNeedsHuman) { n.CreatedAt = time.Time{} },
	} {
		item := needsHumanFixture()
		mutate(&item)
		if err := item.Validate(); err == nil {
			t.Fatalf("invalid needs human accepted: %+v", item)
		}
	}
	broken := needsHumanFixture()
	broken.Resolution = &domain.TaskNeedsHumanResolution{Resolution: "", Actor: domain.AdaptiveActor{Kind: "USER", ID: "u"}, ResolvedAt: time.Now()}
	if err := broken.Validate(); err == nil {
		t.Fatal("empty resolution accepted")
	}
}

func TestProjectWorkCancellationValidate(t *testing.T) {
	base := domain.ProjectWorkCancellation{
		ProjectID: "proj-1",
		Scope:     "all",
		Actor:     domain.AdaptiveActor{Kind: "USER", ID: "local-user"},
		Reason:    "Wrong branch of work",
		Now:       time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid cancellation rejected: %v", err)
	}
	for _, mutate := range []func(*domain.ProjectWorkCancellation){
		func(c *domain.ProjectWorkCancellation) { c.Scope = "some" },
		func(c *domain.ProjectWorkCancellation) { c.Scope = "" },
		func(c *domain.ProjectWorkCancellation) {
			c.Actor = domain.AdaptiveActor{Kind: "ORCHESTRATOR", ID: "orch-1", SessionID: "orch-1"}
		},
		func(c *domain.ProjectWorkCancellation) { c.Reason = "" },
		func(c *domain.ProjectWorkCancellation) { c.Now = time.Time{} },
	} {
		item := base
		mutate(&item)
		if err := item.Validate(); err == nil {
			t.Fatalf("invalid cancellation accepted: %+v", item)
		}
	}
}
