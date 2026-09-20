package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestWorkerConversationSettingsCommitIsAtomicAndFenced(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	rec, original := workerSnapshot(t, s)
	rec.Mode = domain.SessionModeChat
	original.Effective.SessionMode = rec.Mode
	original.ContentHash = original.Hash()
	rec, err := s.CreateConfiguredSession(ctx, rec, original)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := s.CreateConversation(ctx, "worker-settings", domain.ConversationScopeSession, rec.ProjectID, rec.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	settings := domain.ConversationSettings{Model: "selected-model", ReasoningEffort: "high", ApprovalMode: domain.PermissionModeDefault}
	changed := original
	changed.Effective.Config.Model = settings.Model
	changed.Effective.Config.Effort = settings.ReasoningEffort
	changed.Effective.Config.Permissions = settings.ApprovalMode
	changed.NativeSettings = &settings
	changed.ContentHash = changed.Hash()
	execution := domain.WorkerExecution{ID: "settings-change", SessionID: rec.ID, SourceKind: "conversation_settings", SourceID: "settings-change", Configuration: changed, Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Change next turn settings", CreatedAt: time.Now().UTC()}
	owner := rec.ControllerOwner()
	wrong := owner
	wrong.RuntimeLaunchID = "another-controller"
	if err := s.CommitWorkerConversationSettings(ctx, wrong, conversation.ID, settings, execution); !errors.Is(err, ports.ErrRegistryConflict) {
		t.Fatalf("stale owner: %v", err)
	}
	inconsistent := settings
	inconsistent.Model = "unattributed-model"
	if err := s.CommitWorkerConversationSettings(ctx, owner, conversation.ID, inconsistent, execution); err == nil {
		t.Fatal("inconsistent settings accepted")
	}
	other := rec
	other.ID = ""
	other, err = s.CreateSession(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := s.CreateConversation(ctx, "foreign-settings", domain.ConversationScopeSession, other.ProjectID, other.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CommitWorkerConversationSettings(ctx, owner, foreign.ID, settings, execution); !errors.Is(err, ports.ErrRegistryConflict) {
		t.Fatalf("foreign conversation: %v", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_settings_activation BEFORE INSERT ON adaptive_worker_execution_activations BEGIN SELECT RAISE(ABORT,'injected settings failure'); END;`); err != nil {
		t.Fatal(err)
	}
	before, err := s.EventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CommitWorkerConversationSettings(ctx, owner, conversation.ID, settings, execution); err == nil {
		t.Fatal("activation failure accepted")
	}
	stored, err := s.ConversationForSession(ctx, rec.ID)
	if err != nil || stored.Settings != conversation.Settings {
		t.Fatalf("settings escaped rollback: %+v %v", stored.Settings, err)
	}
	if _, err := s.GetWorkerExecution(ctx, rec.ID, execution.ID); err == nil {
		t.Fatal("failed execution escaped rollback")
	}
	after, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(before) != len(after) {
		t.Fatalf("failed settings emitted CDC: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_settings_activation`); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitWorkerConversationSettings(ctx, owner, conversation.ID, settings, execution); err != nil {
		t.Fatal(err)
	}
	stale := execution
	stale.ID, stale.SourceID = "stale-settings", "stale-settings"
	if err := s.CommitWorkerConversationSettings(ctx, owner, conversation.ID, settings, stale); !errors.Is(err, ports.ErrRegistryConflict) {
		t.Fatalf("stale configuration accepted: %v", err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	current, sequence, found, err := reopened.GetEffectiveWorkerConfiguration(ctx, rec.ID)
	if err != nil || !found || sequence == 0 || current.ContentHash != changed.ContentHash || *current.NativeSettings != settings {
		t.Fatalf("settings history lost: %+v %v", current, err)
	}
	stored, err = reopened.ConversationForSession(ctx, rec.ID)
	if err != nil || stored.Settings != settings {
		t.Fatalf("native settings lost: %+v %v", stored.Settings, err)
	}
	launch, found, err := reopened.GetWorkerConfiguration(ctx, rec.ID)
	if err != nil || !found || launch.ContentHash != original.ContentHash {
		t.Fatalf("original launch rewritten: %v", err)
	}
}
