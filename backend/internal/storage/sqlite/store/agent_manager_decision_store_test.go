package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
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

func managerDecisionFixture(t *testing.T, s *sqlite.Store, class domain.ContextClass) (domain.AgentManagerProposalSubmission, domain.AgentManagerAssessment) {
	t.Helper()
	ctx := context.Background()
	input := managerNativeFixture(t, s, domain.SessionModeTUI, domain.ContextMission)
	seal := managerContextRequest(t, s, input, "decision-task", class, "client-a")
	delivery, _, err := s.BeginAgentManagerDelivery(ctx, seal, "decision-delivery")
	mustNoError(t, err)
	mustNoError(t, s.ResolveAgentManagerDelivery(ctx, domain.AgentManagerDeliveryResolution{ID: delivery.ID, State: "handed_off", Reason: "Native accepted exact task"}))
	input.RequestID, input.Now = seal.RequestID, time.Now().UTC()
	_, err = s.CreateRegistryEntry(ctx, "decision-skill", domain.RegistrySkill, registryMetadata("Tests"), domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Test changes", Capabilities: []string{"tests"}}}, registryMutation(domain.RegistryUser, 0))
	mustNoError(t, err)
	_, err = s.CreateProviderBinding(ctx, domain.ProviderBinding{ID: "decision-binding", Name: "Existing native source", Harness: domain.HarnessCodex, ProjectID: "project", Enabled: true}, registryMutation(domain.RegistryUser, 0))
	mustNoError(t, err)
	definition := registryAgentDefinition(domain.SkillVersionRef{ID: "decision-skill", Version: 1})
	definition.AgentType.ProviderBindingID = "decision-binding"
	_, err = s.CreateRegistryEntry(ctx, "unvalidated-type", domain.RegistryAgentType, registryMetadata("Candidate"), definition, registryMutation(domain.RegistryUser, 0))
	mustNoError(t, err)
	_, _, err = s.SubmitAgentManagerProposal(ctx, input)
	mustNoError(t, err)
	return input, managerTestAssessment(t, s, input)
}

// The store consumes trusted native observations, independently rechecking every
// durable compatibility fact. Native probing is covered at the registry boundary.
func managerTestAssessment(t *testing.T, s *sqlite.Store, input domain.AgentManagerProposalSubmission) domain.AgentManagerAssessment {
	t.Helper()
	ctx := context.Background()
	entry, err := s.GetRegistryEntry(ctx, "unvalidated-type")
	mustNoError(t, err)
	version, err := s.GetRegistryVersion(ctx, entry.ID, 1)
	mustNoError(t, err)
	project, found, err := s.GetProject(ctx, "project")
	mustNoError(t, err)
	if !found {
		t.Fatal("project missing")
	}
	definition := domain.ResolveWorkerOptions(*version.Definition.AgentType, domain.WorkerOverrides{}, project.Config, domain.SessionModeTUI)
	skill, err := s.GetRegistryEntry(ctx, "decision-skill")
	mustNoError(t, err)
	skillVersion, err := s.GetRegistryVersion(ctx, skill.ID, 1)
	mustNoError(t, err)
	_, hash, err := domain.TaskContent(project.Config)
	mustNoError(t, err)
	candidate := domain.AgentManagerCandidate{AgentType: domain.WorkerDefinitionRef{ID: entry.ID, Version: 1, Name: entry.Metadata.Name, ContentHash: version.ContentHash}, MetadataRevision: entry.Revision, MaxContextClass: definition.MaxContextClass.Effective(), Harness: definition.Harness, SessionMode: definition.SessionMode, Config: definition.Config, ProviderBindingID: definition.ProviderBindingID, BindingRevision: 1, Capabilities: []string{"tests"}, Skills: []domain.AgentManagerCandidateSkill{{Reference: domain.WorkerDefinitionRef{ID: skill.ID, Version: 1, Name: skill.Metadata.Name, ContentHash: skillVersion.ContentHash}, MetadataRevision: skill.Revision}}, MissingCapabilities: []string{}, Eligible: true, Issues: []domain.AgentManagerCandidateIssue{}, CatalogFingerprint: "observed-native"}
	return domain.AgentManagerAssessment{ProjectID: input.ProjectID, RequestID: input.RequestID, ProposalID: input.ID, ProjectConfigurationHash: hash, Candidates: []domain.AgentManagerCandidate{candidate}, ObservedAt: time.Now().UTC()}
}

func TestManagerDecisionRacesRetainOneSelectionWithoutLaunching(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, assessment := managerDecisionFixture(t, s, domain.ContextTechnical)
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			decision, created, err := s.RecordAgentManagerDecision(ctx, assessment)
			if err == nil && decision.Outcome != "accepted" {
				err = fmt.Errorf("unexpected outcome %s", decision.Outcome)
			}
			results <- created
			failures <- err
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	count := 0
	for created := range results {
		if created {
			count++
		}
	}
	for err := range failures {
		mustNoError(t, err)
	}
	if count != 1 {
		t.Fatalf("created %d decisions", count)
	}
	decision, found, err := s.GetAgentManagerDecision(ctx, input.ProjectID, input.RequestID, input.ID)
	mustNoError(t, err)
	if !found || decision.Validate() != nil || decision.Optimization != "balanced" || decision.Classification != domain.ContextTechnical || decision.EngagementID != "client-a" {
		t.Fatalf("bad decision: %+v", decision)
	}
	resolution, found, err := s.GetAgentManagerRequestResolution(ctx, input.ProjectID, input.RequestID)
	mustNoError(t, err)
	if !found || resolution.Outcome != "selected" || resolution.Validate() != nil {
		t.Fatalf("selection not atomic: %+v", resolution)
	}
	audit, err := s.ListAgentManagerAudit(ctx, input.ProjectID, 0, 100)
	mustNoError(t, err)
	count = 0
	for _, event := range audit {
		if event.Action == "decision_accepted" || event.Action == "request_selected" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("duplicate semantic audit: %d", count)
	}
	metadata := registryMetadata("Candidate")
	metadata.Enabled = false
	_, err = s.UpdateRegistryMetadata(ctx, "unvalidated-type", metadata, registryMutation(domain.RegistryUser, 1))
	mustNoError(t, err)
	replayed, created, err := s.RecordAgentManagerDecision(ctx, assessment)
	if err != nil || created || replayed.ContentHash != decision.ContentHash {
		t.Fatalf("history changed after disable: %+v %v", replayed, err)
	}
	if _, _, err := s.GetAgentManagerDecision(ctx, "other", input.RequestID, input.ID); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("cross-project decision: %v", err)
	}
	input.ID, input.IdempotencyKey = "second", "second"
	input.Now = time.Now().UTC()
	if _, _, err := s.SubmitAgentManagerProposal(ctx, input); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("selected request reopened: %v", err)
	}
}

func TestManagerDecisionRechecksMutablePolicyAndRejectsForgedFacts(t *testing.T) {
	for _, name := range []string{"type permission", "type disabled", "type metadata", "Skill permission", "Skill disabled", "provider disabled", "provider revision", "task revision", "cancelled task", "governance", "project defaults", "wrong clearance", "forged capability", "missing Skill", "changed native model", "wrong provider", "future observation", "wrong selected version", "cross project"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			input, a := managerDecisionFixture(t, s, domain.ContextTechnical)
			switch name {
			case "type permission", "type disabled", "type metadata", "Skill permission", "Skill disabled":
				id := "unvalidated-type"
				if name == "Skill permission" || name == "Skill disabled" {
					id = "decision-skill"
				}
				entry, err := s.GetRegistryEntry(ctx, id)
				mustNoError(t, err)
				switch name {
				case "type permission", "Skill permission":
					entry.Metadata.Policy.ManagerCanSelect = false
				case "type metadata":
					entry.Metadata.Description = "Changed"
				default:
					entry.Metadata.Enabled = false
				}
				_, err = s.UpdateRegistryMetadata(ctx, id, entry.Metadata, registryMutation(domain.RegistryUser, 1))
				mustNoError(t, err)
			case "provider disabled", "provider revision":
				_, err := s.UpdateProviderBinding(ctx, "decision-binding", "Changed source", name != "provider disabled", registryMutation(domain.RegistryUser, 1))
				mustNoError(t, err)
			case "task revision":
				_, err := s.ReviseAdaptiveTask(ctx, "decision-task", taskDefinition(), taskMutation(1))
				mustNoError(t, err)
			case "cancelled task":
				_, err := s.ChangeTaskIntent(ctx, "decision-task", domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)})
				mustNoError(t, err)
			case "governance":
				configuration, err := s.GetAgentManager(ctx, "project")
				mustNoError(t, err)
				_, err = s.ConfigureAgentManager(ctx, "project", configuration.Definition, taskMutation(1))
				mustNoError(t, err)
			case "project defaults":
				project, _, err := s.GetProject(ctx, "project")
				mustNoError(t, err)
				project.Config.Worker.AgentConfig.Model = "changed"
				mustNoError(t, s.UpsertProject(ctx, project))
			case "wrong clearance":
				a.Candidates[0].MaxContextClass = domain.ContextMission
			case "forged capability":
				a.Candidates[0].Capabilities = append(a.Candidates[0].Capabilities, "invented")
			case "missing Skill":
				a.Candidates[0].Skills = []domain.AgentManagerCandidateSkill{}
			case "changed native model":
				a.Candidates[0].Config.Model = "invented"
			case "wrong provider":
				a.Candidates[0].ProviderBindingID = ""
				a.Candidates[0].BindingRevision = 0
			case "future observation":
				a.ObservedAt = time.Now().Add(time.Hour)
			case "wrong selected version":
				a.Candidates[0].AgentType.Version = 2
			case "cross project":
				a.ProjectID = "other"
			}
			if _, _, err := s.RecordAgentManagerDecision(ctx, a); err == nil {
				t.Fatal("unsafe decision committed")
			}
			items, err := s.ListAgentManagerDecisions(ctx, input.ProjectID, input.RequestID)
			mustNoError(t, err)
			if len(items) != 0 {
				t.Fatal("partial decision")
			}
			if _, found, err := s.GetAgentManagerRequestResolution(ctx, input.ProjectID, input.RequestID); err != nil || found {
				t.Fatalf("partial resolution: %v %v", found, err)
			}
		})
	}
}

func TestManagerDecisionClassGateAndAttributedOutputSurviveNativeExit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, a := managerDecisionFixture(t, s, domain.ContextMission)
	if _, _, err := s.RecordAgentManagerDecision(ctx, a); !errors.Is(err, ports.ErrAgentManagerForbidden) {
		t.Fatalf("mission task selected technical worker: %v", err)
	}
	a.Candidates[0].Eligible = false
	a.Candidates[0].Issues = []domain.AgentManagerCandidateIssue{{Code: "CONTEXT_CLEARANCE_EXCEEDED", State: "invalid"}}
	decision, _, err := s.RecordAgentManagerDecision(ctx, a)
	mustNoError(t, err)
	if decision.Outcome != "rejected" || decision.Classification != domain.ContextMission || decision.EngagementID != "client-a" {
		t.Fatalf("rejection lost attribution: %+v", decision)
	}
	// Output already accepted from a confirmed native owner stays assessable after
	// that owner exits; a fresh process is unnecessary to validate durable output.
	s2 := newTestStore(t)
	input2, a2 := managerDecisionFixture(t, s2, domain.ContextTechnical)
	rec, _, err := s2.GetSession(ctx, input2.SessionID)
	mustNoError(t, err)
	rec.IsTerminated = true
	mustNoError(t, s2.UpdateSession(ctx, rec))
	if decision, _, err := s2.RecordAgentManagerDecision(ctx, a2); err != nil || decision.Outcome != "accepted" {
		t.Fatalf("retained native output lost: %+v %v", decision, err)
	}
	if _, _, err := s.GetAgentManagerDecision(ctx, input.ProjectID, input.RequestID, "unrelated"); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("unrelated receipt exposed: %v", err)
	}
}

func TestManagerDecisionCorrectionsShareParserAndSemanticBudget(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input, a := managerDecisionFixture(t, s, domain.ContextTechnical)
	a.Candidates[0].Eligible = false
	a.Candidates[0].Issues = []domain.AgentManagerCandidateIssue{{Code: "NATIVE_READINESS_UNAVAILABLE", State: "unavailable"}}
	_, _, err := s.RecordAgentManagerDecision(ctx, a)
	mustNoError(t, err)
	input.ID, input.IdempotencyKey, input.Raw = "malformed-correction", "correction-2", "{"
	input.Now = time.Now().UTC()
	proposal, _, err := s.SubmitAgentManagerProposal(ctx, input)
	mustNoError(t, err)
	if proposal.Number != 2 || proposal.Definition != nil {
		t.Fatal("correction did not consume shared budget")
	}
	input.ID, input.IdempotencyKey, input.Raw = "final-correction", "correction-3", managerSelectionProposal
	input.Now = time.Now().UTC()
	proposal, _, err = s.SubmitAgentManagerProposal(ctx, input)
	mustNoError(t, err)
	if proposal.Number != 3 {
		t.Fatal("semantic retry reset budget")
	}
	a.ProposalID, a.ObservedAt = proposal.ID, time.Now().UTC()
	_, _, err = s.RecordAgentManagerDecision(ctx, a)
	mustNoError(t, err)
	resolution, found, err := s.GetAgentManagerRequestResolution(ctx, input.ProjectID, input.RequestID)
	mustNoError(t, err)
	if !found || resolution.Outcome != "needs_human" {
		t.Fatalf("budget not escalated: %+v", resolution)
	}
	input.ID, input.IdempotencyKey = "fourth", "fourth"
	input.Now = time.Now().UTC()
	if _, _, err := s.SubmitAgentManagerProposal(ctx, input); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("correction budget exceeded: %v", err)
	}
	items, err := s.ListAgentManagerDecisions(ctx, input.ProjectID, input.RequestID)
	mustNoError(t, err)
	if len(items) != 2 {
		t.Fatal("rejection history lost")
	}
}

func TestManagerDecisionAtomicAuditSQLGuardsAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, a := managerDecisionFixture(t, s, domain.ContextTechnical)
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for _, actor := range []domain.AdaptiveActor{{Kind: "USER", ID: "human"}, {Kind: "SYSTEM", ID: "manager-selector"}} {
		if err := s.ResolveAgentManagerRequest(ctx, input.ProjectID, domain.AgentManagerRequestResolution{RequestID: input.RequestID, Outcome: "selected", Actor: actor, Reason: "Forge selection", CreatedAt: time.Now().UTC()}); !errors.Is(err, ports.ErrAgentManagerForbidden) {
			t.Fatalf("generic resolution forged selection: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO adaptive_agent_manager_request_resolutions VALUES(?,'selected','{"kind":"SYSTEM","id":"manager-selector"}','Forged proof',CURRENT_TIMESTAMP)`, input.RequestID); err == nil {
		t.Fatal("SQL accepted missing decision proof")
	}
	_, err = db.Exec(`CREATE TRIGGER fail_decision_resolution BEFORE INSERT ON adaptive_agent_manager_audit WHEN NEW.action='request_selected' BEGIN SELECT RAISE(ABORT,'injected decision audit failure'); END`)
	mustNoError(t, err)
	var before, after int
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before))
	if _, _, err := s.RecordAgentManagerDecision(ctx, a); err == nil {
		t.Fatal("late audit failure ignored")
	}
	if _, found, err := s.GetAgentManagerDecision(ctx, input.ProjectID, input.RequestID, input.ID); err != nil || found {
		t.Fatal("partial decision survived rollback")
	}
	if _, found, err := s.GetAgentManagerRequestResolution(ctx, input.ProjectID, input.RequestID); err != nil || found {
		t.Fatal("partial resolution survived rollback")
	}
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after))
	if before != after {
		t.Fatal("rolled-back decision emitted CDC")
	}
	_, err = db.Exec(`DROP TRIGGER fail_decision_resolution`)
	mustNoError(t, err)
	decision, _, err := s.RecordAgentManagerDecision(ctx, a)
	mustNoError(t, err)
	for _, statement := range []string{`UPDATE adaptive_agent_manager_decisions SET outcome='rejected'`, `DELETE FROM adaptive_agent_manager_decisions`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("mutable decision history: %s", statement)
		}
	}
	var count int
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM adaptive_task_leases`).Scan(&count))
	if count != 0 {
		t.Fatal("selection allocated a worker lease")
	}
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM sessions WHERE kind='worker'`).Scan(&count))
	if count != 0 {
		t.Fatal("selection launched a worker")
	}
	mustNoError(t, s.Close())
	s, err = sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	retained, found, err := s.GetAgentManagerDecision(ctx, input.ProjectID, input.RequestID, input.ID)
	if err != nil || !found || retained.ContentHash != decision.ContentHash {
		t.Fatalf("restart lost decision: %+v %v", retained, err)
	}
	var integrity string
	mustNoError(t, db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity))
	if integrity != "ok" {
		t.Fatal(integrity)
	}
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&count))
	if count != 0 {
		t.Fatal("foreign key violation")
	}
}

func TestManagerDecisionCannotApplyLegacyUnattributedProposal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, a := managerDecisionFixture(t, s, domain.ContextTechnical)
	proposal, err := s.GetAgentManagerProposal(ctx, input.ProjectID, input.RequestID, input.ID)
	mustNoError(t, err)
	proposal.ContextID, proposal.ContextHash, proposal.ConversationContextHash, proposal.Classification, proposal.EngagementID = "", "", "", "", ""
	proposal.ContentHash = proposal.Hash()
	mustNoError(t, proposal.Validate())
	encoded, err := json.Marshal(proposal)
	mustNoError(t, err)
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// Install an authentic pre-classification snapshot as an upgrade fixture,
	// then restore immutability before invoking any production boundary.
	_, err = db.Exec(`DROP TRIGGER adaptive_agent_manager_proposal_immutable`)
	mustNoError(t, err)
	_, err = db.Exec(`UPDATE adaptive_agent_manager_proposals SET snapshot=?,content_hash=? WHERE id=?`, string(encoded), proposal.ContentHash, proposal.ID)
	mustNoError(t, err)
	_, err = db.Exec(`CREATE TRIGGER adaptive_agent_manager_proposal_immutable BEFORE UPDATE ON adaptive_agent_manager_proposals BEGIN SELECT RAISE(ABORT,'Manager proposal is immutable'); END`)
	mustNoError(t, err)
	if _, _, err := s.RecordAgentManagerDecision(ctx, a); !errors.Is(err, ports.ErrAgentManagerInvalid) {
		t.Fatalf("legacy output invented an input receipt: %v", err)
	}
	retained, err := s.GetAgentManagerProposal(ctx, input.ProjectID, input.RequestID, input.ID)
	if err != nil || retained.ContentHash != proposal.ContentHash {
		t.Fatalf("legacy history lost: %+v %v", retained, err)
	}
}
