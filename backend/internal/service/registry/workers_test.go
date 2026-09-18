package registry

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type workerDefaultMode domain.SessionMode

func (m workerDefaultMode) DefaultSessionMode(context.Context) domain.SessionMode {
	return domain.SessionMode(m)
}

func TestWorkerAuthoringCheckUsesActualProjectDefaults(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpen(t)
	native := nativeReady()
	svc := NewWithNative(store, native)
	svc.SetSessionDefaults(workerDefaultMode(domain.SessionModeTUI))
	input := workerTypeInput()
	input.Definition.AgentType.Config = domain.AgentConfig{}
	entry, err := svc.Create(ctx, domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, domain.RegistryAgentType, input)
	if err != nil {
		t.Fatal(err)
	}
	project := domain.ProjectRecord{ID: "project", RegisteredAt: time.Now().UTC(), Config: domain.ProjectConfig{Worker: domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Model: "unavailable-model"}}}}
	if err := store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	check, err := svc.Check(ctx, entry.Entry.ID, CheckInput{Version: 1, ProjectID: project.ID})
	if err != nil || !hasIssue(check, "MODEL_UNSUPPORTED") {
		t.Fatalf("project default escaped check: %+v %v", check, err)
	}
	project.Config.Worker.AgentConfig.Model = "configured/model"
	if err := store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	check, err = svc.Check(ctx, entry.Entry.ID, CheckInput{Version: 1, ProjectID: project.ID})
	if err != nil || !check.Ready {
		t.Fatalf("valid defaults unavailable: %+v %v", check, err)
	}
	if _, err := svc.Check(ctx, entry.Entry.ID, CheckInput{Version: 1, ProjectID: "missing"}); err == nil {
		t.Fatal("missing project treated as standalone")
	}
}

func workerTypeInput() CreateInput {
	return CreateInput{Metadata: domain.RegistryMetadata{Name: "Reviewer", Enabled: true, Policy: domain.RegistryPolicy{ManagerCanSelect: true}}, Definition: domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, Config: domain.AgentConfig{Model: "configured/model", Effort: "high"}, MaxParallelWorkers: 2, Instructions: "Original type instructions"}}, Reason: "Define reviewer"}
}

func TestWorkerResolutionPinsContentAndExplicitOverrides(t *testing.T) {
	ctx := context.Background()
	svc := NewWithNative(sqlitetest.MustOpen(t), nativeReady())
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
	skill, err := svc.Create(ctx, actor, domain.RegistrySkill, CreateInput{Metadata: domain.RegistryMetadata{Name: "Review checklist", Enabled: true, Policy: domain.RegistryPolicy{ManagerCanSelect: true}}, Definition: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Review exact evidence", Resources: []domain.SkillResource{{Path: "references/checklist.md", Content: "Original reference"}}}}, Reason: "Author checklist"})
	if err != nil {
		t.Fatal(err)
	}
	input := workerTypeInput()
	input.Definition.AgentType.Skills = []domain.SkillVersionRef{{ID: skill.Entry.ID, Version: 1}}
	entry, err := svc.Create(ctx, actor, domain.RegistryAgentType, input)
	if err != nil {
		t.Fatal(err)
	}
	selection := domain.WorkerSelection{AgentTypeID: entry.Entry.ID}
	snapshot, err := svc.ResolveWorker(ctx, selection, domain.ProjectRecord{}, domain.SessionModeTUI, actor)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgentType.Version != 1 || snapshot.Skills[0].Definition.Resources[0].Content != "Original reference" || snapshot.Effective.Config.Permissions != domain.PermissionModeAuto || snapshot.Effective.SessionMode != domain.SessionModeTUI {
		t.Fatalf("wrong snapshot: %+v", snapshot)
	}
	changed := input.Definition
	changed.AgentType.Instructions = "Changed instructions"
	if _, err := svc.Append(ctx, actor, domain.RegistryAgentType, entry.Entry.ID, VersionInput{Definition: changed, ExpectedRevision: 1, Reason: "New instructions"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Activate(ctx, actor, domain.RegistryAgentType, entry.Entry.ID, ActivateInput{Version: 2, ExpectedRevision: 2, Reason: "Use new instructions"}); err != nil {
		t.Fatal(err)
	}
	if snapshot.Effective.Instructions != "Original type instructions" {
		t.Fatal("snapshot mutated after version activation")
	}
	empty := ""
	noSkills := []domain.SkillVersionRef{}
	selection.Version = 1
	selection.Overrides = domain.WorkerOverrides{Model: &empty, Effort: &empty, Instructions: &empty, Skills: &noSkills}
	snapshot, err = svc.ResolveWorker(ctx, selection, domain.ProjectRecord{Config: domain.ProjectConfig{Worker: domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Model: "unavailable-project-model", Effort: "wrong"}}}}, domain.SessionModeTUI, actor)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgentType.Version != 1 || snapshot.Effective.Config.Model != "" || snapshot.Effective.Config.Effort != "" || snapshot.Effective.Instructions != "" || len(snapshot.Skills) != 0 {
		t.Fatalf("explicit clearing lost: %+v", snapshot)
	}
	snapshot.ContentHash = snapshot.Hash()
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerResolutionDoesNotLeakOtherHarnessDefaults(t *testing.T) {
	definition := *workerTypeInput().Definition.AgentType
	definition.Config = domain.AgentConfig{}
	project := domain.ProjectConfig{AgentConfig: domain.AgentConfig{Model: "claude-alias", Effort: "claude-effort"}, Worker: domain.RoleOverride{Harness: domain.HarnessClaudeCode, AgentConfig: domain.AgentConfig{Model: "claude-role", Permissions: domain.PermissionModeDefault}}}
	resolved := resolveWorkerOptions(definition, domain.WorkerOverrides{}, project, domain.SessionModeTUI)
	if resolved.Config.Model != "" || resolved.Config.Effort != "" || resolved.Config.Permissions != domain.PermissionModeDefault {
		t.Fatalf("incompatible inherited defaults: %+v", resolved)
	}
	harness := domain.HarnessClaudeCode
	resolved = resolveWorkerOptions(*workerTypeInput().Definition.AgentType, domain.WorkerOverrides{Harness: &harness}, domain.ProjectConfig{}, domain.SessionModeTUI)
	if resolved.Config.Model != "" || resolved.Config.Effort != "" {
		t.Fatal("one-off harness change retained incompatible type model")
	}
}

func TestWorkerRestoreRetainsDisabledDefinitionContentButChecksNativeBinding(t *testing.T) {
	ctx := context.Background()
	svc := NewWithNative(sqlitetest.MustOpen(t), nativeReady())
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
	binding, err := svc.CreateBinding(ctx, actor, BindingCreateInput{Name: "Configured", Harness: domain.HarnessCodex, Provider: "configured", Reason: "Bind native provider"})
	if err != nil {
		t.Fatal(err)
	}
	input := workerTypeInput()
	input.Definition.AgentType.ProviderBindingID = binding.ID
	entry, err := svc.Create(ctx, actor, domain.RegistryAgentType, input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.ResolveWorker(ctx, domain.WorkerSelection{AgentTypeID: entry.Entry.ID}, domain.ProjectRecord{}, domain.SessionModeTUI, actor)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ContentHash = snapshot.Hash()
	metadata := entry.Entry.Metadata
	metadata.Enabled = false
	if _, err := svc.Update(ctx, actor, domain.RegistryAgentType, entry.Entry.ID, MetadataInput{Metadata: metadata, ExpectedRevision: 1, Reason: "Disable future selection"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateWorkerRestore(ctx, snapshot, ""); err != nil {
		t.Fatalf("historical snapshot consulted current selection policy: %v", err)
	}
	if _, err := svc.UpdateBinding(ctx, actor, binding.ID, BindingUpdateInput{Name: binding.Name, Enabled: false, ExpectedRevision: 1, Reason: "Revoke provider reference"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateWorkerRestore(ctx, snapshot, ""); err == nil {
		t.Fatal("fresh restoration ignored unavailable native binding")
	}
}
