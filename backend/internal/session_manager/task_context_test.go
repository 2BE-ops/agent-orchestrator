package sessionmanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/taskcontext"
)

func taskContextKnowledge(t *testing.T, f taskWorkerFixture, content string) domain.KnowledgeDefinition {
	t.Helper()
	d := domain.KnowledgeDefinition{Title: "Relevant constraint", Kind: "constraint", Content: content, Status: "accepted", Confidence: "high", TaskIDs: []string{"task"}, Sources: []domain.KnowledgeSource{{Kind: "user", Reference: "Reviewed project constraint"}}}
	if _, err := f.store.CreateProjectKnowledge(context.Background(), "knowledge", "task-project", d, domain.KnowledgeMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Accept constraint"}); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTaskContextReachesNativeLaunchAndFreshRestoreWithoutRebuilding(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			f := newTaskWorkerFixture(t, mode, "context.txt", ".env", "missing.txt")
			workspace := f.manager.workspace.(*fakeWorkspace).path
			if err := os.WriteFile(filepath.Join(workspace, "context.txt"), []byte("Original repository interface"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspace, ".env"), []byte("SHOULD_NEVER_REACH_CONTEXT"), 0o600); err != nil {
				t.Fatal(err)
			}
			knowledge := taskContextKnowledge(t, f, "Original accepted knowledge")
			f.launcher.beforeStart = func(start ChatStart) {
				if _, found, err := f.store.GetTaskContextBySession(ctx, start.SessionID); err != nil || !found {
					t.Fatalf("native launch preceded seal: %v %v", found, err)
				}
			}
			rec, promptBytes, _, err := f.manager.Spawn(ctx, f.cfg)
			if err != nil {
				t.Fatal(err)
			}
			frozen, found, err := f.store.GetTaskContext(ctx, f.lease.AttemptID)
			if err != nil || !found {
				t.Fatalf("context: %v %v", found, err)
			}
			if promptBytes != len(frozen.Prompt) || !strings.Contains(frozen.Prompt, "Original repository interface") || !strings.Contains(frozen.Prompt, "Original accepted knowledge") || strings.Contains(frozen.Prompt, "SHOULD_NEVER_REACH_CONTEXT") {
				t.Fatalf("incorrect native context: %+v", frozen)
			}
			agent := f.manager.agents.(singleAgent).agent.(*recordingAgent)
			if mode == domain.SessionModeTUI && agent.lastLaunch.Prompt != frozen.Prompt {
				t.Fatal("TUI did not receive sealed context")
			}
			if mode == domain.SessionModeChat && (len(f.launcher.turns) != 1 || f.launcher.turns[0] != frozen.Prompt) {
				t.Fatal("Chat did not receive sealed context")
			}
			if err := os.WriteFile(filepath.Join(workspace, "context.txt"), []byte("Changed repository interface"), 0o600); err != nil {
				t.Fatal(err)
			}
			knowledge.Status = "invalidated"
			if _, err := f.store.ReviseProjectKnowledge(ctx, "knowledge", knowledge, domain.KnowledgeMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Invalidate claim", ExpectedVersion: 1}); err != nil {
				t.Fatal(err)
			}
			// Even a corrupted fallback prompt cannot replace the immutable input.
			rec.IsTerminated, rec.Metadata.Prompt, rec.Metadata.AgentSessionID = true, "Changed fallback", ""
			if err := f.store.UpdateSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			if _, err := f.manager.RestoreWithMode(ctx, rec.ID); err != nil {
				t.Fatal(err)
			}
			if mode == domain.SessionModeTUI && agent.lastLaunch.Prompt != frozen.Prompt {
				t.Fatal("fresh restore rebuilt historical context")
			}
			if mode == domain.SessionModeChat && len(f.launcher.turns) != 1 {
				t.Fatal("native Chat resume duplicated task input")
			}
			retained, _, err := f.store.GetTaskContext(ctx, f.lease.AttemptID)
			if err != nil || retained.ContentHash != frozen.ContentHash {
				t.Fatalf("restore changed context: %v", err)
			}
		})
	}
}

type limitedTaskContext struct {
	ports.TaskContextBuilder
	budget         domain.ContextBudget
	promptHeadroom int
	tokenHeadroom  int
}

func (b limitedTaskContext) Build(ctx context.Context, request ports.TaskContextRequest) (domain.TaskContextSnapshot, error) {
	request.Budget = b.budget
	if b.promptHeadroom > 0 {
		request.Budget.MaxBytes = len(request.SystemPrompt) + b.promptHeadroom
	}
	if b.tokenHeadroom > 0 {
		request.Budget.MaxEstimatedTokens = (len(request.SystemPrompt)+3)/4 + b.tokenHeadroom
	}
	return b.TaskContextBuilder.Build(ctx, request)
}

func TestTaskContextBudgetsRetainOmissionsAndFenceUnlaunchableInstructions(t *testing.T) {
	for _, kind := range []string{"source_bytes", "prompt_bytes", "estimated_tokens", "source_count", "mandatory", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f := newTaskWorkerFixture(t, domain.SessionModeTUI, "missing.txt")
			taskContextKnowledge(t, f, strings.Repeat("Accepted knowledge ", 800))
			budget := domain.DefaultContextBudget()
			promptHeadroom, tokenHeadroom := 0, 0
			switch kind {
			case "source_bytes":
				budget.MaxSourceBytes = 1024
			case "prompt_bytes":
				promptHeadroom = 8000
			case "estimated_tokens":
				tokenHeadroom = 2000
			case "source_count":
				budget.MaxSources = 4
			case "mandatory":
				budget.MaxSources = 2
			}
			f.manager.taskContexts = limitedTaskContext{TaskContextBuilder: taskcontext.New(f.store), budget: budget, promptHeadroom: promptHeadroom, tokenHeadroom: tokenHeadroom}
			if kind == "unavailable" {
				f.manager.taskContexts = nil
			}
			_, _, _, err := f.manager.Spawn(ctx, f.cfg)
			if kind == "mandatory" || kind == "unavailable" {
				if err == nil || f.runtime.created != 0 {
					t.Fatalf("unbounded launch: %v", err)
				}
				if _, found, err := f.store.GetTaskContext(ctx, f.lease.AttemptID); err != nil || found {
					t.Fatalf("failed build sealed context: %v %v", found, err)
				}
				if _, active, err := f.store.GetActiveTaskLease(ctx, "task"); err != nil || !active {
					t.Fatalf("failed build released lease: %v %v", active, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			snapshot, found, err := f.store.GetTaskContext(ctx, f.lease.AttemptID)
			if err != nil || !found {
				t.Fatalf("bounded context: %v %v", found, err)
			}
			if err := snapshot.Validate(); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(snapshot.Prompt, "Accepted knowledge") {
				t.Fatal("optional content bypassed budget")
			}
			omitted := false
			for _, source := range snapshot.Sources {
				if source.Disposition == "omitted" && (source.Kind == "knowledge" || source.Kind == "selection") {
					omitted = true
				}
			}
			if !omitted {
				t.Fatal("budget silently lost omission provenance")
			}
		})
	}
}

func TestTaskContextReplayIsImmutableAndOwnershipFenced(t *testing.T) {
	ctx := context.Background()
	f := newTaskWorkerFixture(t, domain.SessionModeTUI)
	if _, _, _, err := f.manager.Spawn(ctx, f.cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := f.store.GetTaskContext(ctx, f.lease.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	request := ports.TaskContextRequest{Lease: f.lease.TaskLeaseToken, SessionID: snapshot.SessionID, ExecutionOperationID: snapshot.ExecutionOperationID, Prompt: "Different", WorkspacePath: "missing"}
	replayed, err := f.manager.taskContexts.Build(ctx, request)
	if err != nil || replayed.ContentHash != snapshot.ContentHash {
		t.Fatalf("replay rebuilt context: %v", err)
	}
	request.Lease.HolderID = "intruder"
	if _, err := f.manager.taskContexts.Build(ctx, request); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("unowned replay: %v", err)
	}
}

func TestTaskContextUsesFrozenCriteriaAndDependenciesWithCurrentParentProvenance(t *testing.T) {
	ctx := context.Background()
	f := newTaskWorkerFixture(t, domain.SessionModeTUI)
	mutation := domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Plan related work"}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "proof", Requirement: "Original requirement", EvidenceKind: "review"}}}
	definition := domain.TaskDefinition{Title: "Parent", Brief: "Original parent", MaxAttempts: 3}
	if _, err := f.store.CreateAdaptiveTask(ctx, "parent", "task-project", definition, &criteria, mutation); err != nil {
		t.Fatal(err)
	}
	definition.Title, definition.Brief = "Related work", "Fixed related work"
	definition.ParentID, definition.Dependencies, definition.RequestedWorker = "parent", []string{"task"}, f.cfg.WorkerSelection
	if _, err := f.store.CreateAdaptiveTask(ctx, "related", "task-project", definition, &criteria, mutation); err != nil {
		t.Fatal(err)
	}
	mutation.ExpectedRevision = 1
	_, lease, err := f.store.ReserveTask(ctx, domain.TaskReservation{ID: "related-attempt", TaskID: "related", LaunchIntentID: "related-launch", HolderID: "scheduler", Mutation: mutation, Now: time.Now().UTC(), TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.TaskLease = &lease.TaskLeaseToken
	dependency, err := f.store.GetTaskRevision(ctx, "task", 1)
	if err != nil {
		t.Fatal(err)
	}
	dependency.Definition.Brief = "Changed dependency"
	if _, err := f.store.ReviseAdaptiveTask(ctx, "task", dependency.Definition, mutation); err != nil {
		t.Fatal(err)
	}
	parent, err := f.store.GetTaskRevision(ctx, "parent", 1)
	if err != nil {
		t.Fatal(err)
	}
	parent.Definition.Brief = "Current parent"
	if _, err := f.store.ReviseAdaptiveTask(ctx, "parent", parent.Definition, mutation); err != nil {
		t.Fatal(err)
	}
	criteria.Criteria[0].Requirement = "Changed requirement"
	if _, err := f.store.ReviseAcceptanceCriteria(ctx, "related", criteria, mutation); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := f.manager.Spawn(ctx, f.cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := f.store.GetTaskContext(ctx, lease.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Task.Revision != 1 || snapshot.CriteriaVersion != 1 || !strings.Contains(snapshot.Prompt, "Original requirement") || strings.Contains(snapshot.Prompt, "Changed requirement") || strings.Contains(snapshot.Prompt, "Changed dependency") || !strings.Contains(snapshot.Prompt, "Current parent") {
		t.Fatalf("mixed historical planning: %+v", snapshot)
	}
	for _, source := range snapshot.Sources {
		if source.Kind == "dependency" && source.Version != 1 {
			t.Fatal("dependency drifted")
		}
		if source.Kind == "parent" && source.Version != 2 {
			t.Fatal("parent provenance missing")
		}
	}
}

type failingContextStore struct{ taskcontext.Store }

func (f failingContextStore) SaveTaskContext(context.Context, domain.TaskLeaseToken, domain.TaskContextSnapshot) error {
	return errors.New("injected seal failure")
}

func TestTaskContextSealFailureCannotLaunchOrReleaseNativeReservation(t *testing.T) {
	ctx := context.Background()
	f := newTaskWorkerFixture(t, domain.SessionModeTUI)
	f.manager.taskContexts = taskcontext.New(failingContextStore{f.store})
	if _, _, _, err := f.manager.Spawn(ctx, f.cfg); err == nil || f.runtime.created != 0 {
		t.Fatalf("unsealed context launched: %v", err)
	}
	dispatch, found, err := f.store.GetTaskWorkerDispatch(ctx, f.lease.AttemptID)
	if err != nil || !found {
		t.Fatalf("lost launch association: %v %v", found, err)
	}
	if _, pending, err := f.store.PendingTaskExecution(ctx, dispatch.SessionID); err != nil || !pending {
		t.Fatalf("lost execution guard: %v %v", pending, err)
	}
	if _, found, err := f.store.GetTaskContext(ctx, f.lease.AttemptID); err != nil || found {
		t.Fatalf("failed seal persisted: %v %v", found, err)
	}
}
