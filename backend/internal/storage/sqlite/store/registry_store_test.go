package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func registryMutation(origin domain.RegistryOrigin, revision int64) domain.RegistryMutation {
	return domain.RegistryMutation{Actor: domain.RegistryActor{Origin: origin, ID: "test-actor"}, ExpectedRevision: revision, Reason: "test change"}
}

func registryMetadata(name string) domain.RegistryMetadata {
	return domain.RegistryMetadata{Name: name, Enabled: true, Policy: domain.RegistryPolicy{ManagerCanSelect: true}}
}

func registryAgentDefinition(skills ...domain.SkillVersionRef) domain.RegistryDefinition {
	return domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, MaxParallelWorkers: 3, Instructions: "Check work", Skills: skills}}
}

func TestRegistryBatchFailureRollsBackDependenciesAndAudit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	inputs := []domain.RegistryCreate{
		{ID: "imported-skill", Kind: domain.RegistrySkill, Metadata: domain.RegistryMetadata{Name: "Imported"}, Definition: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Review"}}},
		{ID: "imported-type", Kind: domain.RegistryAgentType, Metadata: domain.RegistryMetadata{Name: "Type"}, Definition: registryAgentDefinition(domain.SkillVersionRef{ID: "imported-skill", Version: 1}, domain.SkillVersionRef{ID: "missing", Version: 1})},
	}
	if _, err := s.CreateRegistryEntries(ctx, inputs, registryMutation(domain.RegistryUser, 0)); !errors.Is(err, ports.ErrRegistryNotFound) {
		t.Fatalf("expected missing pin: %v", err)
	}
	for _, id := range []string{"imported-skill", "imported-type"} {
		if _, err := s.GetRegistryEntry(ctx, id); !errors.Is(err, ports.ErrRegistryNotFound) {
			t.Fatalf("partial entry %s: %v", id, err)
		}
		if events, err := s.ListRegistryAudit(ctx, id, 0, 100); err != nil || len(events) != 0 {
			t.Fatalf("partial audit: %v %v", events, err)
		}
	}
	if events, err := s.EventsAfter(ctx, 0, 100); err != nil || len(events) != 0 {
		t.Fatalf("partial batch emitted CDC: %v %v", events, err)
	}
	inputs[1].Definition.AgentType.Skills = inputs[1].Definition.AgentType.Skills[:1]
	entries, err := s.CreateRegistryEntries(ctx, inputs, registryMutation(domain.RegistryUser, 0))
	if err != nil || len(entries) != 2 || entries[0].Metadata.Enabled || entries[1].Metadata.Enabled {
		t.Fatalf("disabled atomic import failed: %v %v", entries, err)
	}
	if _, err := s.CreateRegistryEntry(ctx, "unrelated", domain.RegistryAgentType, registryMetadata("Unrelated"), inputs[1].Definition, registryMutation(domain.RegistryUser, 0)); !errors.Is(err, ports.ErrRegistryInvalid) {
		t.Fatalf("normal creation accepted previously disabled skill: %v", err)
	}
}

func TestRegistryVersionHistoryPinsAndRollback(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	skill := domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Run relevant tests"}}
	if _, err := s.CreateRegistryEntry(ctx, "testing", domain.RegistrySkill, registryMetadata("Testing"), skill, registryMutation(domain.RegistryUser, 0)); err != nil {
		t.Fatal(err)
	}
	definition := registryAgentDefinition(domain.SkillVersionRef{ID: "testing", Version: 1})
	created, err := s.CreateRegistryEntry(ctx, "coder", domain.RegistryAgentType, registryMetadata("Coder"), definition, registryMutation(domain.RegistryUser, 0))
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || created.ActiveVersion != 1 || created.Origin != domain.RegistryUser {
		t.Fatalf("created: %+v", created)
	}
	definition.AgentType.Instructions = "Check work and report evidence"
	version, err := s.AppendRegistryVersion(ctx, "coder", definition, registryMutation(domain.RegistryUser, 1))
	if err != nil {
		t.Fatal(err)
	}
	if version.Number != 2 || version.ParentVersion != 1 {
		t.Fatalf("version: %+v", version)
	}
	entry, err := s.GetRegistryEntry(ctx, "coder")
	if err != nil || entry.ActiveVersion != 1 || entry.Revision != 2 {
		t.Fatalf("append activated version: %+v %v", entry, err)
	}
	entry, err = s.ActivateRegistryVersion(ctx, "coder", 2, registryMutation(domain.RegistryUser, 2))
	if err != nil || entry.ActiveVersion != 2 {
		t.Fatalf("activation: %+v %v", entry, err)
	}
	entry, err = s.ActivateRegistryVersion(ctx, "coder", 1, registryMutation(domain.RegistryUser, 3))
	if err != nil || entry.ActiveVersion != 1 {
		t.Fatalf("rollback: %+v %v", entry, err)
	}
	old, err := s.GetRegistryVersion(ctx, "coder", 1)
	if err != nil || old.Definition.AgentType.Instructions != "Check work" || old.Definition.AgentType.Skills[0].Version != 1 {
		t.Fatalf("old version changed: %+v %v", old, err)
	}
	if old.ContentHash == version.ContentHash {
		t.Fatal("changed content retained old hash")
	}
	skill.Skill.Instructions = "Different instructions"
	if _, err := s.AppendRegistryVersion(ctx, "testing", skill, registryMutation(domain.RegistryUser, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ActivateRegistryVersion(ctx, "testing", 2, registryMutation(domain.RegistryUser, 2)); err != nil {
		t.Fatal(err)
	}
	old, err = s.GetRegistryVersion(ctx, "coder", 1)
	if err != nil || old.Definition.AgentType.Skills[0].Version != 1 {
		t.Fatal("skill pin floated")
	}
	metadata := entry.Metadata
	metadata.Enabled = false
	if _, err := s.UpdateRegistryMetadata(ctx, "coder", metadata, registryMutation(domain.RegistryUser, 4)); err != nil {
		t.Fatal(err)
	}
	versions, err := s.ListRegistryVersions(ctx, "coder", 0, 100)
	if err != nil || len(versions) != 2 {
		t.Fatalf("history: %d %v", len(versions), err)
	}
	audit, err := s.ListRegistryAudit(ctx, "coder", 0, 100)
	if err != nil || len(audit) != 5 {
		t.Fatalf("audit: %d %v", len(audit), err)
	}
	for i, item := range audit {
		if item.Revision != int64(i+1) {
			t.Fatal("audit revision gap")
		}
	}
	if audit[2].VersionNumber != 2 || audit[3].VersionNumber != 1 {
		t.Fatal("activation and rollback targets missing from audit")
	}
	events, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(events) != 8 {
		t.Fatalf("CDC: %d %v", len(events), err)
	}
	for _, event := range events {
		if event.Type != cdc.EventRegistryChanged || event.ProjectID != "" || event.SessionID != "" {
			t.Fatalf("registry CDC scope: %+v", event)
		}
	}
}

func TestRegistryManagerCannotRewriteOwnership(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	metadata := registryMetadata("Protected")
	definition := registryAgentDefinition()
	if _, err := s.CreateRegistryEntry(ctx, "protected", domain.RegistryAgentType, metadata, definition, registryMutation(domain.RegistryUser, 0)); err != nil {
		t.Fatal(err)
	}
	manager := registryMutation(domain.RegistryManager, 1)
	if _, err := s.AppendRegistryVersion(ctx, "protected", definition, manager); !errors.Is(err, ports.ErrRegistryForbidden) {
		t.Fatalf("version restriction: %v", err)
	}
	metadata.Name = "Rewritten"
	if _, err := s.UpdateRegistryMetadata(ctx, "protected", metadata, manager); !errors.Is(err, ports.ErrRegistryForbidden) {
		t.Fatalf("modify restriction: %v", err)
	}
	metadata.Policy.ManagerCanModify = true
	metadata.Policy.ManagerCanVersion = true
	if _, err := s.UpdateRegistryMetadata(ctx, "protected", metadata, registryMutation(domain.RegistryUser, 1)); err != nil {
		t.Fatal(err)
	}
	manager.ExpectedRevision = 2
	metadata.Policy.ManagerCanSelect = false
	if _, err := s.UpdateRegistryMetadata(ctx, "protected", metadata, manager); !errors.Is(err, ports.ErrRegistryForbidden) {
		t.Fatalf("policy escalation: %v", err)
	}
	if _, err := s.AppendRegistryVersion(ctx, "protected", definition, manager); err != nil {
		t.Fatal(err)
	}
	manager.ExpectedRevision = 3
	if _, err := s.ActivateRegistryVersion(ctx, "protected", 2, manager); !errors.Is(err, ports.ErrRegistryForbidden) {
		t.Fatalf("unapproved promotion: %v", err)
	}
	entry, err := s.GetRegistryEntry(ctx, "protected")
	if err != nil || entry.Origin != domain.RegistryUser || entry.ActiveVersion != 1 || entry.Revision != 3 {
		t.Fatalf("denied mutation changed state: %+v %v", entry, err)
	}
	created, err := s.CreateRegistryEntry(ctx, "manager", domain.RegistryAgentType, registryMetadata("Manager created"), definition, registryMutation(domain.RegistryManager, 0))
	if err != nil || created.Origin != domain.RegistryManager {
		t.Fatalf("manager provenance: %+v %v", created, err)
	}
}

func TestRegistryFailedPinsRollbackIdentityAuditAndCDC(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateRegistryEntry(ctx, "bad", domain.RegistryAgentType, registryMetadata("Bad"), registryAgentDefinition(domain.SkillVersionRef{ID: "missing", Version: 1}), registryMutation(domain.RegistryUser, 0)); !errors.Is(err, ports.ErrRegistryNotFound) {
		t.Fatalf("missing pin: %v", err)
	}
	if _, err := s.GetRegistryEntry(ctx, "bad"); !errors.Is(err, ports.ErrRegistryNotFound) {
		t.Fatal("partial identity persisted")
	}
	seq, err := s.LatestSeq(ctx)
	if err != nil || seq != 0 {
		t.Fatalf("failed creation emitted CDC: %d %v", seq, err)
	}
	if _, err := s.CreateRegistryEntry(ctx, "bad", domain.RegistryAgentType, registryMetadata("Recovered"), registryAgentDefinition(), registryMutation(domain.RegistryUser, 0)); err != nil {
		t.Fatalf("rolled back identity cannot be reused: %v", err)
	}
}

func TestRegistryConcurrentEditsRequireCurrentRevision(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateRegistryEntry(ctx, "race", domain.RegistryAgentType, registryMetadata("Race"), registryAgentDefinition(), registryMutation(domain.RegistryUser, 0)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.AppendRegistryVersion(ctx, "race", registryAgentDefinition(), registryMutation(domain.RegistryUser, 1))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ports.ErrRegistryConflict) {
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("%d writers succeeded with the same revision", success)
	}
	versions, err := s.ListRegistryVersions(ctx, "race", 0, 100)
	if err != nil || len(versions) != 2 {
		t.Fatal("rejected writers persisted versions")
	}
	if _, err := s.ListRegistryEntries(ctx, domain.RegistryAgentType, "", 201); err == nil {
		t.Fatal("unbounded page accepted")
	}
}
