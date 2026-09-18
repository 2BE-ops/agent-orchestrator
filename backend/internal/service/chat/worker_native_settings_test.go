package chat_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
)

type workerNativeConversation struct {
	*fakeConversation
	model, effort   string
	fast            bool
	calls           int
	failRollback    bool
	afterApplyError error
	onSet           func()
}

func newWorkerNativeConversation() *workerNativeConversation {
	return &workerNativeConversation{fakeConversation: newFakeConversation(), model: "first", effort: "high"}
}
func (c *workerNativeConversation) ListConfigOptions(context.Context) ([]ports.ChatConfigOption, error) {
	fast := c.fast
	return []ports.ChatConfigOption{
		{ID: "model", Type: ports.ChatConfigOptionSelect, Current: ports.ChatConfigOptionValue{Select: c.model}, Choices: []ports.ChatConfigOptionChoice{{Value: "first"}, {Value: "second"}, {Value: "not-in-native-catalog"}}},
		{ID: "effort", Type: ports.ChatConfigOptionSelect, Current: ports.ChatConfigOptionValue{Select: c.effort}, Choices: []ports.ChatConfigOptionChoice{{Value: "high"}, {Value: "low"}}},
		{ID: "fast", Type: ports.ChatConfigOptionBoolean, Current: ports.ChatConfigOptionValue{Boolean: &fast}},
	}, nil
}
func (c *workerNativeConversation) SetConfigOption(ctx context.Context, id string, value ports.ChatConfigOptionValue) ([]ports.ChatConfigOption, error) {
	c.calls++
	if c.onSet != nil {
		c.onSet()
	}
	if c.failRollback && id == "model" && value.Select == "first" {
		return nil, errors.New("provider cannot restore model")
	}
	switch id {
	case "model":
		c.model = value.Select
		if c.model == "second" {
			c.effort = "low"
		} else {
			c.effort = "high"
		}
	case "effort":
		c.effort = value.Select
	case "fast":
		c.fast = *value.Boolean
	}
	if c.afterApplyError != nil {
		err := c.afterApplyError
		c.afterApplyError = nil
		return nil, err
	}
	return c.ListConfigOptions(ctx)
}

func TestWorkerNativeSettingsValidateBeforeMutationAndRestoreExactControls(t *testing.T) {
	ctx := context.Background()
	conv := newWorkerNativeConversation()
	st, svc, rec, original := configuredSettingsWorker(t, conv)
	for _, input := range []struct {
		id    string
		value ports.ChatConfigOptionValue
	}{{"missing", ports.ChatConfigOptionValue{Select: "x"}}, {"model", ports.ChatConfigOptionValue{Select: "not-in-native-catalog"}}, {"fast", ports.ChatConfigOptionValue{Select: "true"}}} {
		if _, err := svc.SetConfigOption(ctx, rec.ID, input.id, input.value); err == nil {
			t.Fatalf("invalid control accepted: %+v", input)
		}
	}
	if conv.calls != 0 {
		t.Fatal("invalid request reached provider")
	}
	conv.onSet = func() {
		if _, found, err := st.PendingWorkerNativeChange(ctx, rec.ID); err != nil || !found {
			t.Fatalf("provider mutated before durable intent: %v", err)
		}
	}
	if _, err := svc.SetConfigOption(ctx, rec.ID, "model", ports.ChatConfigOptionValue{Select: "second"}); err != nil {
		t.Fatal(err)
	}
	fast := true
	if _, err := svc.SetConfigOption(ctx, rec.ID, "fast", ports.ChatConfigOptionValue{Boolean: &fast}); err != nil {
		t.Fatal(err)
	}
	current, _, found, err := st.GetEffectiveWorkerConfiguration(ctx, rec.ID)
	if err != nil || !found || current.Effective.Config.Model != "second" || current.Effective.Config.Effort != "low" || len(current.NativeOptions) != 3 || current.Effective.Instructions != original.Effective.Instructions {
		t.Fatalf("native configuration lost: %+v %v", current, err)
	}
	if _, pending, err := st.PendingWorkerNativeChange(ctx, rec.ID); err != nil || pending {
		t.Fatalf("successful operation unresolved: %v", err)
	}
	if _, err := svc.SetTurnSettings(ctx, rec.ID, domain.ConversationSettings{Model: "first"}); err == nil {
		t.Fatal("portable endpoint bypassed native controls")
	}
	if err := svc.Stop(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	replacement := newWorkerNativeConversation()
	restarted := chatsvc.New(chatsvc.Options{Store: st, Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: replacement}}, Log: slog.New(slog.DiscardHandler), NewID: uuid.NewString})
	restarted.SetWorkerConfigurationResolver(registry.NewWithNative(st, workerSettingsNative{}))
	t.Cleanup(func() { _ = restarted.Stop(ctx, rec.ID) })
	if _, err := restarted.Start(ctx, chatsvc.StartConfig{SessionID: rec.ID, ProjectID: rec.ProjectID, Harness: rec.Harness, WorkspacePath: t.TempDir(), ProviderConversationID: "thread-1", Permissions: domain.PermissionModeAuto}); err != nil {
		t.Fatal(err)
	}
	if replacement.model != "second" || replacement.effort != "low" || !replacement.fast {
		t.Fatalf("native defaults replaced retained controls: %+v", replacement)
	}
	if err := restarted.Stop(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	preparedTarget := newWorkerNativeConversation()
	target := chatsvc.New(chatsvc.Options{Store: st, Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: preparedTarget}}, Log: slog.New(slog.DiscardHandler), NewID: uuid.NewString})
	t.Cleanup(func() { _ = target.Stop(ctx, rec.ID) })
	empty := []domain.WorkerNativeOption{}
	if _, err := target.Start(ctx, chatsvc.StartConfig{SessionID: rec.ID, ProjectID: rec.ProjectID, Harness: rec.Harness, WorkspacePath: t.TempDir(), ProviderConversationID: "thread-1", Permissions: domain.PermissionModeAuto, WorkerNativeOptions: &empty}); err != nil {
		t.Fatal(err)
	}
	if preparedTarget.calls != 0 || preparedTarget.fast {
		t.Fatal("active source controls overwrote prepared target defaults")
	}
	launch, _, err := st.GetWorkerConfiguration(ctx, rec.ID)
	if err != nil || launch.ContentHash != original.ContentHash {
		t.Fatalf("launch changed: %v", err)
	}
}

func TestWorkerNativeSettingsCompensateDatabaseFailureAndFenceUnknownOutcome(t *testing.T) {
	for _, recovery := range []string{"compensate", "retry", "restart"} {
		t.Run(recovery, func(t *testing.T) {
			ctx := context.Background()
			conv := newWorkerNativeConversation()
			st, svc, rec, original := configuredSettingsWorker(t, conv)
			project, _, err := st.GetProject(ctx, string(testProject))
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", "file:"+filepath.Join(project.Path, "ao.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_native_resolution BEFORE INSERT ON adaptive_worker_native_resolutions WHEN NEW.outcome='applied' BEGIN SELECT RAISE(ABORT,'injected resolution failure'); END;`); err != nil {
				t.Fatal(err)
			}
			conv.failRollback = recovery != "compensate"
			if _, err := svc.SetConfigOption(ctx, rec.ID, "model", ports.ChatConfigOptionValue{Select: "second"}); err == nil {
				t.Fatal("resolution failure accepted")
			}
			current, sequence, _, err := st.GetEffectiveWorkerConfiguration(ctx, rec.ID)
			if err != nil || sequence != 0 || current.ContentHash != original.ContentHash {
				t.Fatalf("failed native commit escaped rollback: %v", err)
			}
			stored, err := st.ConversationForSession(ctx, rec.ID)
			if err != nil || stored.Settings.Model != "first" {
				t.Fatalf("failed settings escaped rollback: %+v %v", stored.Settings, err)
			}
			pending, found, err := st.PendingWorkerNativeChange(ctx, rec.ID)
			if err != nil || found != conv.failRollback {
				t.Fatalf("wrong recovery state: %v %v", found, err)
			}
			for _, statement := range []string{`UPDATE adaptive_worker_native_changes SET requested='{}'`, `DELETE FROM adaptive_worker_native_changes`} {
				if _, err := db.ExecContext(ctx, statement); err == nil {
					t.Fatalf("mutable native intent: %s", statement)
				}
			}
			if recovery == "compensate" {
				for _, statement := range []string{`UPDATE adaptive_worker_native_resolutions SET reason='rewritten'`, `DELETE FROM adaptive_worker_native_resolutions`} {
					if _, err := db.ExecContext(ctx, statement); err == nil {
						t.Fatalf("mutable native resolution: %s", statement)
					}
				}
				if conv.model != "first" || conv.effort != "high" {
					t.Fatal("previous controls not restored")
				}
				return
			}
			controller, err := svc.Controller(rec.ID)
			if err != nil {
				t.Fatal(err)
			}
			duplicate := pending
			duplicate.ID = "overlapping-native-change"
			if err := st.BeginWorkerNativeChange(ctx, duplicate); !errors.Is(err, ports.ErrRegistryConflict) {
				t.Fatalf("overlapping native intent accepted: %v", err)
			}
			wrongOwner := pending.Owner
			wrongOwner.ProviderConversationID = "foreign-thread"
			if err := st.RevertWorkerNativeChange(ctx, wrongOwner, pending.ID, "invalid recovery"); !errors.Is(err, ports.ErrRegistryConflict) {
				t.Fatalf("foreign recovery accepted: %v", err)
			}
			if _, err := controller.Send(ctx, ports.ChatUserMessage{Text: "must not dispatch"}); err == nil {
				t.Fatal("unresolved native state accepted a turn")
			}
			if _, err := st.PrepareWorkerExecution(ctx, pending.Owner, domain.WorkerExecution{ID: "blocked", SessionID: rec.ID, SourceKind: "conversation_settings", SourceID: "blocked", Configuration: original, Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Unsafe overlap", CreatedAt: pending.CreatedAt}); !errors.Is(err, ports.ErrRegistryConflict) {
				t.Fatalf("unresolved state admitted another execution: %v", err)
			}
			if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_native_resolution`); err != nil {
				t.Fatal(err)
			}
			conv.failRollback = false
			if recovery == "retry" {
				if _, err := st.AppendUserMessage(ctx, stored.ID, rec.ID, controller.Generation(), domain.ConversationMessage{ID: "waiting-message", Text: "queued before recovery", Origin: domain.MessageOriginHuman, ClientMessageID: "waiting-client"}, "waiting-turn", time.Now()); err != nil {
					t.Fatal(err)
				}
				if _, err := svc.SetConfigOption(ctx, rec.ID, "model", ports.ChatConfigOptionValue{Select: "second"}); err != nil {
					t.Fatal(err)
				}
				if texts := conv.sentTexts(); len(texts) != 1 || texts[0] != "queued before recovery" {
					t.Fatalf("recovery did not resume accepted queue: %v", texts)
				}
			} else {
				if err := svc.Stop(ctx, rec.ID); err != nil {
					t.Fatal(err)
				}
				replacement := newWorkerNativeConversation()
				replacement.model, replacement.effort = "second", "low"
				restarted := chatsvc.New(chatsvc.Options{Store: st, Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: replacement}}, Log: slog.New(slog.DiscardHandler), NewID: uuid.NewString})
				t.Cleanup(func() { _ = restarted.Stop(ctx, rec.ID) })
				if _, err := restarted.Start(ctx, chatsvc.StartConfig{SessionID: rec.ID, ProjectID: rec.ProjectID, Harness: rec.Harness, WorkspacePath: t.TempDir(), ProviderConversationID: "thread-1", Permissions: domain.PermissionModeAuto}); err != nil {
					t.Fatal(err)
				}
				if replacement.model != "first" || replacement.effort != "high" {
					t.Fatal("restart published unconfirmed native settings")
				}
			}
			if _, found, err := st.PendingWorkerNativeChange(ctx, rec.ID); err != nil || found {
				t.Fatalf("recovery did not resolve intent: %v", err)
			}
		})
	}
}

func TestWorkerNativeSettingsCancelledResponseRestoresPreviousControls(t *testing.T) {
	ctx := context.Background()
	conv := newWorkerNativeConversation()
	st, svc, rec, _ := configuredSettingsWorker(t, conv)
	conv.afterApplyError = context.Canceled
	if _, err := svc.SetConfigOption(ctx, rec.ID, "model", ports.ChatConfigOptionValue{Select: "second"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if conv.model != "first" || conv.effort != "high" {
		t.Fatal("cancelled mutation was left applied")
	}
	if _, found, err := st.PendingWorkerNativeChange(ctx, rec.ID); err != nil || found {
		t.Fatalf("confirmed compensation unresolved: %v", err)
	}
}
