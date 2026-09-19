package agentmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type registryServiceStore struct {
	ports.AgentManagerStore
	ports.AgentManagerInboxStore
	ports.AgentManagerRegistryStore
	rec               domain.SessionRecord
	found             bool
	readErr, writeErr error
	captured          *domain.AgentManagerRegistrySubmission
}

func (s *registryServiceStore) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return s.rec, s.found, s.readErr
}

func (s *registryServiceStore) ApplyAgentManagerRegistryAction(_ context.Context, input domain.AgentManagerRegistrySubmission) (domain.AgentManagerRegistryReceipt, bool, error) {
	s.captured = &input
	return domain.AgentManagerRegistryReceipt{ID: input.ID, Target: domain.WorkerDefinitionRef{ID: input.CreatedEntryID, Version: 1}}, s.writeErr == nil, s.writeErr
}

func (s *registryServiceStore) GetAgentManagerRegistryReceipt(context.Context, domain.ProjectID, string, string) (domain.AgentManagerRegistryReceipt, error) {
	return domain.AgentManagerRegistryReceipt{}, ports.ErrAgentManagerNotFound
}

func (s *registryServiceStore) ListAgentManagerRegistryReceipts(context.Context, domain.ProjectID, string, string, int) ([]domain.AgentManagerRegistryReceipt, error) {
	return nil, nil
}

func (s *registryServiceStore) GetAgentManagerRequest(_ context.Context, _ domain.ProjectID, requestID string) (domain.AgentManagerRequest, error) {
	if requestID != "request" {
		return domain.AgentManagerRequest{}, ports.ErrAgentManagerNotFound
	}
	return domain.AgentManagerRequest{ID: requestID, ProjectID: "project"}, nil
}

func registryServiceAction() domain.AgentManagerRegistryAction {
	return domain.AgentManagerRegistryAction{Action: "create", Kind: domain.RegistrySkill, Name: "Binary Format Tests", Definition: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Verify decoded boundaries", Capabilities: []string{"binary-tests"}}}, Reason: "Existing Skills lack binary format assertions"}
}

func TestNativeManagerRegistryServiceDerivesOwnerAndMintsIdentity(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			store := &registryServiceStore{rec: domain.SessionRecord{ID: "manager", ProjectID: "project", Kind: domain.KindAgentManager, Harness: domain.HarnessCodex, Mode: mode, Metadata: domain.SessionMetadata{RuntimeLaunchID: "generation", ControllerGeneration: "generation"}}, found: true}
			manager := New(store)
			input := RegistryActionInput{SourceGeneration: "generation", IdempotencyKey: "key", Action: registryServiceAction()}
			receipt, err := manager.AuthorRegistryAction(ctx, "manager", "request", input)
			if err != nil || !receipt.Created || store.captured == nil || store.captured.ProjectID != "project" || store.captured.SourceOwner != store.rec.ControllerOwner() || store.captured.RequestID != "request" || store.captured.CreatedEntryID == "" || store.captured.CreatedEntryID == input.Action.EntryID {
				t.Fatalf("native owner or identity not derived: %+v %v", store.captured, err)
			}
			if receipt.Receipt.Target.ID != store.captured.CreatedEntryID {
				t.Fatalf("receipt target does not carry the minted identity: %+v", receipt.Receipt.Target)
			}
			store.captured = nil
			versioned := input
			versioned.Action.Action, versioned.Action.EntryID, versioned.Action.ExpectedRevision, versioned.Action.Name = "append_version", "skill-entry", 2, ""
			if _, err := manager.AuthorRegistryAction(ctx, "manager", "request", versioned); err != nil || store.captured == nil || store.captured.CreatedEntryID != "" {
				t.Fatalf("version authoring must not mint an identity: %+v %v", store.captured, err)
			}
			store.captured = nil
			for _, change := range []func(*RegistryActionInput){
				func(i *RegistryActionInput) { i.SourceGeneration = "stale" },
				func(i *RegistryActionInput) { i.SourceGeneration = "" },
				func(i *RegistryActionInput) { i.IdempotencyKey = "" },
				func(i *RegistryActionInput) { i.Action.Reason = "" },
				func(i *RegistryActionInput) { i.Action.Kind = "workspace" },
				func(i *RegistryActionInput) { i.Action.Action = "disable" },
			} {
				bad := input
				change(&bad)
				if _, err := manager.AuthorRegistryAction(ctx, "manager", "request", bad); err == nil || store.captured != nil {
					t.Fatalf("invalid native claim reached store: %v", err)
				}
			}
			for _, name := range []string{strings.Repeat("x", 201), "bad\x00name"} {
				bad := input
				bad.Action.Name = name
				if _, err := manager.AuthorRegistryAction(ctx, "manager", "request", bad); err == nil || store.captured != nil {
					t.Fatalf("unbounded native content reached store: %q", name)
				}
			}
			bad := input
			bad.Action.Definition = domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: strings.Repeat("x", (256<<10)+1), Capabilities: []string{"binary-tests"}}}
			if _, err := manager.AuthorRegistryAction(ctx, "manager", "request", bad); err == nil || store.captured != nil {
				t.Fatal("oversized definition reached store")
			}
			store.rec.Kind = domain.KindWorker
			if _, err := manager.AuthorRegistryAction(ctx, "manager", "request", input); err == nil || store.captured != nil {
				t.Fatal("worker impersonated Manager")
			}
			store.rec.Kind, store.rec.IsTerminated = domain.KindAgentManager, true
			if _, err := manager.AuthorRegistryAction(ctx, "manager", "request", input); err == nil || store.captured != nil {
				t.Fatal("terminated Manager authored")
			}
			store.rec.IsTerminated = false
			failure := errors.New("native registry database unavailable")
			store.readErr = failure
			if _, err := manager.AuthorRegistryAction(ctx, "manager", "request", input); !errors.Is(err, failure) {
				t.Fatalf("read failure hidden: %v", err)
			}
			store.readErr, store.writeErr = nil, failure
			if _, err := manager.AuthorRegistryAction(ctx, "manager", "request", input); !errors.Is(err, failure) {
				t.Fatalf("write failure hidden: %v", err)
			}
		})
	}
}

func TestRegistryHistoryRequiresRequestAndBoundsPages(t *testing.T) {
	ctx := context.Background()
	store := &registryServiceStore{rec: domain.SessionRecord{ID: "manager", ProjectID: "project", Kind: domain.KindAgentManager}, found: true}
	manager := New(store)
	if _, err := manager.RegistryReceipts(ctx, "project", "request", "", 20); err != nil {
		t.Fatalf("bounded page refused: %v", err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := manager.RegistryReceipts(ctx, "project", "request", "", limit); err == nil {
			t.Fatalf("invalid page limit accepted: %d", limit)
		}
	}
	if _, err := manager.RegistryReceipts(ctx, "project", "request", "bad\ncursor", 20); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	if _, err := manager.RegistryReceipt(ctx, "project", "request", strings.Repeat("x", 201)); err == nil {
		t.Fatal("invalid receipt id accepted")
	}
	if _, err := manager.RegistryReceipts(ctx, "project", "unknown-request", "", 20); err == nil {
		t.Fatal("unknown request accepted")
	}
	if _, err := manager.RegistryReceipt(ctx, "project", "unknown-request", "receipt"); err == nil {
		t.Fatal("unknown request accepted for exact read")
	}
}
