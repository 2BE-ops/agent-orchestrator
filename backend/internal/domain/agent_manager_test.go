package domain

import (
	"strings"
	"testing"
	"time"
)

func managerDefinition() AgentManagerDefinition {
	return AgentManagerDefinition{SchemaVersion: 1, AgentTypeID: "manager-type", AgentTypeVersion: 1, Policy: DefaultAgentManagerPolicy()}
}

func TestAgentManagerPolicyBoundsAndExactControllerVersion(t *testing.T) {
	valid := managerDefinition()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if valid.Enabled || valid.Policy.AllowCreateTypes || valid.Policy.AllowCreateSkills || valid.Policy.AllowCreateVersions {
		t.Fatal("default manager governance enabled evolution")
	}
	for name, change := range map[string]func(*AgentManagerDefinition){
		"schema":              func(d *AgentManagerDefinition) { d.SchemaVersion = 2 },
		"missing type":        func(d *AgentManagerDefinition) { d.AgentTypeID = "" },
		"control type":        func(d *AgentManagerDefinition) { d.AgentTypeID = "bad\ntype" },
		"floating version":    func(d *AgentManagerDefinition) { d.AgentTypeVersion = 0 },
		"preference":          func(d *AgentManagerDefinition) { d.Policy.Optimization = "score" },
		"type explosion":      func(d *AgentManagerDefinition) { d.Policy.MaxCreatedTypes = 33 },
		"skill explosion":     func(d *AgentManagerDefinition) { d.Policy.MaxCreatedSkills = 65 },
		"version explosion":   func(d *AgentManagerDefinition) { d.Policy.MaxVersionsPerEntry = 33 },
		"inbox explosion":     func(d *AgentManagerDefinition) { d.Policy.MaxPendingRequests = 1001 },
		"retry explosion":     func(d *AgentManagerDefinition) { d.Policy.MaxProposalAttempts = 6 },
		"empty inbox":         func(d *AgentManagerDefinition) { d.Policy.MaxPendingRequests = 0 },
		"empty retries":       func(d *AgentManagerDefinition) { d.Policy.MaxProposalAttempts = 0 },
		"negative quota":      func(d *AgentManagerDefinition) { d.Policy.MaxCreatedSkills = -1 },
		"unbudgeted types":    func(d *AgentManagerDefinition) { d.Policy.AllowCreateTypes = true; d.Policy.MaxCreatedTypes = 0 },
		"unbudgeted skills":   func(d *AgentManagerDefinition) { d.Policy.AllowCreateSkills = true; d.Policy.MaxCreatedSkills = 0 },
		"unbudgeted versions": func(d *AgentManagerDefinition) { d.Policy.AllowCreateVersions = true; d.Policy.MaxVersionsPerEntry = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			d := valid
			change(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("invalid manager policy accepted")
			}
		})
	}
}

func TestAgentManagerConfigurationSealsPolicyAndUserProvenance(t *testing.T) {
	c := AgentManagerConfiguration{ProjectID: "project", Number: 1, Definition: managerDefinition(), ControllerType: WorkerDefinitionRef{ID: "manager-type", Version: 1, Name: "Manager", ContentHash: strings.Repeat("a", 64)}, Actor: AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Choose manager governance", CreatedAt: time.Now().UTC()}
	c.ContentHash = c.Hash()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*AgentManagerConfiguration){
		"policy":   func(c *AgentManagerConfiguration) { c.Definition.Policy.AllowCreateTypes = true },
		"type pin": func(c *AgentManagerConfiguration) { c.ControllerType.Version = 2 },
		"actor":    func(c *AgentManagerConfiguration) { c.Actor.ID = "other" },
		"reason":   func(c *AgentManagerConfiguration) { c.Reason = "Silent policy escalation" },
	} {
		t.Run(name, func(t *testing.T) {
			altered := c
			change(&altered)
			if err := altered.Validate(); err == nil {
				t.Fatal("altered governance accepted")
			}
		})
	}
	for _, actor := range []AdaptiveActor{{Kind: "AGENT_MANAGER", ID: "manager"}, {Kind: "WORKER", ID: "worker"}, {Kind: "ORCHESTRATOR", ID: "planner", SessionID: "planner"}, {Kind: "SYSTEM", ID: "daemon"}, {Kind: "USER", ID: "human", SessionID: "borrowed-worker"}} {
		altered := c
		altered.Actor = actor
		altered.ContentHash = altered.Hash()
		if err := altered.Validate(); err == nil {
			t.Fatalf("non-user governance accepted: %+v", actor)
		}
	}
}
