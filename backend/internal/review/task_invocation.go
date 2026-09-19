package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

func (l *agentLauncher) prepareTaskInvocation(ctx context.Context, spec LaunchSpec, inv ports.ReviewInvocation) (ports.ReviewInvocation, error) {
	frozen := spec.TaskContext
	if spec.AgentSessionID != "" {
		return inv, fmt.Errorf("fresh task review cannot inherit a previous native conversation")
	}
	if err := frozen.Validate(); err != nil {
		return inv, err
	}
	if frozen.SessionID != spec.WorkerID || frozen.TargetCommit != spec.TargetSHA || frozen.LaunchID != spec.LaunchID || frozen.Reviewer.Effective.Harness != domain.AgentHarness(spec.Harness) || frozen.Reviewer.Effective.Config != spec.AgentConfig {
		return inv, fmt.Errorf("review launch differs from frozen task configuration")
	}
	reviewer, ok := l.reviewers.Reviewer(spec.Harness)
	if !ok {
		return inv, fmt.Errorf("task reviewer adapter is unavailable")
	}
	capability, ok := reviewer.(ports.ReviewerTaskContextSupport)
	if !ok || !capability.SupportsTaskContext() {
		return inv, fmt.Errorf("reviewer does not support sealed task context")
	}
	configuration := frozen.Reviewer
	if configuration.Effective.SessionMode != domain.SessionModeTUI || configuration.Provider != nil || configuration.NativeSettings != nil || len(configuration.NativeOptions) != 0 {
		return inv, fmt.Errorf("task reviewer requires TUI configuration supported by its native review adapter")
	}
	if configuration.Effective.Config.Permissions != "" && configuration.Effective.Config.Permissions != domain.PermissionModeAuto {
		return inv, fmt.Errorf("task reviewer requires the native review adapter's automatic read-only policy")
	}
	for _, id := range []string{string(spec.WorkerID), spec.BatchID, spec.RunID, spec.LaunchID} {
		if id == "" || !filepath.IsLocal(id) || strings.ContainsAny(id, "/\\:") {
			return inv, fmt.Errorf("invalid task reviewer artifact identity")
		}
	}
	root, err := os.OpenRoot(l.dataDir)
	if err != nil {
		return inv, err
	}
	defer func() { _ = root.Close() }()
	relative := filepath.Join("prompts", string(spec.WorkerID), "reviewer", "requests", spec.BatchID, spec.RunID)
	encoded, err := json.Marshal(frozen)
	if err != nil {
		return inv, err
	}
	if err := sessionmanager.WriteSnapshotResource(root, filepath.Join(relative, "context.json"), string(encoded)); err != nil {
		return inv, err
	}
	requestRoot := filepath.Join(l.dataDir, relative)
	system, err := sessionmanager.MaterializeWorkerSnapshot(ctx, requestRoot, domain.SessionID(spec.RunID), configuration)
	if err != nil {
		return inv, err
	}
	system += "\n\n" + inv.SystemPrompt
	for _, resource := range []struct{ name, content string }{{"task.md", inv.Prompt}, {"system.md", system}} {
		if err := ctx.Err(); err != nil {
			return inv, err
		}
		if err := sessionmanager.WriteSnapshotResource(root, filepath.Join(relative, resource.name), resource.content); err != nil {
			return inv, err
		}
	}
	inv.TaskPromptFile = filepath.Join(requestRoot, "task.md")
	inv.SystemPromptFile = filepath.Join(requestRoot, "system.md")
	inv.TaskPromptRoot = requestRoot
	inv.SystemPrompt = ""
	inv.Prompt = reviewerTaskMessagePrefix + filepath.ToSlash(inv.TaskPromptFile) + "`."
	// Fresh passes must not inherit a prior Type's provider-native conversation.
	inv.ReviewerID = reviewerHandleID(spec.WorkerID) + "-" + spec.LaunchID
	return inv, nil
}
