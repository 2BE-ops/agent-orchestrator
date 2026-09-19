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

func managerAuthoringPolicy() domain.AgentManagerPolicy {
	p := domain.DefaultAgentManagerPolicy()
	p.AllowCreateTypes, p.AllowCreateSkills, p.AllowCreateVersions = true, true, true
	return p
}

func managerRegistryFixture(t *testing.T, s *sqlite.Store, mode domain.SessionMode, class domain.ContextClass, policy domain.AgentManagerPolicy) domain.AgentManagerRegistrySubmission {
	t.Helper()
	ctx := context.Background()
	reservation, seed, snapshot := managerControllerFixture(t, s, domain.ContextMission)
	configuration, err := s.GetAgentManager(ctx, "project")
	mustNoError(t, err)
	configuration.Definition.Policy = policy
	_, err = s.ConfigureAgentManager(ctx, "project", configuration.Definition, taskMutation(1))
	mustNoError(t, err)
	reservation.ConfigurationVersion = 2
	reservation.Now = time.Now().UTC()
	if mode == domain.SessionModeChat {
		seed.Mode, snapshot.Effective.SessionMode = mode, mode
		snapshot.ContentHash = snapshot.Hash()
	}
	controller, _, err := s.ReserveAgentManagerController(ctx, reservation)
	mustNoError(t, err)
	seed, _, err = s.CreateAgentManagerSession(ctx, controller.AgentManagerControllerToken, seed, snapshot, time.Now().UTC())
	mustNoError(t, err)
	op := domain.AgentManagerExecutionOperation{ID: "author-generation", ControllerID: controller.ID, SessionID: seed.ID, SourceOwner: seed.ControllerOwner(), Kind: "dispatch", CreatedAt: time.Now().UTC()}
	_, err = s.BeginAgentManagerExecution(ctx, op)
	mustNoError(t, err)
	if mode == domain.SessionModeChat {
		seed.Metadata.ControllerGeneration = op.ID
	} else {
		seed.Metadata.RuntimeLaunchID = op.ID
	}
	mustNoError(t, s.UpdateSession(ctx, seed))
	mustNoError(t, s.ResolveAgentManagerExecution(ctx, domain.AgentManagerExecutionResolution{OperationID: op.ID, ObservedOwner: seed.ControllerOwner(), Outcome: "connected", Reason: "Observed native connection", CreatedAt: time.Now().UTC()}))
	definition := taskDefinition()
	definition.Classification, definition.EngagementID = class, "client-a"
	createTask(t, s, "author-task", definition)
	_, _, err = s.EnqueueAgentManagerRequest(ctx, domain.AgentManagerEnqueue{ID: "author-request", ProjectID: "project", TaskID: "author-task", TaskRevision: 1, ConfigurationVersion: 2, Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Route work", Now: time.Now().UTC()})
	mustNoError(t, err)
	delivery, _, err := s.BeginAgentManagerDelivery(ctx, domain.AgentManagerContextSeal{ID: "author-context", ProjectID: "project", RequestID: "author-request", SessionID: seed.ID, SourceOwner: seed.ControllerOwner(), Now: time.Now().UTC()}, "author-delivery")
	mustNoError(t, err)
	mustNoError(t, s.ResolveAgentManagerDelivery(ctx, domain.AgentManagerDeliveryResolution{ID: delivery.ID, State: "handed_off", Reason: "Native accepted context"}))
	return domain.AgentManagerRegistrySubmission{ID: "author-receipt", CreatedEntryID: "authored-skill", ProjectID: "project", RequestID: "author-request", SessionID: seed.ID, SourceOwner: seed.ControllerOwner(), IdempotencyKey: "author-key", Now: time.Now().UTC(), Action: domain.AgentManagerRegistryAction{Action: "create", Kind: domain.RegistrySkill, Name: "Binary Format Tests", Definition: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Verify decoded boundaries", Capabilities: []string{"binary-tests"}}}, Reason: "Existing Skills lack binary format assertions"}}
}

func TestManagerRegistryRacesCreateOnceAndRetainNativeAttribution(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			input := managerRegistryFixture(t, s, mode, domain.ContextTechnical, managerAuthoringPolicy())
			var wg sync.WaitGroup
			results := make(chan bool, 8)
			failures := make(chan error, 8)
			for i := range 8 {
				wg.Go(func() {
					submission := input
					submission.ID, submission.CreatedEntryID = fmt.Sprintf("receipt-%d", i), fmt.Sprintf("skill-%d", i)
					_, created, err := s.ApplyAgentManagerRegistryAction(ctx, submission)
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
				t.Fatalf("created %d duplicate definitions", count)
			}
			items, err := s.ListAgentManagerRegistryReceipts(ctx, input.ProjectID, input.RequestID, "", 100)
			mustNoError(t, err)
			if len(items) != 1 || items[0].Validate() != nil {
				t.Fatalf("bad receipt: %+v", items)
			}
			r := items[0]
			entry, err := s.GetRegistryEntry(ctx, r.Target.ID)
			mustNoError(t, err)
			version, err := s.GetRegistryVersion(ctx, r.Target.ID, r.Target.Version)
			mustNoError(t, err)
			if entry.Origin != domain.RegistryManager || entry.CreatedBy != "controller" || version.Actor.Origin != domain.RegistryManager || version.Actor.ID != "controller" || version.ContentHash != r.Target.ContentHash || r.ContextID != "author-context" || r.Classification != domain.ContextTechnical || r.EngagementID != "client-a" || !entry.Metadata.Enabled || !entry.Metadata.Policy.ManagerCanSelect {
				t.Fatalf("lost attributed reusable definition: %+v %+v %+v", entry, version, r)
			}
			// Historical retry returns the first receipt after governance changes,
			// without restoring disabled definitions or charging another quota.
			entry.Metadata.Enabled = false
			_, err = s.UpdateRegistryMetadata(ctx, entry.ID, entry.Metadata, registryMutation(domain.RegistryUser, entry.Revision))
			mustNoError(t, err)
			config, err := s.GetAgentManager(ctx, input.ProjectID)
			mustNoError(t, err)
			config.Definition.Enabled = false
			_, err = s.ConfigureAgentManager(ctx, input.ProjectID, config.Definition, taskMutation(config.Number))
			mustNoError(t, err)
			replay, created, err := s.ApplyAgentManagerRegistryAction(ctx, input)
			if err != nil || created || replay.ContentHash != r.ContentHash {
				t.Fatalf("retry rewrote receipt: %+v %v", replay, err)
			}
			input.Action.Reason = "Changed payload with same key"
			if _, _, err := s.ApplyAgentManagerRegistryAction(ctx, input); !errors.Is(err, ports.ErrAgentManagerConflict) {
				t.Fatalf("key accepted changed content: %v", err)
			}
			if _, err := s.GetAgentManagerRegistryReceipt(ctx, "other", input.RequestID, r.ID); !errors.Is(err, ports.ErrAgentManagerNotFound) {
				t.Fatalf("cross-project history: %v", err)
			}
			if _, err := s.GetAgentManagerRegistryReceipt(ctx, input.ProjectID, "other", r.ID); !errors.Is(err, ports.ErrAgentManagerNotFound) {
				t.Fatalf("cross-request history: %v", err)
			}
			if _, found, err := s.GetAgentManagerRequestResolution(ctx, input.ProjectID, input.RequestID); err != nil || found {
				t.Fatalf("authoring claimed routing completion: %v %v", found, err)
			}
			sessions, err := s.ListAllSessions(ctx)
			if err != nil || len(sessions) != 1 {
				t.Fatalf("authoring spawned workers: %d %v", len(sessions), err)
			}
		})
	}
}

func TestManagerRegistryComposesPinnedSkillsAndVersionsWithoutPromotion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	input := managerRegistryFixture(t, s, domain.SessionModeTUI, domain.ContextTechnical, managerAuthoringPolicy())
	skill, _, err := s.ApplyAgentManagerRegistryAction(ctx, input)
	mustNoError(t, err)
	input.ID, input.IdempotencyKey, input.CreatedEntryID = "type-receipt", "type-key", "authored-type"
	input.Action = domain.AgentManagerRegistryAction{Action: "create", Kind: domain.RegistryAgentType, Name: "Binary Format Specialist", Definition: registryAgentDefinition(domain.SkillVersionRef{ID: skill.Target.ID, Version: 1}), Reason: "Compose native coder with exact assertion Skill"}
	typeReceipt, _, err := s.ApplyAgentManagerRegistryAction(ctx, input)
	mustNoError(t, err)
	input.ID, input.IdempotencyKey, input.CreatedEntryID = "version-receipt", "version-key", ""
	input.Action.Action, input.Action.EntryID, input.Action.ExpectedRevision = "append_version", typeReceipt.Target.ID, 1
	input.Action.Name = ""
	input.Action.Definition.AgentType.Instructions = "Investigate binary data with bounded samples"
	versionReceipt, _, err := s.ApplyAgentManagerRegistryAction(ctx, input)
	mustNoError(t, err)
	entry, err := s.GetRegistryEntry(ctx, typeReceipt.Target.ID)
	mustNoError(t, err)
	v1, err := s.GetRegistryVersion(ctx, entry.ID, 1)
	mustNoError(t, err)
	v2, err := s.GetRegistryVersion(ctx, entry.ID, 2)
	mustNoError(t, err)
	if entry.ActiveVersion != 1 || entry.Revision != 2 || versionReceipt.Target.Version != 2 || v2.ParentVersion != 1 || v1.ContentHash != typeReceipt.Target.ContentHash || v2.Definition.AgentType.Skills[0].ID != skill.Target.ID {
		t.Fatalf("composition lost pins or promoted: %+v %+v %+v", entry, v1, v2)
	}
	page, err := s.ListAgentManagerRegistryReceipts(ctx, input.ProjectID, input.RequestID, "", 2)
	if err != nil || len(page) != 2 {
		t.Fatalf("receipt page: %+v %v", page, err)
	}
	tail, err := s.ListAgentManagerRegistryReceipts(ctx, input.ProjectID, input.RequestID, page[1].ID, 2)
	if err != nil || len(tail) != 1 || tail[0].ID <= page[1].ID {
		t.Fatalf("receipt cursor: %+v %v", tail, err)
	}
}

func TestManagerRegistryOwnershipAndPinsRemainIndependent(t *testing.T) {
	for _, name := range []string{"version only", "select only", "modify only", "disabled", "stale revision", "wrong kind", "Skill protected", "Skill disabled", "Skill missing version"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			input := managerRegistryFixture(t, s, domain.SessionModeTUI, domain.ContextTechnical, managerAuthoringPolicy())
			metadata := registryMetadata("User owned type")
			metadata.Policy = domain.RegistryPolicy{ManagerCanVersion: true}
			switch name {
			case "select only":
				metadata.Policy = domain.RegistryPolicy{ManagerCanSelect: true}
			case "modify only":
				metadata.Policy = domain.RegistryPolicy{ManagerCanModify: true}
			case "disabled":
				metadata.Enabled = false
			}
			_, err := s.CreateRegistryEntry(ctx, "user-type", domain.RegistryAgentType, metadata, registryAgentDefinition(), registryMutation(domain.RegistryUser, 0))
			mustNoError(t, err)
			input.CreatedEntryID = ""
			input.Action = domain.AgentManagerRegistryAction{Action: "append_version", Kind: domain.RegistryAgentType, EntryID: "user-type", ExpectedRevision: 1, Definition: registryAgentDefinition(), Reason: "Version permitted configuration"}
			if name == "stale revision" {
				input.Action.ExpectedRevision = 2
			}
			if name == "wrong kind" {
				input.Action.Kind = domain.RegistrySkill
				input.Action.Definition = domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Wrong kind"}}
			}
			if name == "Skill protected" || name == "Skill disabled" || name == "Skill missing version" {
				skillMeta := registryMetadata("Skill")
				skillMeta.Policy.ManagerCanSelect = name != "Skill protected"
				skillMeta.Enabled = name != "Skill disabled"
				_, err := s.CreateRegistryEntry(ctx, "pinned-skill", domain.RegistrySkill, skillMeta, domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Skill instructions"}}, registryMutation(domain.RegistryUser, 0))
				mustNoError(t, err)
				version := int64(1)
				if name == "Skill missing version" {
					version = 2
				}
				input.Action.Definition.AgentType.Skills = []domain.SkillVersionRef{{ID: "pinned-skill", Version: version}}
			}
			_, created, err := s.ApplyAgentManagerRegistryAction(ctx, input)
			if name == "version only" {
				if err != nil || !created {
					t.Fatalf("version incorrectly requires modify/select: %v", err)
				}
				entry, err := s.GetRegistryEntry(ctx, "user-type")
				mustNoError(t, err)
				if entry.Metadata.Policy != metadata.Policy || entry.Origin != domain.RegistryUser || entry.ActiveVersion != 1 {
					t.Fatal("Manager rewrote ownership or promoted version")
				}
				return
			}
			if err == nil || created {
				t.Fatalf("prohibited authoring applied: %v", err)
			}
			versions, err := s.ListRegistryVersions(ctx, "user-type", 0, 100)
			if err != nil || len(versions) != 1 {
				t.Fatalf("partial version after refusal: %+v %v", versions, err)
			}
		})
	}
}

func TestManagerRegistryEnforcesPolicyAndConcurrentLifetimeQuotas(t *testing.T) {
	for _, kind := range []domain.RegistryKind{domain.RegistryAgentType, domain.RegistrySkill} {
		t.Run(string(kind), func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			policy := managerAuthoringPolicy()
			policy.MaxCreatedTypes, policy.MaxCreatedSkills = 2, 2
			input := managerRegistryFixture(t, s, domain.SessionModeTUI, domain.ContextTechnical, policy)
			if kind == domain.RegistryAgentType {
				input.Action.Kind, input.Action.Definition = kind, registryAgentDefinition()
			}
			var wg sync.WaitGroup
			results := make(chan error, 8)
			for i := range 8 {
				wg.Go(func() {
					submission := input
					submission.ID, submission.IdempotencyKey, submission.CreatedEntryID = fmt.Sprintf("receipt-%d", i), fmt.Sprintf("key-%d", i), fmt.Sprintf("entry-%d", i)
					_, _, err := s.ApplyAgentManagerRegistryAction(ctx, submission)
					results <- err
				})
			}
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				} else if !errors.Is(err, ports.ErrAgentManagerForbidden) {
					t.Fatal(err)
				}
			}
			if success != 2 {
				t.Fatalf("quota admitted %d creations", success)
			}
			items, err := s.ListAgentManagerRegistryReceipts(ctx, input.ProjectID, input.RequestID, "", 100)
			mustNoError(t, err)
			entry, err := s.GetRegistryEntry(ctx, items[0].Target.ID)
			mustNoError(t, err)
			entry.Metadata.Enabled = false
			_, err = s.UpdateRegistryMetadata(ctx, entry.ID, entry.Metadata, registryMutation(domain.RegistryUser, 1))
			mustNoError(t, err)
			if _, _, err := s.ApplyAgentManagerRegistryAction(ctx, input); !errors.Is(err, ports.ErrAgentManagerForbidden) {
				t.Fatalf("disable reset creation quota: %v", err)
			}
		})
	}
	for _, name := range []string{"types", "Skills", "versions", "version limit"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			policy := managerAuthoringPolicy()
			switch name {
			case "types":
				policy.AllowCreateTypes = false
			case "Skills":
				policy.AllowCreateSkills = false
			case "versions":
				policy.AllowCreateVersions = false
			case "version limit":
				policy.MaxVersionsPerEntry = 1
			}
			input := managerRegistryFixture(t, s, domain.SessionModeTUI, domain.ContextTechnical, policy)
			if name == "types" {
				input.Action.Kind, input.Action.Definition = domain.RegistryAgentType, registryAgentDefinition()
			}
			if name == "versions" || name == "version limit" {
				r, _, err := s.ApplyAgentManagerRegistryAction(ctx, input)
				mustNoError(t, err)
				input.CreatedEntryID, input.ID, input.IdempotencyKey = "", "append", "append"
				input.Action.Action, input.Action.Name, input.Action.EntryID, input.Action.ExpectedRevision = "append_version", "", r.Target.ID, 1
				if name == "version limit" {
					// Historical internal Manager writes count even without a native
					// action receipt; a different project cannot reset this limit.
					_, err = s.AppendRegistryVersion(ctx, r.Target.ID, input.Action.Definition, registryMutation(domain.RegistryManager, 1))
					mustNoError(t, err)
					input.Action.ExpectedRevision = 2
				}
			}
			if _, created, err := s.ApplyAgentManagerRegistryAction(ctx, input); !errors.Is(err, ports.ErrAgentManagerForbidden) || created {
				t.Fatalf("policy bypass: %v %v", created, err)
			}
		})
	}
}

func TestManagerRegistryFencesNativeChangesAndClassifiedConversations(t *testing.T) {
	for _, name := range []string{"engagement", "mission", "cumulative mission", "governance", "task revision", "cancelled task", "request resolved", "wrong generation", "wrong mode", "terminated", "cross project", "native operation"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			class := domain.ContextTechnical
			if name == "engagement" || name == "mission" {
				class = domain.ContextClass(name)
			}
			input := managerRegistryFixture(t, s, domain.SessionModeTUI, class, managerAuthoringPolicy())
			switch name {
			case "governance":
				config, err := s.GetAgentManager(ctx, input.ProjectID)
				mustNoError(t, err)
				_, err = s.ConfigureAgentManager(ctx, input.ProjectID, config.Definition, taskMutation(config.Number))
				mustNoError(t, err)
			case "task revision":
				_, err := s.ReviseAdaptiveTask(ctx, "author-task", taskDefinition(), taskMutation(1))
				mustNoError(t, err)
			case "cancelled task":
				_, err := s.ChangeTaskIntent(ctx, "author-task", domain.TaskIntentChange{Intent: "cancel", Mutation: taskMutation(1)})
				mustNoError(t, err)
			case "request resolved":
				err := s.ResolveAgentManagerRequest(ctx, input.ProjectID, domain.AgentManagerRequestResolution{RequestID: input.RequestID, Outcome: "needs_human", Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Human took control", CreatedAt: time.Now().UTC()})
				mustNoError(t, err)
			case "wrong generation":
				input.SourceOwner.RuntimeLaunchID = "other"
			case "wrong mode":
				input.SourceOwner.Mode = domain.SessionModeChat
				input.SourceOwner.ControllerGeneration = input.SourceOwner.RuntimeLaunchID
			case "terminated":
				rec, _, err := s.GetSession(ctx, input.SessionID)
				mustNoError(t, err)
				rec.IsTerminated = true
				mustNoError(t, s.UpdateSession(ctx, rec))
			case "cross project":
				input.ProjectID = "other"
			case "native operation":
				_, err := s.BeginAgentManagerExecution(ctx, domain.AgentManagerExecutionOperation{ID: "pending-restore", ControllerID: "controller", SessionID: input.SessionID, SourceOwner: input.SourceOwner, Kind: "restore", CreatedAt: time.Now().UTC()})
				mustNoError(t, err)
			case "cumulative mission":
				definition := taskDefinition()
				definition.Classification = domain.ContextMission
				createTask(t, s, "later-task", definition)
				_, _, err := s.EnqueueAgentManagerRequest(ctx, domain.AgentManagerEnqueue{ID: "later-request", ProjectID: "project", TaskID: "later-task", TaskRevision: 1, ConfigurationVersion: 2, Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Private later work", Now: time.Now().UTC()})
				mustNoError(t, err)
				_, _, err = s.SealAgentManagerContext(ctx, domain.AgentManagerContextSeal{ID: "later-context", ProjectID: "project", RequestID: "later-request", SessionID: input.SessionID, SourceOwner: input.SourceOwner, Now: time.Now().UTC()})
				mustNoError(t, err)
				input.Now = time.Now().UTC()
			}
			if _, created, err := s.ApplyAgentManagerRegistryAction(ctx, input); err == nil || created {
				t.Fatalf("unsafe reusable content written: %v", err)
			}
			if _, err := s.GetRegistryEntry(ctx, input.CreatedEntryID); !errors.Is(err, ports.ErrRegistryNotFound) {
				t.Fatalf("refusal left registry identity: %v", err)
			}
			items, err := s.ListAgentManagerRegistryReceipts(ctx, "project", input.RequestID, "", 100)
			if err != nil || len(items) != 0 {
				t.Fatalf("refusal charged quota: %d %v", len(items), err)
			}
		})
	}
}

func TestManagerRegistryRequiresDeliveredInputAndBoundsRequestWork(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	policy := managerAuthoringPolicy()
	policy.MaxCreatedSkills = 64
	input := managerRegistryFixture(t, s, domain.SessionModeTUI, domain.ContextTechnical, policy)
	// A native controller with no received request may not author definitions.
	unseeded := newTestStore(t)
	native := managerNativeFixture(t, unseeded, domain.SessionModeTUI)
	missing := input
	missing.ProjectID, missing.RequestID, missing.SessionID, missing.SourceOwner, missing.Now = native.ProjectID, native.RequestID, native.SessionID, native.SourceOwner, time.Now().UTC()
	if _, _, err := unseeded.ApplyAgentManagerRegistryAction(ctx, missing); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("authoring without received input: %v", err)
	}
	for i := range 64 {
		submission := input
		submission.ID, submission.IdempotencyKey, submission.CreatedEntryID = fmt.Sprintf("receipt-%02d", i), fmt.Sprintf("key-%02d", i), fmt.Sprintf("skill-%02d", i)
		_, _, err := s.ApplyAgentManagerRegistryAction(ctx, submission)
		mustNoError(t, err)
	}
	// The type quota is still unused; the cross-kind request bound must refuse.
	input.Action.Kind, input.Action.Definition = domain.RegistryAgentType, registryAgentDefinition()
	if _, _, err := s.ApplyAgentManagerRegistryAction(ctx, input); !errors.Is(err, ports.ErrAgentManagerForbidden) {
		t.Fatalf("unbounded request authoring: %v", err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := s.ListAgentManagerRegistryReceipts(ctx, input.ProjectID, input.RequestID, "", limit); !errors.Is(err, ports.ErrAgentManagerInvalid) {
			t.Fatalf("invalid page limit: %v", err)
		}
	}
	if _, err := s.ListAgentManagerRegistryReceipts(ctx, input.ProjectID, input.RequestID, "bad\ncursor", 20); !errors.Is(err, ports.ErrAgentManagerInvalid) {
		t.Fatalf("invalid page cursor: %v", err)
	}
}

func TestManagerRegistryAuditFailureRollsBackRegistryReceiptQuotaAndCDC(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input := managerRegistryFixture(t, s, domain.SessionModeTUI, domain.ContextTechnical, managerAuthoringPolicy())
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TRIGGER reject_manager_registry_audit BEFORE INSERT ON adaptive_agent_manager_audit WHEN NEW.action='registry_create' BEGIN SELECT RAISE(ABORT,'injected late audit failure'); END`)
	mustNoError(t, err)
	var before, after int
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before))
	if _, created, err := s.ApplyAgentManagerRegistryAction(ctx, input); err == nil || created {
		t.Fatal("late failure was ignored")
	}
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after))
	if before != after {
		t.Fatal("failed transaction leaked CDC")
	}
	if _, err := s.GetRegistryEntry(ctx, input.CreatedEntryID); !errors.Is(err, ports.ErrRegistryNotFound) {
		t.Fatalf("failed transaction leaked registry data: %v", err)
	}
	items, err := s.ListAgentManagerRegistryReceipts(ctx, input.ProjectID, input.RequestID, "", 100)
	if err != nil || len(items) != 0 {
		t.Fatalf("failed transaction charged quota: %d %v", len(items), err)
	}
	_, err = db.Exec(`DROP TRIGGER reject_manager_registry_audit`)
	mustNoError(t, err)
	r, _, err := s.ApplyAgentManagerRegistryAction(ctx, input)
	mustNoError(t, err)
	for _, query := range []string{`UPDATE adaptive_agent_manager_registry_actions SET content_hash=printf('%064d',0)`, `DELETE FROM adaptive_agent_manager_registry_actions`} {
		if _, err := db.Exec(query); err == nil {
			t.Fatal("receipt history was mutable")
		}
	}
	// Confirm restart preserves exact receipt and suppresses duplicate creation.
	mustNoError(t, s.Close())
	restarted, err := sqlite.Open(dir)
	mustNoError(t, err)
	defer restarted.Close()
	replay, created, err := restarted.ApplyAgentManagerRegistryAction(ctx, input)
	if err != nil || created || replay.ContentHash != r.ContentHash {
		t.Fatalf("restart repeated creation: %+v %v", replay, err)
	}
}
