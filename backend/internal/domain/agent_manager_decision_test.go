package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func managerDecisionDomainFixture() AgentManagerDecision {
	hash := strings.Repeat("a", 64)
	now := time.Now().UTC()
	candidate := AgentManagerCandidate{AgentType: WorkerDefinitionRef{ID: "type", Version: 1, Name: "Worker", ContentHash: hash}, MetadataRevision: 1, MaxContextClass: ContextTechnical, Harness: HarnessCodex, SessionMode: SessionModeTUI, Config: AgentConfig{Permissions: PermissionModeAuto}, Capabilities: []string{}, MissingCapabilities: []string{}, Skills: []AgentManagerCandidateSkill{}, Issues: []AgentManagerCandidateIssue{}, Eligible: true}
	d := AgentManagerDecision{SchemaVersion: 1, ProjectID: "project", RequestID: "request", ProposalID: "proposal", RequestHash: hash, ProposalHash: hash, ConfigurationHash: hash, ProjectConfigurationHash: hash, Optimization: "balanced", Classification: ContextEngagement, EngagementID: "client-a", Candidates: []AgentManagerCandidate{candidate}, Outcome: "accepted", ObservedAt: now, CreatedAt: now}
	d.ContentHash = d.Hash()
	return d
}

func TestManagerDecisionHistoryValidatesWithoutLiveConfiguration(t *testing.T) {
	d := managerDecisionDomainFixture()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var restored AgentManagerDecision
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Validate(); err != nil || restored.Hash() != d.Hash() {
		t.Fatalf("history drifted: %v", err)
	}
	for _, mutate := range []func(*AgentManagerDecision){
		func(d *AgentManagerDecision) { d.Outcome = "rejected" },
		func(d *AgentManagerDecision) { d.Candidates[0].Eligible = false },
		func(d *AgentManagerDecision) {
			d.Candidates[0].Issues = []AgentManagerCandidateIssue{{Code: "PROHIBITED", State: "invalid"}}
		},
		func(d *AgentManagerDecision) { d.Candidates[0].MetadataRevision = 0 },
		func(d *AgentManagerDecision) { d.Candidates = append(d.Candidates, d.Candidates[0]) },
		func(d *AgentManagerDecision) { d.Candidates = nil },
		func(d *AgentManagerDecision) { d.Classification = "" },
		func(d *AgentManagerDecision) { d.EngagementID = "" },
		func(d *AgentManagerDecision) { d.Optimization = "best-score" },
		func(d *AgentManagerDecision) { d.ObservedAt = d.CreatedAt.Add(time.Second) },
	} {
		changed := managerDecisionDomainFixture()
		mutate(&changed)
		changed.ContentHash = changed.Hash()
		if err := changed.Validate(); err == nil {
			t.Fatalf("accepted inconsistent decision: %+v", changed)
		}
	}
	d.Candidates[0].AgentType.Name = "Changed"
	if err := d.Validate(); err == nil {
		t.Fatal("rewritten content kept old hash")
	}
	d = managerDecisionDomainFixture()
	d.Candidates[0].Eligible = false
	d.Candidates[0].Issues = []AgentManagerCandidateIssue{{Code: "NATIVE_READINESS_UNAVAILABLE", State: "unavailable"}}
	d.Outcome = "rejected"
	d.ContentHash = d.Hash()
	if err := d.Validate(); err != nil {
		t.Fatalf("bounded rejection lost: %v", err)
	}
}

func TestManagerSelectedResolutionRequiresDecisionAuthority(t *testing.T) {
	r := AgentManagerRequestResolution{RequestID: "request", Outcome: "selected", Actor: AdaptiveActor{Kind: "SYSTEM", ID: "manager-selector"}, Reason: "Recorded selection", CreatedAt: time.Now().UTC()}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []AdaptiveActor{{Kind: "USER", ID: "human"}, {Kind: "AGENT_MANAGER", ID: "manager"}, {Kind: "SYSTEM", ID: "other"}, {Kind: "SYSTEM", ID: "manager-selector", SessionID: "worker"}} {
		r.Actor = actor
		if err := r.Validate(); err == nil {
			t.Fatalf("forged selection authority: %+v", actor)
		}
	}
}
