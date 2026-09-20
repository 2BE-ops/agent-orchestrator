package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.WorkerNativeChangeStore = (*Store)(nil)

func pendingWorkerNativeChange(ctx context.Context, q *gen.Queries, id domain.SessionID) (domain.WorkerNativeChange, bool, error) {
	row, err := q.PendingWorkerNativeChange(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkerNativeChange{}, false, nil
	}
	if err != nil {
		return domain.WorkerNativeChange{}, false, err
	}
	change := domain.WorkerNativeChange{ID: row.ID, SessionID: id, ConversationID: row.ConversationID, PreviousActivation: row.PreviousActivation, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Owner), &change.Owner); err != nil {
		return change, false, err
	}
	if err := json.Unmarshal([]byte(row.PreviousOptions), &change.Previous); err != nil {
		return change, false, err
	}
	if err := json.Unmarshal([]byte(row.Requested), &change.Requested); err != nil {
		return change, false, err
	}
	return change, true, change.Validate()
}

// PendingWorkerNativeChange distinguishes unresolved provider state from failure.
func (s *Store) PendingWorkerNativeChange(ctx context.Context, id domain.SessionID) (domain.WorkerNativeChange, bool, error) {
	return pendingWorkerNativeChange(ctx, s.qr, id)
}

// BeginWorkerNativeChange reserves one controller's side effect before native I/O.
func (s *Store) BeginWorkerNativeChange(ctx context.Context, change domain.WorkerNativeChange) error {
	if err := change.Validate(); err != nil {
		return err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "reserve native configuration change", func(q *gen.Queries) error {
		row, err := q.GetSession(ctx, change.SessionID)
		if err != nil {
			return err
		}
		if rowToRecord(row).ControllerOwner() != change.Owner || change.Owner.IsTerminated {
			return ports.ErrRegistryConflict
		}
		conversation, err := q.SelectConversationByID(ctx, change.ConversationID)
		if err != nil {
			return err
		}
		if conversation.CurrentSessionID == nil || *conversation.CurrentSessionID != change.SessionID {
			return ports.ErrRegistryConflict
		}
		if _, err := q.GetActiveAgentSwitch(ctx, change.SessionID); err == nil {
			return ports.ErrRegistryConflict
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := q.GetActiveSessionInterfaceTransition(ctx, change.SessionID); err == nil {
			return ports.ErrRegistryConflict
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, sequence, found, err := effectiveWorkerConfiguration(ctx, q, change.SessionID)
		if err != nil {
			return err
		}
		if !found || sequence != change.PreviousActivation {
			return ports.ErrRegistryConflict
		}
		if _, found, err := pendingWorkerNativeChange(ctx, q, change.SessionID); err != nil {
			return err
		} else if found {
			return ports.ErrRegistryConflict
		}
		owner, err := json.Marshal(change.Owner)
		if err != nil {
			return err
		}
		previous, err := json.Marshal(change.Previous)
		if err != nil {
			return err
		}
		requested, err := json.Marshal(change.Requested)
		if err != nil {
			return err
		}
		return q.InsertWorkerNativeChange(ctx, gen.InsertWorkerNativeChangeParams{ID: change.ID, SessionID: string(change.SessionID), ConversationID: change.ConversationID, Owner: string(owner), PreviousActivation: sequence, PreviousOptions: string(previous), Requested: string(requested), CreatedAt: change.CreatedAt})
	})
}

// RevertWorkerNativeChange records verified compensation without inventing an
// execution activation. Unconfirmed compensation leaves the intent unresolved.
func (s *Store) RevertWorkerNativeChange(ctx context.Context, owner domain.SessionControllerOwner, id, reason string) error {
	if len(reason) > 2000 {
		return fmt.Errorf("native configuration recovery reason is too long")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "resolve reverted native configuration", func(q *gen.Queries) error {
		row, err := q.GetWorkerNativeChange(ctx, id)
		if err != nil {
			return err
		}
		change, found, err := pendingWorkerNativeChange(ctx, q, domain.SessionID(row.SessionID))
		if err != nil {
			return err
		}
		if !found || change.ID != id {
			return ports.ErrRegistryConflict
		}
		record, err := q.GetSession(ctx, change.SessionID)
		if err != nil {
			return err
		}
		_, sequence, _, err := effectiveWorkerConfiguration(ctx, q, change.SessionID)
		if err != nil {
			return err
		}
		if rowToRecord(record).ControllerOwner() != owner || owner.Harness != change.Owner.Harness || owner.Mode != change.Owner.Mode || owner.ProviderConversationID != change.Owner.ProviderConversationID || sequence != change.PreviousActivation {
			return ports.ErrRegistryConflict
		}
		return q.ResolveWorkerNativeChange(ctx, gen.ResolveWorkerNativeChangeParams{ChangeID: id, Outcome: "reverted", Reason: reason, CreatedAt: time.Now().UTC()})
	})
}
