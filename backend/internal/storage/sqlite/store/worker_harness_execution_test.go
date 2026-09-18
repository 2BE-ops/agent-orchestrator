package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestWorkerExecutionHarnessActivationSharesOwnershipTransaction(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			s := sqlitetest.MustOpenAt(t, dir)
			rec, snapshot := workerSnapshot(t, s)
			now := time.Now().UTC()
			rec.Mode = mode
			snapshot.Effective.SessionMode = mode
			snapshot.ContentHash = snapshot.Hash()
			if mode == domain.SessionModeChat {
				rec.Metadata.ControllerGeneration = "source-generation"
				rec.Metadata.ProviderConversationID = "source-native"
			} else {
				rec.Metadata.RuntimeLaunchID = "source-generation"
				rec.Metadata.RuntimeHandleID = "source-runtime"
			}
			rec, err := s.CreateConfiguredSession(ctx, rec, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if mode == domain.SessionModeChat {
				if _, err := s.CreateConversation(ctx, "conversation", domain.ConversationScopeSession, "", rec.ID, now); err != nil {
					t.Fatal(err)
				}
			}
			target := domain.HarnessClaudeCode
			if rec.Harness == target {
				target = domain.HarnessCodex
			}
			sw, created, err := s.CreateAgentSwitch(ctx, domain.AgentSwitch{ID: "configured-switch", SessionID: rec.ID, IdempotencyKey: "configured-switch", RequestFingerprint: domain.ComputeAgentSwitchRequestFingerprint(rec.ID, target, "target-model"), FromHarness: rec.Harness, TargetHarness: target, State: domain.AgentSwitchPreparingHandoff, TargetStartMode: domain.AgentSwitchTargetStartPending, AgentHandoffStatus: domain.AgentHandoffNotAttempted, SourceGenerationID: "source-generation", RequestedAt: now, UpdatedAt: now})
			if err != nil || !created {
				t.Fatalf("switch: %v %v", created, err)
			}
			changed := snapshot
			changed.Effective.Harness = target
			changed.Effective.Config.Model = "target-model"
			changed.Selection.Overrides.Harness = &target
			changed.ContentHash = changed.Hash()
			execution, err := s.PrepareWorkerExecution(ctx, rec.ControllerOwner(), domain.WorkerExecution{ID: "target-execution", SessionID: rec.ID, SourceKind: "agent_switch", SourceID: string(sw.ID), Configuration: changed, Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Select target harness", CreatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			advanceAgentSwitchFixtureWithMutation(ctx, t, s, &sw, domain.AgentSwitchStoppingSource, now.Add(time.Second), func(next *domain.AgentSwitch) {
				next.TargetStartMode = domain.AgentSwitchTargetStartFresh
				next.TargetGenerationID = "target-generation"
			})
			confirmation := domain.AgentSwitchSourceStopConfirmation{SwitchID: sw.ID, SessionID: rec.ID, SourceMode: mode, SourceHarness: rec.Harness, SourceGenerationID: "source-generation", TargetGenerationID: "target-generation", StoppedAt: now.Add(2 * time.Second)}
			if mode == domain.SessionModeChat {
				confirmation.ExpectedSourceControllerGeneration = "source-generation"
			} else {
				confirmation.ExpectedSourceRuntimeLaunchID = "source-generation"
			}
			if ok, err := s.ConfirmAgentSwitchSourceStopped(ctx, confirmation); err != nil || !ok {
				t.Fatalf("stop source: %v %v", ok, err)
			}
			native := domain.AgentNativeSession{ID: "target-native-ref", AOSessionID: rec.ID, Harness: target, NativeSessionID: "target-native", LastGenerationID: "target-generation", CreatedAt: now.Add(3 * time.Second), LastUsedAt: now.Add(3 * time.Second)}
			if _, _, err := s.CreateAgentNativeSession(ctx, native); err != nil {
				t.Fatal(err)
			}
			sw, _, err = s.GetAgentSwitch(ctx, sw.ID)
			if err != nil {
				t.Fatal(err)
			}
			advanceAgentSwitchFixtureWithMutation(ctx, t, s, &sw, domain.AgentSwitchStartingTarget, now.Add(3*time.Second), func(next *domain.AgentSwitch) {
				next.TargetNativeSessionRef = &native.ID
				if mode == domain.SessionModeTUI {
					next.TargetRuntimeHandleID = "target-runtime"
				}
			})
			activate := func() (bool, error) {
				if mode == domain.SessionModeChat {
					return s.ActivateChatAgentSwitchTarget(ctx, domain.AgentSwitchChatTargetActivation{SwitchID: sw.ID, SessionID: rec.ID, SourceHarness: rec.Harness, SourceGenerationID: "source-generation", ExpectedSourceControllerGeneration: "source-generation", TargetHarness: target, TargetNativeSessionRef: native.ID, TargetGenerationID: "target-generation", ProviderConversationID: "target-native", ControllerGeneration: "target-generation", ActivatedAt: now.Add(4 * time.Second)})
				}
				return s.ActivateAgentSwitchTarget(ctx, domain.AgentSwitchTargetActivation{SwitchID: sw.ID, SessionID: rec.ID, SourceHarness: rec.Harness, SourceGenerationID: "source-generation", ExpectedSourceRuntimeLaunchID: "source-generation", TargetHarness: target, TargetNativeSessionRef: native.ID, TargetGenerationID: "target-generation", RuntimeHandleID: "target-runtime", ActivatedAt: now.Add(4 * time.Second)})
			}
			db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_worker_switch BEFORE INSERT ON adaptive_worker_execution_activations BEGIN SELECT RAISE(ABORT,'injected failure'); END;`); err != nil {
				t.Fatal(err)
			}
			if ok, err := activate(); err == nil || ok {
				t.Fatalf("activation failure accepted: %v %v", ok, err)
			}
			owner, _, err := s.GetSession(ctx, rec.ID)
			if err != nil || owner.Harness != rec.Harness {
				t.Fatalf("ownership escaped rollback: %+v %v", owner, err)
			}
			if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_worker_switch`); err != nil {
				t.Fatal(err)
			}
			if ok, err := activate(); err != nil || !ok {
				t.Fatalf("activate: %v %v", ok, err)
			}
			current, sequence, found, err := s.GetEffectiveWorkerConfiguration(ctx, rec.ID)
			if err != nil || !found || sequence == 0 || current.ContentHash != execution.Configuration.ContentHash || current.Effective.Harness != target {
				t.Fatalf("configuration did not transfer: %+v %v", current, err)
			}
			original, _, err := s.GetWorkerConfiguration(ctx, rec.ID)
			if err != nil || original.ContentHash != snapshot.ContentHash {
				t.Fatal("switch rewrote original launch")
			}
		})
	}
}
