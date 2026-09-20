package sessionmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/sessionguard"
)

func nativeManagerDeliveryFixture(t *testing.T, mode domain.SessionMode) (nativeManagerFixture, *fakeMessenger, domain.AgentManagerDelivery, domain.AgentManagerContext) {
	t.Helper()
	ctx := context.Background()
	f := newNativeManagerFixture(t, mode)
	rec, _, _, err := f.manager.Spawn(ctx, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec.Activity.State, rec.FirstSignalAt = domain.ActivityIdle, time.Now().UTC()
	if err := f.store.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	criteria := domain.AcceptanceCriteria{Criteria: []domain.AcceptanceCriterion{{ID: "checked", Requirement: "Independent verification", EvidenceKind: "manual"}}}
	_, err = f.store.CreateAdaptiveTask(ctx, "native-route", rec.ProjectID, domain.TaskDefinition{Title: "Route work", Brief: "Exact sealed native input <data>", MaxAttempts: 1}, &criteria, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Plan task"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = f.store.EnqueueAgentManagerRequest(ctx, domain.AgentManagerEnqueue{ID: "native-request", ProjectID: rec.ProjectID, TaskID: "native-route", TaskRevision: 1, ConfigurationVersion: 1, Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Choose worker", Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	delivery, created, err := f.store.BeginAgentManagerDelivery(ctx, domain.AgentManagerContextSeal{ID: "native-context", ProjectID: rec.ProjectID, RequestID: "native-request", SessionID: rec.ID, SourceOwner: rec.ControllerOwner(), Now: time.Now().UTC()}, "native-send")
	if err != nil || !created {
		t.Fatalf("reservation: %+v %v", delivery, err)
	}
	sealed, err := f.store.GetAgentManagerContext(ctx, rec.ProjectID, "native-request", delivery.ContextID)
	if err != nil {
		t.Fatal(err)
	}
	messenger := &fakeMessenger{}
	f.manager.messenger = sessionguard.New(f.store, messenger, nil)
	f.launcher.live = true
	return f, messenger, delivery, sealed
}

func TestAgentManagerDeliveryUsesExactSealedInputThroughBothNativeModes(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			f, messenger, delivery, sealed := nativeManagerDeliveryFixture(t, mode)
			if ready, err := f.manager.AgentManagerTargetReady(ctx, delivery.SessionID); err != nil || !ready {
				t.Fatalf("readiness: %v %v", ready, err)
			}
			if ready, err := f.manager.TaskMessageTargetReady(ctx, delivery.SessionID); err != nil || ready {
				t.Fatalf("Manager accepted worker transport role: %v %v", ready, err)
			}
			messenger.onSend = func(domain.SessionID, string) {
				if !f.manager.SessionMutationInProgress(delivery.SessionID) {
					t.Fatal("native write escaped operation guard")
				}
				if err := f.manager.beginAgentOperation(ctx, delivery.SessionID, agentOperationRestore); !errors.Is(err, errAgentOperationInProgress) {
					t.Fatalf("restore raced native input: %v", err)
				}
			}
			result := f.manager.DeliverAgentManagerContext(ctx, delivery, sealed)
			if result.State != "handed_off" || f.manager.SessionMutationInProgress(delivery.SessionID) {
				t.Fatalf("native result: %+v", result)
			}
			if mode == domain.SessionModeChat {
				if len(f.launcher.relayed) != 1 || f.launcher.relayed[0] != sealed.Prompt || f.launcher.relayIDs[0] != delivery.DeliveryKey || len(messenger.msgs) != 0 {
					t.Fatal("Chat did not receive exact sealed input and stable key")
				}
			} else if len(messenger.msgs) != 1 || messenger.msgs[0] != sealed.Prompt || len(f.launcher.relayed) != 0 {
				t.Fatal("TUI did not receive exact sealed input")
			}
			if err := f.store.ResolveAgentManagerDelivery(ctx, domain.AgentManagerDeliveryResolution{ID: delivery.ID, State: result.State, Reason: result.Reason}); err != nil {
				t.Fatal(err)
			}
			if retry := f.manager.DeliverAgentManagerContext(ctx, delivery, sealed); retry.State != "not_sent" {
				t.Fatalf("resolved claim repeated native input: %+v", retry)
			}
		})
	}
}

func TestAgentManagerDeliveryRefusesAlteredStaleAndUnavailableInput(t *testing.T) {
	ctx := context.Background()
	for _, reason := range []string{"hash", "forged receipt", "owner", "busy", "unknown probe", "generation exits", "gate", "unreserved", "cancelled task", "disabled policy"} {
		t.Run(reason, func(t *testing.T) {
			f, messenger, delivery, sealed := nativeManagerDeliveryFixture(t, domain.SessionModeTUI)
			rec, _, err := f.store.GetSession(ctx, delivery.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			switch reason {
			case "hash":
				sealed.Prompt += " changed"
			case "forged receipt":
				sealed.Policy.Optimization = "speed"
				sealed.Prompt, _ = sealed.RenderPrompt()
				sealed.ContentHash = sealed.Hash()
			case "owner":
				delivery.Owner.RuntimeLaunchID = "stale"
			case "busy":
				rec.Activity.State = domain.ActivityActive
			case "unknown probe":
				f.runtime.supervisedErr = errors.New("unknown native probe")
			case "generation exits":
				f.runtime.supervisedSequence = []bool{true, false}
			case "gate":
				if err := f.manager.beginAgentOperation(ctx, rec.ID, agentOperationRestore); err != nil {
					t.Fatal(err)
				}
				defer f.manager.endAgentOperation(rec.ID, agentOperationRestore)
			case "unreserved":
				delivery.ID = "invented-claim"
			case "cancelled task":
				if _, err := f.store.ChangeTaskIntent(ctx, "native-route", domain.TaskIntentChange{Intent: "cancel", Mutation: domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Cancel before delivery", ExpectedRevision: 1}}); err != nil {
					t.Fatal(err)
				}
			case "disabled policy":
				configuration, err := f.store.GetAgentManager(ctx, rec.ProjectID)
				if err != nil {
					t.Fatal(err)
				}
				configuration.Definition.Enabled = false
				if _, err := f.store.ConfigureAgentManager(ctx, rec.ProjectID, configuration.Definition, domain.TaskMutation{Actor: configuration.Actor, Reason: "Disable before delivery", ExpectedRevision: 1}); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.store.UpdateSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			result := f.manager.DeliverAgentManagerContext(ctx, delivery, sealed)
			if result.State != "not_sent" || len(messenger.msgs) != 0 {
				t.Fatalf("invalid input reached runtime: %+v", result)
			}
		})
	}
}

func TestAgentManagerDeliveryKeepsPartialWritesUncertain(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			f, messenger, delivery, sealed := nativeManagerDeliveryFixture(t, mode)
			messenger.err = errors.New("partial runtime write")
			f.launcher.turnErr = errors.New("lost provider response")
			if result := f.manager.DeliverAgentManagerContext(context.Background(), delivery, sealed); result.State != "uncertain" {
				t.Fatalf("ambiguous send was retryable: %+v", result)
			}
		})
	}
}

func TestAgentManagerChatDeliveryRechecksPolicyAfterReservation(t *testing.T) {
	ctx := context.Background()
	f, _, delivery, sealed := nativeManagerDeliveryFixture(t, domain.SessionModeChat)
	configuration, err := f.store.GetAgentManager(ctx, "manager-project")
	if err != nil {
		t.Fatal(err)
	}
	configuration.Definition.Enabled = false
	if _, err := f.store.ConfigureAgentManager(ctx, "manager-project", configuration.Definition, domain.TaskMutation{Actor: configuration.Actor, Reason: "Disable after reservation", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	result := f.manager.DeliverAgentManagerContext(ctx, delivery, sealed)
	if result.State != "not_sent" || len(f.launcher.relayed) != 0 {
		t.Fatalf("disabled Manager received Chat input: %+v", result)
	}
}
