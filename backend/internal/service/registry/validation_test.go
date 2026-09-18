package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type nativeFixture struct {
	catalog       ports.AgentModelCatalog
	configuration agentsvc.Configuration
	readiness     domain.AgentReadinessSnapshot
	modelErr      error
}

func (n *nativeFixture) Configuration(context.Context, string, domain.SessionMode) (agentsvc.Configuration, error) {
	return n.configuration, nil
}
func (n *nativeFixture) Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error) {
	return n.catalog, n.modelErr
}
func (n *nativeFixture) EnsureAgentReadiness(context.Context, string, domain.AgentReadinessPurpose) (domain.AgentReadinessSnapshot, error) {
	return n.readiness, nil
}
func nativeReady() *nativeFixture {
	return &nativeFixture{catalog: ports.AgentModelCatalog{SelectionMode: ports.ModelSelectionCatalog, CustomModelEntry: ports.CustomModelEntryConfigured, BinaryVersion: "native-fingerprint", Models: []ports.AgentModelInfo{{ID: "configured/model", Provider: "configured", Efforts: []string{"high"}}}}, configuration: agentsvc.Configuration{Fields: []agentsvc.ConfigurationField{{Key: "model"}, {Key: "permissions", Options: []string{"default", "auto"}}}, CapabilityState: "supported"}, readiness: domain.AgentReadinessSnapshot{EffectiveReadiness: domain.AgentReadinessReady}}
}
func hasIssue(check ConfigurationCheck, code string) bool {
	for _, issue := range check.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestNativeBindingAndConfigurationValidation(t *testing.T) {
	ctx := context.Background()
	native := nativeReady()
	svc := NewWithNative(sqlitetest.MustOpen(t), native)
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
	binding, err := svc.CreateBinding(ctx, actor, BindingCreateInput{Name: "Configured", Harness: domain.HarnessCodex, Provider: "configured", ProjectID: "project", Reason: "Select existing provider"})
	if err != nil {
		t.Fatal(err)
	}
	definition := domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeTUI, ProviderBindingID: binding.ID, Config: domain.AgentConfig{Model: "configured/model", Effort: "high"}, MaxParallelWorkers: 1}
	check, err := svc.CheckConfiguration(ctx, definition, "project")
	if err != nil || !check.Ready || check.BindingRevision != 1 || check.CatalogFingerprint != "native-fingerprint" {
		t.Fatalf("valid reference rejected: %+v %v", check, err)
	}
	for _, tc := range []struct {
		name, code string
		change     func(*domain.AgentTypeDefinition)
	}{
		{"missing binding", "PROVIDER_BINDING_UNAVAILABLE", func(d *domain.AgentTypeDefinition) { d.ProviderBindingID = "deleted" }},
		{"required rebind", "PROVIDER_BINDING_UNAVAILABLE", func(d *domain.AgentTypeDefinition) { d.ProviderBindingID = ""; d.ProviderBindingRequired = true }},
		{"wrong harness", "PROVIDER_BINDING_UNAVAILABLE", func(d *domain.AgentTypeDefinition) { d.Harness = domain.HarnessClaudeCode }},
		{"unlisted model", "MODEL_UNSUPPORTED", func(d *domain.AgentTypeDefinition) { d.Config.Model = "unavailable" }},
		{"unsupported effort", "EFFORT_UNSUPPORTED", func(d *domain.AgentTypeDefinition) { d.Config.Effort = "low" }},
		{"unsupported permissions", "PERMISSIONS_UNSUPPORTED", func(d *domain.AgentTypeDefinition) { d.Config.Permissions = domain.PermissionModeBypassPermissions }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := definition
			tc.change(&changed)
			check, err := svc.CheckConfiguration(ctx, changed, "project")
			if err != nil || check.Ready || !hasIssue(check, tc.code) {
				t.Fatalf("invalid configuration accepted: %+v %v", check, err)
			}
		})
	}
	check, _ = svc.CheckConfiguration(ctx, definition, "other-project")
	if !hasIssue(check, "PROVIDER_BINDING_UNAVAILABLE") {
		t.Fatal("cross-project reference accepted")
	}
	native.catalog.Models[0].Provider = "changed-provider"
	check, _ = svc.CheckConfiguration(ctx, definition, "project")
	if !hasIssue(check, "BOUND_PROVIDER_UNAVAILABLE") {
		t.Fatal("missing native provider silently fell back")
	}
	if _, err := svc.UpdateBinding(ctx, actor, binding.ID, BindingUpdateInput{Name: binding.Name, Enabled: false, ExpectedRevision: 1, Reason: "Disable"}); err != nil {
		t.Fatal(err)
	}
	check, _ = svc.CheckConfiguration(ctx, definition, "project")
	if !hasIssue(check, "PROVIDER_BINDING_UNAVAILABLE") {
		t.Fatal("disabled reference accepted")
	}
}

func TestNativeUnknownAndDefaultConfiguration(t *testing.T) {
	ctx := context.Background()
	native := nativeReady()
	svc := NewWithNative(sqlitetest.MustOpen(t), native)
	definition := domain.AgentTypeDefinition{Harness: domain.HarnessCodex, MaxParallelWorkers: 1}
	native.catalog.Stale = true
	native.modelErr = errors.New("catalog offline")
	check, err := svc.CheckConfiguration(ctx, definition, "")
	if err != nil || !check.Ready {
		t.Fatalf("native default unnecessarily requires model catalog: %+v %v", check, err)
	}
	definition.Config.Model = "configured/model"
	check, _ = svc.CheckConfiguration(ctx, definition, "")
	if !hasIssue(check, "MODELS_UNAVAILABLE") {
		t.Fatal("stale model choices treated as verified")
	}
	native.readiness.EffectiveReadiness = domain.AgentReadinessUnknown
	check, _ = svc.CheckConfiguration(ctx, definition, "")
	if !hasIssue(check, "NATIVE_READINESS_UNAVAILABLE") || check.Readiness.EffectiveReadiness != domain.AgentReadinessUnknown {
		t.Fatal("unknown readiness lost")
	}
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
	if _, err := svc.CreateBinding(ctx, actor, BindingCreateInput{Name: "Unverified", Harness: domain.HarnessCodex, Provider: "unknown", Reason: "Import"}); err == nil {
		t.Fatal("unverified native provider reference accepted")
	}
}

func TestSkillRequirementsAreNotPermissionGrants(t *testing.T) {
	ctx := context.Background()
	svc := NewWithNative(sqlitetest.MustOpen(t), nativeReady())
	skill, err := svc.Create(ctx, domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, domain.RegistrySkill, CreateInput{Metadata: domain.RegistryMetadata{Name: "MCP", Enabled: true}, Definition: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Use required capabilities", RequiredTools: []string{"unadvertised-tool"}, RequiredMCPServers: []string{"private-server"}}}, Reason: "Describe prerequisites"})
	if err != nil {
		t.Fatal(err)
	}
	check, err := svc.CheckConfiguration(ctx, domain.AgentTypeDefinition{Harness: domain.HarnessCodex, MaxParallelWorkers: 1, Skills: []domain.SkillVersionRef{{ID: skill.Entry.ID, Version: 1}}}, "")
	if err != nil || check.Ready || !hasIssue(check, "SKILL_TOOL_UNVERIFIED") || !hasIssue(check, "SKILL_MCP_UNVERIFIED") {
		t.Fatalf("unverified requirements granted: %+v %v", check, err)
	}
}
