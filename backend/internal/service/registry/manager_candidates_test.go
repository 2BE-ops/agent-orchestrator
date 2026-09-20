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

type candidateNative struct {
	*nativeFixture
	calls int
}

func (n *candidateNative) Configuration(ctx context.Context, harness string, mode domain.SessionMode) (agentsvc.Configuration, error) {
	n.calls++
	return n.nativeFixture.Configuration(ctx, harness, mode)
}

func candidateTask(class domain.ContextClass) domain.TaskDefinition {
	return domain.TaskDefinition{Title: "Implement work", Brief: "Pinned requirements", MaxAttempts: 1, Classification: class, EngagementID: "client-a"}
}
func candidateHasIssue(candidate domain.AgentManagerCandidate, code string) bool {
	for _, issue := range candidate.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestManagerCandidatesEnforceExactClearanceLatticeBeforeNativeProbes(t *testing.T) {
	ctx := context.Background()
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
	for _, clearance := range []domain.ContextClass{domain.ContextTechnical, domain.ContextEngagement, domain.ContextMission} {
		t.Run(string(clearance), func(t *testing.T) {
			native := &candidateNative{nativeFixture: nativeReady()}
			m := NewWithNative(sqlitetest.MustOpen(t), native)
			input := workerTypeInput()
			input.Definition.AgentType.MaxContextClass = clearance
			view, err := m.Create(ctx, actor, domain.RegistryAgentType, input)
			if err != nil {
				t.Fatal(err)
			}
			for _, class := range []domain.ContextClass{domain.ContextTechnical, domain.ContextEngagement, domain.ContextMission} {
				before := native.calls
				candidate, err := m.AssessManagerCandidate(ctx, view.Entry.ID, 1, candidateTask(class), domain.ProjectRecord{})
				allowed := clearance.Allows(class)
				if err != nil || candidate.Eligible != allowed || candidate.AgentType.ContentHash != view.Version.ContentHash || candidate.MaxContextClass != clearance {
					t.Fatalf("%s -> %s: %+v %v", class, clearance, candidate, err)
				}
				if !allowed && (!candidateHasIssue(candidate, "CONTEXT_CLEARANCE_EXCEEDED") || native.calls != before) {
					t.Fatalf("prohibited input probed native configuration: %+v", candidate)
				}
			}
		})
	}
}

func TestManagerCandidateUsesPinnedVersionDespiteHigherActiveClearance(t *testing.T) {
	ctx := context.Background()
	m := NewWithNative(sqlitetest.MustOpen(t), nativeReady())
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
	input := workerTypeInput()
	view, err := m.Create(ctx, actor, domain.RegistryAgentType, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Definition.AgentType.MaxContextClass = domain.ContextMission
	if _, err := m.Append(ctx, actor, domain.RegistryAgentType, view.Entry.ID, VersionInput{Definition: input.Definition, ExpectedRevision: 1, Reason: "Raise clearance for new work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Activate(ctx, actor, domain.RegistryAgentType, view.Entry.ID, ActivateInput{Version: 2, ExpectedRevision: 2, Reason: "Activate mission type"}); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int64{1, 2} {
		candidate, err := m.AssessManagerCandidate(ctx, view.Entry.ID, version, candidateTask(domain.ContextMission), domain.ProjectRecord{})
		if err != nil || candidate.Eligible != (version == 2) || candidate.AgentType.Version != version || candidate.MetadataRevision != 3 {
			t.Fatalf("historical version changed: %+v %v", candidate, err)
		}
	}
	metadata := view.Entry.Metadata
	metadata.Policy.ManagerCanSelect = false
	metadata.Policy.ManagerCanModify = true
	metadata.Policy.ManagerCanVersion = true
	if _, err := m.Update(ctx, actor, domain.RegistryAgentType, view.Entry.ID, MetadataInput{Metadata: metadata, ExpectedRevision: 3, Reason: "Retain independent ownership"}); err != nil {
		t.Fatal(err)
	}
	candidate, err := m.AssessManagerCandidate(ctx, view.Entry.ID, 2, candidateTask(domain.ContextTechnical), domain.ProjectRecord{})
	if err != nil || candidate.Eligible || !candidateHasIssue(candidate, "MANAGER_SELECTION_FORBIDDEN") {
		t.Fatalf("modify/version permission authorized selection: %+v %v", candidate, err)
	}
}

func TestManagerCandidateSkillsCapabilitiesAndNativeChecks(t *testing.T) {
	ctx := context.Background()
	native := &candidateNative{nativeFixture: nativeReady()}
	m := NewWithNative(sqlitetest.MustOpen(t), native)
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
	skill, err := m.Create(ctx, actor, domain.RegistrySkill, CreateInput{Metadata: domain.RegistryMetadata{Name: "Rust checks", Enabled: true, Policy: domain.RegistryPolicy{ManagerCanSelect: true}}, Definition: domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Inspect code", Capabilities: []string{"rust"}}}, Reason: "Reuse checks"})
	if err != nil {
		t.Fatal(err)
	}
	input := workerTypeInput()
	input.Definition.AgentType.Capabilities = []string{"tests"}
	input.Definition.AgentType.Skills = []domain.SkillVersionRef{{ID: skill.Entry.ID, Version: 1}}
	view, err := m.Create(ctx, actor, domain.RegistryAgentType, input)
	if err != nil {
		t.Fatal(err)
	}
	task := candidateTask(domain.ContextTechnical)
	task.RequiredCapabilities = []string{"tests", "rust"}
	check := func() domain.AgentManagerCandidate {
		t.Helper()
		result, err := m.AssessManagerCandidate(ctx, view.Entry.ID, 1, task, domain.ProjectRecord{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := check()
	if !result.Eligible || len(result.Skills) != 1 || result.Skills[0].Reference.ContentHash != skill.Version.ContentHash || result.CatalogFingerprint != "native-fingerprint" {
		t.Fatalf("composition lost: %+v", result)
	}
	task.RequiredCapabilities = []string{"Rust"}
	before := native.calls
	result = check()
	if result.Eligible || !candidateHasIssue(result, "REQUIRED_CAPABILITIES_MISSING") || native.calls != before || len(result.MissingCapabilities) != 1 {
		t.Fatalf("semantic matching invented: %+v", result)
	}
	task.RequiredCapabilities = []string{"rust"}
	metadata := skill.Entry.Metadata
	metadata.Policy.ManagerCanSelect = false
	if _, err := m.Update(ctx, actor, domain.RegistrySkill, skill.Entry.ID, MetadataInput{Metadata: metadata, ExpectedRevision: 1, Reason: "Protect Skill selection"}); err != nil {
		t.Fatal(err)
	}
	before = native.calls
	result = check()
	if result.Eligible || !candidateHasIssue(result, "MANAGER_SKILL_SELECTION_FORBIDDEN") || native.calls != before {
		t.Fatalf("protected Skill selected: %+v", result)
	}
	metadata.Policy.ManagerCanSelect = true
	metadata.Enabled = false
	if _, err := m.Update(ctx, actor, domain.RegistrySkill, skill.Entry.ID, MetadataInput{Metadata: metadata, ExpectedRevision: 2, Reason: "Disable Skill"}); err != nil {
		t.Fatal(err)
	}
	if result = check(); result.Eligible || !candidateHasIssue(result, "SKILL_DISABLED") {
		t.Fatalf("disabled Skill selected: %+v", result)
	}
	metadata.Enabled = true
	if _, err := m.Update(ctx, actor, domain.RegistrySkill, skill.Entry.ID, MetadataInput{Metadata: metadata, ExpectedRevision: 3, Reason: "Enable reusable Skill"}); err != nil {
		t.Fatal(err)
	}
	native.modelErr = errors.New("private provider diagnostics must not enter candidate output")
	result = check()
	if result.Eligible || !candidateHasIssue(result, "MODELS_UNAVAILABLE") {
		t.Fatalf("unavailable provider accepted: %+v", result)
	}
	native.modelErr = nil
	native.readiness.EffectiveReadiness = domain.AgentReadinessUnknown
	if result = check(); result.Eligible {
		t.Fatal("unknown readiness accepted")
	}
}

func TestManagerCandidateMissingDisabledAndInheritedConfiguration(t *testing.T) {
	ctx := context.Background()
	m := NewWithNative(sqlitetest.MustOpen(t), nativeReady())
	actor := domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}
	input := workerTypeInput()
	input.Definition.AgentType.Config = domain.AgentConfig{}
	view, err := m.Create(ctx, actor, domain.RegistryAgentType, input)
	if err != nil {
		t.Fatal(err)
	}
	project := domain.ProjectRecord{Config: domain.ProjectConfig{Worker: domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Model: "unavailable-inherited-model"}}}}
	result, err := m.AssessManagerCandidate(ctx, view.Entry.ID, 1, candidateTask(domain.ContextTechnical), project)
	if err != nil || result.Eligible || !candidateHasIssue(result, "MODEL_UNSUPPORTED") || result.Config.Model != "unavailable-inherited-model" {
		t.Fatalf("project config bypassed: %+v %v", result, err)
	}
	for _, tc := range []struct {
		id      string
		version int64
		code    string
	}{{"missing", 1, "AGENT_TYPE_NOT_FOUND"}, {view.Entry.ID, 999, "AGENT_TYPE_VERSION_NOT_FOUND"}} {
		result, err := m.AssessManagerCandidate(ctx, tc.id, tc.version, candidateTask(domain.ContextTechnical), domain.ProjectRecord{})
		if err != nil || result.Eligible || !candidateHasIssue(result, tc.code) {
			t.Fatalf("missing candidate: %+v %v", result, err)
		}
	}
	metadata := view.Entry.Metadata
	metadata.Enabled = false
	if _, err := m.Update(ctx, actor, domain.RegistryAgentType, view.Entry.ID, MetadataInput{Metadata: metadata, ExpectedRevision: 1, Reason: "Disabled type"}); err != nil {
		t.Fatal(err)
	}
	result, err = m.AssessManagerCandidate(ctx, view.Entry.ID, 1, candidateTask(domain.ContextTechnical), domain.ProjectRecord{})
	if err != nil || result.Eligible || !candidateHasIssue(result, "AGENT_TYPE_DISABLED") {
		t.Fatalf("disabled Type accepted: %+v %v", result, err)
	}
	if _, err = m.AssessManagerCandidate(ctx, view.Entry.ID, 0, candidateTask(domain.ContextTechnical), domain.ProjectRecord{}); err == nil {
		t.Fatal("implicit active version accepted")
	}
}

type failedCandidateRegistry struct {
	ports.RegistryStore
	err error
}

func (s failedCandidateRegistry) GetRegistryEntry(context.Context, string) (domain.RegistryEntry, error) {
	return domain.RegistryEntry{}, s.err
}
func TestManagerCandidatePreservesStorageFailure(t *testing.T) {
	err := errors.New("database unavailable")
	m := New(failedCandidateRegistry{err: err})
	if _, got := m.AssessManagerCandidate(context.Background(), "type", 1, candidateTask(domain.ContextTechnical), domain.ProjectRecord{}); !errors.Is(got, err) {
		t.Fatalf("storage failure masked as incompatible: %v", got)
	}
}
