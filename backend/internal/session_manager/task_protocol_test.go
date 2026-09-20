package sessionmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

func TestTaskWorkerLaunchProtocolSubmitsThroughSharedServices(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			f := newTaskWorkerFixture(t, mode)
			// These characters must remain literal argv data in both native modes.
			executable := "C:/Program Files/AO/ao`$(literal)<tool>.exe"
			f.manager.executable = func() (string, error) { return executable, nil }
			f.manager.runFilePath = "C:/AO lab/isolated-running.json"
			rec, _, _, err := f.manager.Spawn(ctx, f.cfg)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, found, err := f.store.GetTaskContext(ctx, f.lease.AttemptID)
			if err != nil || !found {
				t.Fatalf("context missing: %v %v", found, err)
			}
			_, body, found := strings.Cut(snapshot.BasePrompt, "AO_WORKER_OUTPUT_JSON\n")
			if !found {
				t.Fatal("native worker did not receive submission instructions")
			}
			var protocol struct {
				SessionID   domain.SessionID    `json:"sessionId"`
				Commands    map[string][]string `json:"commands"`
				Environment map[string]string   `json:"environment"`
				Result      task.ResultInput    `json:"resultExample"`
				Message     task.MessageInput   `json:"messageExample"`
			}
			if err := json.Unmarshal([]byte(body), &protocol); err != nil {
				t.Fatal(err)
			}
			if protocol.SessionID != rec.ID || protocol.Result.SourceGeneration != snapshot.ExecutionOperationID || protocol.Message.SourceGeneration != snapshot.ExecutionOperationID {
				t.Fatal("incorrect native submission identity")
			}
			if protocol.Environment["AO_RUN_FILE"] != f.manager.runFilePath {
				t.Fatal("output protocol could route to another daemon")
			}
			for _, command := range protocol.Commands {
				if command[0] != executable {
					t.Fatalf("executable changed: %v", command)
				}
			}
			if strings.Contains(body, "<tool>") {
				t.Fatal("unescaped protocol data")
			}
			if mode == domain.SessionModeTUI && f.manager.agents.(singleAgent).agent.(*recordingAgent).lastLaunch.Prompt != snapshot.Prompt {
				t.Fatal("TUI missed protocol")
			}
			if mode == domain.SessionModeChat && (len(f.launcher.turns) != 1 || f.launcher.turns[0] != snapshot.Prompt) {
				t.Fatal("Chat missed protocol")
			}
			service := task.New(f.store)
			protocol.Result.Definition.Summary = "Actual native worker claim"
			receipt, err := service.SubmitResult(ctx, rec.ID, protocol.Result)
			if err != nil || !receipt.Created || receipt.Result.ContextHash != snapshot.ContentHash {
				t.Fatalf("documented result request rejected: %+v %v", receipt, err)
			}
			if retry, err := service.SubmitResult(ctx, rec.ID, protocol.Result); err != nil || retry.Created || retry.Result.ID != receipt.Result.ID {
				t.Fatalf("documented result retry duplicated: %+v %v", retry, err)
			}
			if _, err := f.store.CreateAdaptiveTask(ctx, "recipient", rec.ProjectID, domain.TaskDefinition{Title: "Recipient", Brief: "Receive coordination", MaxAttempts: 1}, nil, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Plan related task"}); err != nil {
				t.Fatal(err)
			}
			protocol.Message.Definition.TargetTaskID = "recipient"
			protocol.Message.Definition.ResultID = receipt.Result.ID
			message, err := service.SubmitMessage(ctx, rec.ID, protocol.Message)
			if err != nil || !message.Created || message.Message.NativeGeneration != protocol.Message.SourceGeneration {
				t.Fatalf("documented message request rejected: %+v %v", message, err)
			}
			if retry, err := service.SubmitMessage(ctx, rec.ID, protocol.Message); err != nil || retry.Created || retry.Message.ID != message.Message.ID {
				t.Fatalf("documented message retry duplicated: %+v %v", retry, err)
			}
			if _, active, err := f.store.GetActiveTaskLease(ctx, "task"); err != nil || !active {
				t.Fatalf("worker claims released ownership: %v %v", active, err)
			}
			// A retained historical prompt must never discover replacement authority.
			if mode == domain.SessionModeTUI {
				rec.Metadata.RuntimeLaunchID = "replacement"
			} else {
				rec.Metadata.ControllerGeneration = "replacement"
			}
			if err := f.store.UpdateSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			protocol.Result.IdempotencyKey, protocol.Result.ExpectedVersion = "new-result", 1
			if _, err := service.SubmitResult(ctx, rec.ID, protocol.Result); err == nil {
				t.Fatal("stale launch instructions submitted as replacement")
			}
			protocol.Message.IdempotencyKey = "new-message"
			if _, err := service.SubmitMessage(ctx, rec.ID, protocol.Message); err == nil {
				t.Fatal("stale launch instructions sent as replacement")
			}
		})
	}
}

func TestTaskWorkerProtocolCannotBypassPromptOrExecutableBounds(t *testing.T) {
	for _, kind := range []string{"combined_prompt", "invalid_executable", "empty_executable"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f := newTaskWorkerFixture(t, domain.SessionModeTUI)
			switch kind {
			case "combined_prompt":
				f.cfg.Prompt = strings.Repeat("x", (64<<10)-1024)
			case "invalid_executable":
				f.manager.executable = func() (string, error) { return "ao\nother-command", nil }
			case "empty_executable":
				f.manager.executable = func() (string, error) { return "", nil }
			}
			if _, _, _, err := f.manager.Spawn(ctx, f.cfg); err == nil {
				t.Fatal("invalid protocol admitted a worker")
			}
			if f.runtime.created != 0 || len(f.launcher.started) != 0 {
				t.Fatal("protocol failed after native execution")
			}
			if _, found, err := f.store.GetTaskContext(ctx, f.lease.AttemptID); err != nil || found {
				t.Fatalf("invalid protocol sealed: %v %v", found, err)
			}
			if _, active, err := f.store.GetActiveTaskLease(ctx, "task"); err != nil || !active {
				t.Fatalf("failed launch lost reservation: %v %v", active, err)
			}
		})
	}
}
