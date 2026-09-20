package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.WorkerConversationSettingsStore = (*Store)(nil)

// CommitWorkerConversationSettings publishes one validated turn configuration
// and its execution record atomically. It never changes the original launch.
func (s *Store) CommitWorkerConversationSettings(ctx context.Context, owner domain.SessionControllerOwner, conversationID string, settings domain.ConversationSettings, e domain.WorkerExecution) error {
	if err := e.Validate(); err != nil {
		return err
	}
	permissions := settings.ApprovalMode
	if permissions == "" {
		permissions = domain.PermissionModeAuto
	}
	if e.SourceKind != "conversation_settings" || e.Configuration.NativeSettings == nil || *e.Configuration.NativeSettings != settings || e.Configuration.Effective.Config.Model != settings.Model || e.Configuration.Effective.Config.Effort != settings.ReasoningEffort || e.Configuration.Effective.Config.Permissions != permissions {
		return fmt.Errorf("worker configuration does not match native settings")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "commit worker conversation settings", func(q *gen.Queries) error {
		row, err := q.GetSession(ctx, e.SessionID)
		if err != nil {
			return err
		}
		if rowToRecord(row).ControllerOwner() != owner || domain.NormalizeSessionMode(row.SessionMode) != domain.SessionModeChat {
			return ports.ErrRegistryConflict
		}
		conversation, err := q.SelectConversationByID(ctx, conversationID)
		if err != nil {
			return err
		}
		if conversation.CurrentSessionID == nil || *conversation.CurrentSessionID != e.SessionID {
			return ports.ErrRegistryConflict
		}
		current, sequence, found, err := effectiveWorkerConfiguration(ctx, q, e.SessionID)
		if err != nil {
			return err
		}
		if !found || sequence != e.PreviousActivation || current.AgentType != e.Configuration.AgentType {
			return ports.ErrRegistryConflict
		}
		pending, hasPending, err := pendingWorkerNativeChange(ctx, q, e.SessionID)
		if err != nil {
			return err
		}
		if hasPending && (pending.ID != e.SourceID || pending.Owner != owner || pending.ConversationID != conversationID) {
			return ports.ErrRegistryConflict
		}
		if hasPending && !slices.ContainsFunc(e.Configuration.NativeOptions, pending.Requested.Equal) {
			return ports.ErrRegistryConflict
		}
		if err := insertWorkerExecution(ctx, q, e); err != nil {
			return err
		}
		if err := updateConversationSettings(ctx, q, conversationID, settings, e.CreatedAt); err != nil {
			return err
		}
		if err := activateWorkerExecution(ctx, q, e, false, e.CreatedAt); err != nil {
			return err
		}
		if hasPending {
			return q.ResolveWorkerNativeChange(ctx, gen.ResolveWorkerNativeChangeParams{ChangeID: pending.ID, Outcome: "applied", ExecutionID: sql.NullString{String: e.ID, Valid: true}, Reason: "Provider confirmed native configuration", CreatedAt: e.CreatedAt})
		}
		return nil
	})
}
