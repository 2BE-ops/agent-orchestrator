package sessionmanager

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/sessionguard"
)

func taskMessageTransportFixture(t *testing.T, mode domain.SessionMode) (taskWorkerFixture, *fakeMessenger, domain.TaskMessageDelivery, domain.TaskMessage) {
	t.Helper()
	f := newTaskWorkerFixture(t, mode)
	rec, _, _, err := f.manager.Spawn(context.Background(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec.Activity.State, rec.FirstSignalAt = domain.ActivityIdle, time.Now().UTC()
	if err := f.store.UpdateSession(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	messenger := &fakeMessenger{}
	f.manager.messenger = sessionguard.New(f.store, messenger, nil)
	f.launcher.live = true
	message := domain.TaskMessage{ID: "message", ProjectID: rec.ProjectID, TaskID: "source", Definition: domain.TaskMessageDefinition{SchemaVersion: 1, Kind: "finding", TargetTaskID: "task", Subject: "Observation", Body: "Unverified worker observation <ao-test>", CorrelationID: "thread"}}
	_, message.ContentHash, err = domain.TaskContent(message.Definition)
	if err != nil {
		t.Fatal(err)
	}
	delivery := domain.TaskMessageDelivery{ID: "delivery", MessageID: message.ID, State: "dispatching", SessionID: rec.ID, Owner: rec.ControllerOwner(), DeliveryKey: "adaptive-message:" + message.ID}
	return f, messenger, delivery, message
}

func TestTaskMessageTransportUsesGuardedTUIAndIdempotentChatRelay(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			f, messenger, delivery, message := taskMessageTransportFixture(t, mode)
			if ready, err := f.manager.TaskMessageTargetReady(context.Background(), delivery.SessionID); err != nil || !ready {
				t.Fatalf("ready: %v %v", ready, err)
			}
			messenger.onSend = func(domain.SessionID, string) {
				if !f.manager.SessionMutationInProgress(delivery.SessionID) {
					t.Fatal("native write escaped operation fence")
				}
				if err := f.manager.beginAgentOperation(context.Background(), delivery.SessionID, agentOperationRestore); !errors.Is(err, errAgentOperationInProgress) {
					t.Fatalf("restore raced coordination: %v", err)
				}
			}
			result := f.manager.DeliverTaskMessage(context.Background(), delivery, message)
			if result.State != "handed_off" || f.manager.SessionMutationInProgress(delivery.SessionID) {
				t.Fatalf("delivery/fence: %+v", result)
			}
			var prompt string
			if mode == domain.SessionModeChat {
				if len(messenger.msgs) != 0 || len(f.launcher.relayIDs) != 1 || f.launcher.relayIDs[0] != delivery.DeliveryKey {
					t.Fatalf("incorrect Chat boundary: %+v", f.launcher.relayIDs)
				}
				prompt = f.launcher.relayed[0]
			} else {
				if len(messenger.msgs) != 1 || len(f.launcher.relayed) != 0 {
					t.Fatalf("incorrect TUI boundary: %+v", messenger.msgs)
				}
				prompt = messenger.msgs[0]
			}
			if !strings.Contains(prompt, "Preserve your own pinned task") || !strings.Contains(prompt, `"id":"message"`) || strings.Contains(prompt, "<ao-test>") {
				t.Fatalf("unattributed/unescaped coordination: %s", prompt)
			}
		})
	}
}

func TestTaskMessageTransportRefusesStaleBusyBlockedAndUnknownRecipients(t *testing.T) {
	for _, reason := range []string{"stale", "busy", "blocked", "unknown_probe", "gate", "hash", "generation_exits_before_write"} {
		t.Run(reason, func(t *testing.T) {
			f, messenger, delivery, message := taskMessageTransportFixture(t, domain.SessionModeTUI)
			rec, _, err := f.store.GetSession(context.Background(), delivery.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			switch reason {
			case "stale":
				delivery.Owner.RuntimeLaunchID = "old"
			case "busy":
				rec.Activity.State = domain.ActivityActive
			case "blocked":
				rec.Activity.State = domain.ActivityBlocked
			case "unknown_probe":
				f.runtime.supervisedErr = errors.New("probe unavailable")
			case "gate":
				if err := f.manager.beginAgentOperation(context.Background(), rec.ID, agentOperationRestore); err != nil {
					t.Fatal(err)
				}
				defer f.manager.endAgentOperation(rec.ID, agentOperationRestore)
			case "hash":
				message.Definition.Body = "Changed content"
			case "generation_exits_before_write":
				f.runtime.supervisedSequence = []bool{true, false}
			}
			if err := f.store.UpdateSession(context.Background(), rec); err != nil {
				t.Fatal(err)
			}
			result := f.manager.DeliverTaskMessage(context.Background(), delivery, message)
			if result.State != "not_sent" || len(messenger.msgs) != 0 {
				t.Fatalf("unsafe recipient reached transport: %+v %+v", result, messenger.msgs)
			}
		})
	}
}

func TestTaskMessageTransportDistinguishesPartialWriteFromRefusal(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			f, messenger, delivery, message := taskMessageTransportFixture(t, mode)
			messenger.err = errors.New("write failed after paste")
			f.launcher.turnErr = errors.New("provider response lost")
			result := f.manager.DeliverTaskMessage(context.Background(), delivery, message)
			if result.State != "uncertain" {
				t.Fatalf("ambiguous native write was retryable: %+v", result)
			}
		})
	}
}
