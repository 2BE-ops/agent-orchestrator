package agentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type selectionNative struct {
	checks  int
	onCheck func()
}

func (n *selectionNative) Configuration(context.Context, string, domain.SessionMode) (agentsvc.Configuration, error) {
	n.checks++
	if n.onCheck != nil {
		n.onCheck()
	}
	return agentsvc.Configuration{CapabilityState: "supported", Fields: []agentsvc.ConfigurationField{{Key: "permissions", Options: []string{"auto", "default"}}}}, nil
}
func (n *selectionNative) Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error) {
	return ports.AgentModelCatalog{BinaryVersion: "native-checked", SelectionMode: ports.ModelSelectionCatalog}, nil
}
func (n *selectionNative) EnsureAgentReadiness(context.Context, string, domain.AgentReadinessPurpose) (domain.AgentReadinessSnapshot, error) {
	return domain.AgentReadinessSnapshot{EffectiveReadiness: domain.AgentReadinessReady}, nil
}

func selectionFixture(t *testing.T) (*InboxDispatcher, *inboxNative, *sqlite.Store, *selectionNative) {
	t.Helper()
	d, n, s := inboxFixture(t, domain.ContextMission)
	_, err := s.CreateRegistryEntry(context.Background(), "worker-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Worker", Enabled: true, Policy: domain.RegistryPolicy{ManagerCanSelect: true}}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeTUI, MaxParallelWorkers: 1}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Worker candidate"})
	if err != nil {
		t.Fatal(err)
	}
	native := &selectionNative{}
	d.controllers.SetCandidateAssessor(registrysvc.NewWithNative(s, native))
	return d, n, s, native
}

func selectionInput(t *testing.T, c domain.AgentManagerContext, typeID, key string) ProposalInput {
	t.Helper()
	raw, err := json.Marshal(domain.AgentManagerProposalDefinition{SchemaVersion: 1, Action: "select_existing", AgentTypeID: typeID, AgentTypeVersion: 1, Rationale: "Use the compatible existing definition for the pinned task", Candidates: []domain.AgentManagerCandidateReason{{AgentTypeID: "manager-type", Version: 1, Reason: "Controller is protected from worker selection"}, {AgentTypeID: typeID, Version: 1, Reason: "Selected candidate"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Avoid duplicate candidate reasons when proposing the protected controller.
	if typeID == "manager-type" {
		raw = []byte(`{"schemaVersion":1,"action":"select_existing","agentTypeId":"manager-type","agentTypeVersion":1,"rationale":"Check protected selection","candidates":[]}`)
	}
	return ProposalInput{SourceGeneration: c.NativeGeneration, IdempotencyKey: key, Raw: string(raw)}
}

func TestManagerNativeProposalAssessesCorrectionsAndReturnsRetainedDecision(t *testing.T) {
	ctx := context.Background()
	d, n, s, native := selectionFixture(t)
	request := inboxRequest(t, s, "project", "select-work", domain.ContextTechnical, "client-a")
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	sealed := n.inputs[0]
	first, err := d.controllers.Propose(ctx, sealed.SessionID, request.ID, selectionInput(t, sealed, "manager-type", "rejected"))
	if err != nil || first.Decision == nil || first.Decision.Outcome != "rejected" || first.RoutingOutcome != "" || native.checks != 0 {
		t.Fatalf("protected choice not rejected: %+v %v", first, err)
	}
	input := selectionInput(t, sealed, "worker-type", "accepted")
	accepted, err := d.controllers.Propose(ctx, sealed.SessionID, request.ID, input)
	if err != nil || accepted.Decision == nil || accepted.Decision.Outcome != "accepted" || accepted.RoutingOutcome != "selected" || accepted.Proposal.Number != 2 || len(accepted.Decision.Candidates) != 2 || accepted.Decision.Candidates[0].AgentType.ID != "worker-type" || accepted.Decision.Candidates[1].Eligible || native.checks != 1 || n.starts != 1 {
		t.Fatalf("native selection not applied to routing: %+v %v", accepted, err)
	}
	configuration, err := s.GetAgentManager(ctx, "project")
	if err != nil {
		t.Fatal(err)
	}
	configuration.Definition.Enabled = false
	if _, err := s.ConfigureAgentManager(ctx, "project", configuration.Definition, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Disable later work", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	replayed, err := d.controllers.Propose(ctx, sealed.SessionID, request.ID, input)
	if err != nil || replayed.Created || replayed.Decision == nil || replayed.Decision.ContentHash != accepted.Decision.ContentHash || native.checks != 1 {
		t.Fatalf("retry re-assessed history: %+v %v", replayed, err)
	}
	history, err := d.controllers.Decisions(ctx, "project", request.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("decision history: %+v %v", history, err)
	}
	if _, err := d.controllers.Decision(ctx, "other", request.ID, accepted.Proposal.ID); err == nil {
		t.Fatal("decision escaped project scope")
	}
}

func TestManagerDecisionRecoversInterruptedNativeAssessment(t *testing.T) {
	ctx := context.Background()
	d, n, s, native := selectionFixture(t)
	request := inboxRequest(t, s, "project", "interrupted-work", domain.ContextTechnical, "")
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	sealed := n.inputs[0]
	callCtx, cancel := context.WithCancel(ctx)
	native.onCheck = cancel
	receipt, err := d.controllers.Propose(callCtx, sealed.SessionID, request.ID, selectionInput(t, sealed, "worker-type", "stable-key"))
	if !errors.Is(err, context.Canceled) || receipt.Proposal.ID == "" {
		t.Fatalf("interruption lost persisted output: %+v %v", receipt, err)
	}
	pending, err := s.ListUnassessedAgentManagerProposals(ctx, "", 8)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending recovery: %+v %v", pending, err)
	}
	if decision, err := d.controllers.Decision(ctx, "project", request.ID, receipt.Proposal.ID); err != nil || decision != nil {
		t.Fatalf("cancelled assessment was committed: %+v %v", decision, err)
	}
	native.onCheck = nil
	// A fresh service/consumer instance uses only retained data, not an in-memory
	// callback or a native restart. The store suite separately verifies DB reopening.
	restarted := New(s)
	restarted.SetCandidateAssessor(registrysvc.NewWithNative(s, native))
	recovery := NewDecisionDispatcher(s, restarted, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := recovery.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	decision, err := restarted.Decision(ctx, "project", request.ID, receipt.Proposal.ID)
	if err != nil || decision == nil || decision.Outcome != "accepted" || n.starts != 1 || len(n.inputs) != 1 {
		t.Fatalf("recovery repeated native input: %+v %v", decision, err)
	}
	checks := native.checks
	if err := recovery.dispatch(ctx); err != nil || native.checks != checks {
		t.Fatal("recovery reassessed committed history")
	}
}

func TestManagerDecisionRefreshesPolicyRaceOnceThenDefers(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		t.Run(fmt.Sprint(continuous), func(t *testing.T) {
			ctx := context.Background()
			d, n, s, native := selectionFixture(t)
			request := inboxRequest(t, s, "project", "race-work", domain.ContextTechnical, "")
			if err := d.dispatch(ctx); err != nil {
				t.Fatal(err)
			}
			sealed := n.inputs[0]
			native.onCheck = func() {
				entry, err := s.GetRegistryEntry(ctx, "worker-type")
				if err != nil {
					t.Fatal(err)
				}
				if !continuous {
					entry.Metadata.Policy.ManagerCanSelect = false
				}
				if _, err := s.UpdateRegistryMetadata(ctx, entry.ID, entry.Metadata, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, ExpectedRevision: entry.Revision, Reason: "Changed concurrent selection policy"}); err != nil {
					t.Fatal(err)
				}
			}
			receipt, err := d.controllers.Propose(ctx, sealed.SessionID, request.ID, selectionInput(t, sealed, "worker-type", "raced"))
			if continuous {
				if err == nil || !strings.Contains(err.Error(), "prerequisites changed") || native.checks != 2 {
					t.Fatalf("unbounded refresh: %+v %v calls=%d", receipt, err, native.checks)
				}
				items, readErr := d.controllers.Decisions(ctx, "project", request.ID)
				if readErr != nil || len(items) != 0 {
					t.Fatal("raced choice persisted")
				}
			} else if err != nil || receipt.Decision == nil || receipt.Decision.Outcome != "rejected" || native.checks != 1 {
				t.Fatalf("revoked selection not refreshed: %+v %v", receipt, err)
			}
		})
	}
}

func TestManagerDecisionClosesSupersededOrCancelledWorkWithoutNativeProbes(t *testing.T) {
	for _, change := range []string{"task", "governance", "cancel"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			d, n, s, native := selectionFixture(t)
			request := inboxRequest(t, s, "project", "changed-work", domain.ContextTechnical, "")
			if err := d.dispatch(ctx); err != nil {
				t.Fatal(err)
			}
			sealed := n.inputs[0]
			d.controllers.SetCandidateAssessor(nil)
			receipt, err := d.controllers.Propose(ctx, sealed.SessionID, request.ID, selectionInput(t, sealed, "worker-type", "pending"))
			if err != nil {
				t.Fatal(err)
			}
			expected := "superseded"
			switch change {
			case "task":
				revision, err := s.GetTaskRevision(ctx, request.TaskID, 1)
				if err != nil {
					t.Fatal(err)
				}
				revision.Definition.Brief = "New scope"
				_, err = s.ReviseAdaptiveTask(ctx, request.TaskID, revision.Definition, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, ExpectedRevision: 1, Reason: "Replan"})
				if err != nil {
					t.Fatal(err)
				}
			case "governance":
				configuration, err := s.GetAgentManager(ctx, "project")
				if err != nil {
					t.Fatal(err)
				}
				configuration.Definition.Enabled = false
				_, err = s.ConfigureAgentManager(ctx, "project", configuration.Definition, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, ExpectedRevision: 1, Reason: "Disable"})
				if err != nil {
					t.Fatal(err)
				}
			case "cancel":
				expected = "cancelled"
				_, err := s.ChangeTaskIntent(ctx, request.TaskID, domain.TaskIntentChange{Intent: "cancel", Mutation: domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, ExpectedRevision: 1, Reason: "Cancel work"}})
				if err != nil {
					t.Fatal(err)
				}
			}
			d.controllers.SetCandidateAssessor(registrysvc.NewWithNative(s, native))
			decision, err := d.controllers.AssessProposal(ctx, "project", request.ID, receipt.Proposal.ID)
			if err != nil || decision != nil || native.checks != 0 {
				t.Fatalf("superseded work assessed: %+v %v", decision, err)
			}
			resolution, err := d.controllers.Resolution(ctx, "project", request.ID)
			if err != nil || resolution == nil || resolution.Outcome != expected {
				t.Fatalf("routing not closed: %+v %v", resolution, err)
			}
		})
	}
}

type blockedProjectCandidates struct {
	base    CandidateAssessor
	blocked map[domain.ProjectID]bool
}

func (a blockedProjectCandidates) AssessManagerCandidate(ctx context.Context, id string, version int64, task domain.TaskDefinition, project domain.ProjectRecord) (domain.AgentManagerCandidate, error) {
	if a.blocked[domain.ProjectID(project.ID)] {
		return domain.AgentManagerCandidate{}, errors.New("temporary independent project failure")
	}
	return a.base.AssessManagerCandidate(ctx, id, version, task, project)
}

func TestManagerDecisionRecoveryCursorPassesBlockedProjects(t *testing.T) {
	ctx := context.Background()
	d, n, s, native := selectionFixture(t)
	d.controllers.SetCandidateAssessor(nil)
	for i := 0; i < 9; i++ {
		project := domain.ProjectID(fmt.Sprintf("fair-%d", i))
		inboxProject(t, s, project)
		inboxRequest(t, s, project, fmt.Sprintf("fair-task-%d", i), domain.ContextTechnical, "")
	}
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	for i, sealed := range n.inputs {
		rec, found, err := s.GetSession(ctx, sealed.SessionID)
		if err != nil || !found {
			t.Fatal("native seed missing")
		}
		input := selectionInput(t, sealed, "worker-type", "key")
		_, _, err = s.SubmitAgentManagerProposal(ctx, domain.AgentManagerProposalSubmission{ID: fmt.Sprintf("fair-proposal-%d", i), ProjectID: rec.ProjectID, RequestID: sealed.RequestID, IdempotencyKey: input.IdempotencyKey, SessionID: rec.ID, SourceOwner: rec.ControllerOwner(), Raw: input.Raw, Now: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.ListUnassessedAgentManagerProposals(ctx, "", 100)
	if err != nil || len(items) != 9 {
		t.Fatalf("pending proposals: %d %v", len(items), err)
	}
	blocked := map[domain.ProjectID]bool{}
	for _, item := range items[:8] {
		blocked[item.ProjectID] = true
	}
	d.controllers.SetCandidateAssessor(blockedProjectCandidates{base: registrysvc.NewWithNative(s, native), blocked: blocked})
	recovery := NewDecisionDispatcher(s, d.controllers, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := recovery.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	cursor, err := s.AgentManagerDecisionCursor(ctx)
	if err != nil || cursor != items[7].ProposalID {
		t.Fatalf("cursor not retained: %s %v", cursor, err)
	}
	restarted := NewDecisionDispatcher(s, d.controllers, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := restarted.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	decision, err := d.controllers.Decision(ctx, items[8].ProjectID, items[8].RequestID, items[8].ProposalID)
	if err != nil || decision == nil || decision.Outcome != "accepted" {
		t.Fatalf("blocked projects starved eligible work: %+v %v", decision, err)
	}
	remaining, err := s.ListUnassessedAgentManagerProposals(ctx, "", 100)
	if err != nil || len(remaining) != 8 {
		t.Fatalf("pending history changed: %d %v", len(remaining), err)
	}
}

func TestManagerProposalFeedbackExcludesUnclassifiedResolutionReason(t *testing.T) {
	ctx := context.Background()
	d, n, s, native := selectionFixture(t)
	request := inboxRequest(t, s, "project", "private-resolution", domain.ContextTechnical, "")
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	sealed := n.inputs[0]
	d.controllers.SetCandidateAssessor(nil)
	input := selectionInput(t, sealed, "worker-type", "pending")
	if _, err := d.controllers.Propose(ctx, sealed.SessionID, request.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := d.controllers.Resolve(ctx, domain.AdaptiveActor{Kind: "USER", ID: "private-human"}, "project", request.ID, ResolveInput{Outcome: "needs_human", Reason: "PRIVATE UNCLASSIFIED RESOLUTION TEXT"}); err != nil {
		t.Fatal(err)
	}
	d.controllers.SetCandidateAssessor(registrysvc.NewWithNative(s, native))
	receipt, err := d.controllers.Propose(ctx, sealed.SessionID, request.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RoutingOutcome != "needs_human" || receipt.Decision != nil || native.checks != 0 || strings.Contains(string(encoded), "PRIVATE UNCLASSIFIED") || strings.Contains(string(encoded), "private-human") {
		t.Fatalf("native response leaked caller material: %s", encoded)
	}
}

func TestManagerAssessmentStopsWhenHumanClosesRoutingDuringNativeCheck(t *testing.T) {
	ctx := context.Background()
	d, n, s, native := selectionFixture(t)
	request := inboxRequest(t, s, "project", "human-closed", domain.ContextTechnical, "")
	if err := d.dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	sealed := n.inputs[0]
	native.onCheck = func() {
		if _, err := d.controllers.Resolve(ctx, domain.AdaptiveActor{Kind: "USER", ID: "human"}, "project", request.ID, ResolveInput{Outcome: "needs_human", Reason: "PRIVATE LIVE RESOLUTION"}); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := d.controllers.Propose(ctx, sealed.SessionID, request.ID, selectionInput(t, sealed, "worker-type", "racing-human"))
	if err != nil || receipt.Decision != nil || receipt.RoutingOutcome != "needs_human" || native.checks != 1 {
		t.Fatalf("assessment ignored terminal intervention: %+v %v", receipt, err)
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "PRIVATE LIVE") {
		t.Fatal("native feedback leaked human intervention text")
	}
}
