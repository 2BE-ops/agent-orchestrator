package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type taskPromptReviewer struct{ fakeReviewer }

func (*taskPromptReviewer) SupportsTaskContext() bool { return true }

func taskLaunchSpec(t *testing.T) LaunchSpec {
	t.Helper()
	spec := launchSpec()
	spec.LaunchID, spec.TargetSHA = "native-task-launch", strings.Repeat("a", 40)
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "review", Requirement: "Identify authorization bypasses", EvidenceKind: "review"}}, ReviewPolicy: &domain.TaskReviewPolicy{AgentTypeID: "security-reviewer", Version: 2, DifferentAgentType: true, DifferentHarness: true}}
	_, criteriaHash, _ := domain.TaskContent(criteria)
	skill := domain.SkillDefinition{Instructions: "Inspect trust boundaries", Resources: []domain.SkillResource{{Path: "notes/reference.md", Content: "Review boundary reference"}}}
	_, skillHash, err := (domain.RegistryDefinition{Skill: &skill}).MarshalContent(domain.RegistrySkill)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	reviewer := domain.WorkerConfiguration{SchemaVersion: 1, AgentType: domain.WorkerDefinitionRef{ID: "security-reviewer", Version: 2, Name: "Security reviewer", ContentHash: strings.Repeat("b", 64)}, Selection: domain.WorkerSelection{AgentTypeID: "security-reviewer", Version: 2}, Effective: domain.AgentTypeDefinition{Harness: domain.HarnessClaudeCode, SessionMode: domain.SessionModeTUI, Config: domain.AgentConfig{Model: "review-model"}, Instructions: "Report only substantiated findings", Skills: []domain.SkillVersionRef{{ID: "security-skill", Version: 1}}, MaxParallelWorkers: 1}, Skills: []domain.WorkerSkillSnapshot{{Reference: domain.WorkerDefinitionRef{ID: "security-skill", Version: 1, Name: "Boundary review", ContentHash: skillHash}, Definition: skill}}, Origin: domain.RegistryUser, ActorID: "human", SystemPrompt: "Pinned project review instructions", CreatedAt: now}
	reviewer.ContentHash = reviewer.Hash()
	frozen := domain.TaskReviewContext{SchemaVersion: 1, TaskID: "task", AttemptID: "attempt", SessionID: spec.WorkerID, ResultID: "result", ResultHash: strings.Repeat("c", 64), TaskRevision: 1, CriteriaVersion: 1, Criteria: criteria, CriteriaHash: criteriaHash, TargetCommit: spec.TargetSHA, ImplementingType: domain.WorkerDefinitionRef{ID: "implementer", Version: 1}, ImplementingHarness: domain.HarnessCodex, ImplementingConfigurationHash: strings.Repeat("d", 64), Reviewer: reviewer, LaunchID: spec.LaunchID, Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, CreatedAt: now}
	frozen.ContentHash = frozen.Hash()
	if err := frozen.Validate(); err != nil {
		t.Fatal(err)
	}
	spec.TaskContext, spec.AgentConfig = &frozen, reviewer.Effective.Config
	return spec
}

func TestTaskReviewInvocationDeliversFrozenContextAndResources(t *testing.T) {
	ctx := context.Background()
	reviewer := &taskPromptReviewer{}
	runtime := &fakeRuntime{}
	dataDir := t.TempDir()
	launcher := NewLauncher(fakeReviewerResolver{reviewer: reviewer, ok: true}, runtime, dataDir)
	spec := taskLaunchSpec(t)
	if _, err := launcher.Spawn(ctx, spec); err != nil {
		t.Fatal(err)
	}
	inv := reviewer.gotInv
	if !runtime.created || inv.Config.Model != "review-model" || inv.ReviewerID != "review-mer-1-native-task-launch" || inv.AgentSessionID != "" {
		t.Fatalf("native invocation lost pinned identity: %+v", inv)
	}
	if strings.Contains(inv.Prompt, "authorization bypasses") || inv.SystemPrompt != "" {
		t.Fatal("full context leaked into terminal-visible prompt")
	}
	task, err := os.ReadFile(inv.TaskPromptFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, exact := range []string{spec.TaskContext.CriteriaHash, spec.TargetSHA, `"sourceGeneration": "native-task-launch"`, "Identify authorization bypasses"} {
		if !strings.Contains(string(task), exact) {
			t.Fatalf("frozen task omitted %q", exact)
		}
	}
	system, err := os.ReadFile(inv.SystemPromptFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(system), "Security reviewer (v2)") || !strings.Contains(string(system), "Pinned project review instructions") || !strings.Contains(string(system), "Code reviewer role") {
		t.Fatalf("system context lost pinned Type or review role: %s", system)
	}
	resourceDir := filepath.Join(inv.TaskPromptRoot, "worker-configurations", spec.RunID, "00-"+spec.TaskContext.Reviewer.Skills[0].Reference.ContentHash[:16])
	resource, err := os.ReadFile(filepath.Join(resourceDir, "notes", "reference.md"))
	if err != nil || string(resource) != "Review boundary reference" || !strings.Contains(string(system), filepath.ToSlash(filepath.Join(resourceDir, "SKILL.md"))) {
		t.Fatalf("sealed Skill missing or outside allowed prompt root: %s %v", resource, err)
	}
	encoded, err := os.ReadFile(filepath.Join(inv.TaskPromptRoot, "context.json"))
	var retained domain.TaskReviewContext
	if err != nil || json.Unmarshal(encoded, &retained) != nil || retained.ContentHash != spec.TaskContext.ContentHash {
		t.Fatalf("inspectable context differs from launch: %v", err)
	}
	// Retained bytes cannot be overwritten by a re-render using changed context.
	if err := os.WriteFile(inv.TaskPromptFile, []byte("changed prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := launcher.Spawn(ctx, spec); err == nil {
		t.Fatal("changed retained prompt was silently overwritten")
	}
}

func TestTaskReviewInvocationRejectsUnsupportedAndInheritedContext(t *testing.T) {
	for _, tc := range []struct {
		name     string
		change   func(*LaunchSpec)
		reviewer ports.Reviewer
	}{
		{"one-shot adapter", func(*LaunchSpec) {}, &fakeReviewer{}},
		{"different target", func(s *LaunchSpec) { s.TargetSHA = strings.Repeat("e", 40) }, &taskPromptReviewer{}},
		{"different native config", func(s *LaunchSpec) { s.AgentConfig.Model = "another-model" }, &taskPromptReviewer{}},
		{"inherited conversation", func(s *LaunchSpec) { s.AgentSessionID = "old-native-thread" }, &taskPromptReviewer{}},
		{"path escape", func(s *LaunchSpec) { s.RunID = "../outside" }, &taskPromptReviewer{}},
		{"Chat type", func(s *LaunchSpec) { s.TaskContext.Reviewer.Effective.SessionMode = domain.SessionModeChat }, &taskPromptReviewer{}},
		{"unsupported provider binding", func(s *LaunchSpec) {
			s.TaskContext.Reviewer.Effective.ProviderBindingID = "binding"
			s.TaskContext.Reviewer.Provider = &domain.ProviderBinding{ID: "binding", Harness: domain.HarnessClaudeCode, Enabled: true}
		}, &taskPromptReviewer{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := taskLaunchSpec(t)
			tc.change(&spec)
			spec.TaskContext.Reviewer.ContentHash = spec.TaskContext.Reviewer.Hash()
			spec.TaskContext.ContentHash = spec.TaskContext.Hash()
			runtime := &fakeRuntime{}
			launcher := NewLauncher(fakeReviewerResolver{reviewer: tc.reviewer, ok: true}, runtime, t.TempDir())
			if _, err := launcher.Spawn(context.Background(), spec); err == nil || runtime.created || runtime.destroyed != "" {
				t.Fatalf("invalid context affected runtime: %v %+v", err, runtime)
			}
		})
	}
	launcher := newTestLauncher(t, &taskPromptReviewer{}, &fakeRuntime{})
	spec := taskLaunchSpec(t)
	if err := launcher.Notify(context.Background(), "existing-pane", spec); err == nil {
		t.Fatal("context injected into an unrelated native configuration")
	}
}

type uncertainTaskRuntime struct{ fakeRuntime }

func (r *uncertainTaskRuntime) Create(_ context.Context, _ ports.RuntimeConfig) (ports.RuntimeHandle, error) {
	r.created = true
	return ports.RuntimeHandle{}, errors.New("runtime connection lost during creation")
}

func TestTaskReviewRuntimeCreationErrorIsUncertain(t *testing.T) {
	runtime := &uncertainTaskRuntime{}
	launcher := NewLauncher(fakeReviewerResolver{reviewer: &taskPromptReviewer{}, ok: true}, runtime, t.TempDir())
	if _, err := launcher.Spawn(context.Background(), taskLaunchSpec(t)); !errors.Is(err, ErrTaskReviewLaunchUncertain) || !runtime.created {
		t.Fatalf("runtime side effect classified as known failure: %v", err)
	}
}
