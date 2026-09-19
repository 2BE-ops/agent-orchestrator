package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func messageDeliveryFromRow(row gen.AdaptiveTaskMessageDelivery) (domain.TaskMessageDelivery, error) {
	d := domain.TaskMessageDelivery{ID: row.ID, MessageID: row.MessageID, Number: row.Number, TargetAttemptID: row.TargetAttemptID, SessionID: domain.SessionID(row.SessionID), DeliveryKey: row.DeliveryKey, State: row.State, Reason: row.Reason, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	err := json.Unmarshal([]byte(row.Owner), &d.Owner)
	d.NativeGeneration = resultGeneration(d.Owner)
	return d, err
}

// BeginTaskMessageDelivery reserves one native attempt before I/O. Callers may
// send only when created is true. A replay does not authorize a second send.
func (s *Store) BeginTaskMessageDelivery(ctx context.Context, messageID, deliveryID string) (domain.TaskMessageDelivery, bool, error) {
	var result domain.TaskMessageDelivery
	for _, id := range []string{messageID, deliveryID} {
		if strings.TrimSpace(id) == "" || len(id) > 200 || strings.IndexFunc(id, unicode.IsControl) >= 0 {
			return result, false, ports.ErrTaskInvalid
		}
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return result, false, err
	}
	defer s.writeMu.Unlock()
	created := false
	err := s.inTx(ctx, "reserve typed message delivery", func(q *gen.Queries) error {
		previous, err := q.GetTaskMessageDelivery(ctx, deliveryID)
		if err == nil {
			if previous.MessageID != messageID {
				return ports.ErrTaskConflict
			}
			result, err = messageDeliveryFromRow(previous)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		row, err := q.GetTaskMessage(ctx, messageID)
		if err != nil {
			return taskReadError(err)
		}
		message, err := taskMessageFromRow(row)
		if err != nil {
			return err
		}
		deliveries, err := q.ListTaskMessageDeliveries(ctx, messageID)
		if err != nil {
			return err
		}
		if len(deliveries) >= 4 {
			return fmt.Errorf("%w: delivery retry bound reached", ports.ErrTaskConflict)
		}
		for _, delivery := range deliveries {
			if delivery.State != "not_sent" {
				return ports.ErrTaskConflict
			}
		}
		if err := requireTaskRunIntent(ctx, q, message.Definition.TargetTaskID); err != nil {
			return err
		}
		lease, err := q.GetActiveTaskLease(ctx, message.Definition.TargetTaskID)
		if err != nil {
			return taskReadError(err)
		}
		now := time.Now().UTC()
		if !now.Before(lease.ExpiresAt) {
			return ports.ErrTaskLeaseFenced
		}
		dispatch, err := q.GetTaskWorkerDispatch(ctx, lease.AttemptID)
		if err != nil {
			return taskReadError(err)
		}
		session, err := q.GetSession(ctx, domain.SessionID(dispatch.SessionID))
		if err != nil {
			return taskReadError(err)
		}
		rec := rowToRecord(session)
		unsettled, err := q.CountUnsettledTaskMessageDeliveries(ctx, string(rec.ID))
		if err != nil {
			return err
		}
		// A previous partial TUI paste may still occupy the composer. Do not
		// append another automated message until that recipient is reconciled.
		if unsettled != 0 {
			return ports.ErrTaskLeaseFenced
		}
		_, activation, _, err := effectiveWorkerConfiguration(ctx, q, rec.ID)
		if err != nil {
			return err
		}
		facts, err := readTaskWorkerFacts(ctx, q, lease.AttemptID, rec.ID, rec.ControllerOwner(), activation)
		if err != nil {
			return err
		}
		if facts.task.ProjectID != string(message.ProjectID) {
			return ports.ErrTaskLeaseFenced
		}
		owner, err := json.Marshal(rec.ControllerOwner())
		if err != nil {
			return err
		}
		result = domain.TaskMessageDelivery{ID: deliveryID, MessageID: messageID, Number: int64(len(deliveries)) + 1, TargetAttemptID: lease.AttemptID, SessionID: rec.ID, Owner: rec.ControllerOwner(), DeliveryKey: "adaptive-message:" + messageID, State: "dispatching", Reason: "Reserved before native delivery", CreatedAt: now, UpdatedAt: now}
		result.NativeGeneration = resultGeneration(result.Owner)
		if err := q.InsertTaskMessageDelivery(ctx, gen.InsertTaskMessageDeliveryParams{ID: result.ID, MessageID: messageID, Number: result.Number, TargetAttemptID: result.TargetAttemptID, SessionID: string(rec.ID), Owner: string(owner), DeliveryKey: result.DeliveryKey, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		mutation := domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "task-message-delivery"}, Reason: "Reserved message " + messageID + " delivery " + deliveryID}
		if err := insertTaskAudit(ctx, q, facts.task.ID, facts.attempt.TaskRevision, "message_delivery_reserved", mutation, now); err != nil {
			return err
		}
		created = true
		return nil
	})
	return result, created && err == nil, err
}

// ResolveTaskMessageDelivery only records observation. It cannot authorize a
// retry of handed-off or uncertain work, rewrite its target or release a lease.
func (s *Store) ResolveTaskMessageDelivery(ctx context.Context, resolution domain.TaskMessageDeliveryResolution) error {
	if err := resolution.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "resolve typed message delivery", func(q *gen.Queries) error {
		previous, err := q.GetTaskMessageDelivery(ctx, resolution.ID)
		if err != nil {
			return taskReadError(err)
		}
		if previous.State != "dispatching" {
			if previous.State == resolution.State && previous.Reason == resolution.Reason {
				return nil
			}
			return ports.ErrTaskConflict
		}
		now := time.Now().UTC()
		changed, err := q.ResolveTaskMessageDelivery(ctx, gen.ResolveTaskMessageDeliveryParams{ID: resolution.ID, State: resolution.State, Reason: resolution.Reason, UpdatedAt: now})
		if err != nil {
			return err
		}
		if changed != 1 {
			return ports.ErrTaskConflict
		}
		attempt, err := q.GetTaskAttempt(ctx, previous.TargetAttemptID)
		if err != nil {
			return err
		}
		mutation := domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "task-message-delivery"}, Reason: "Message " + previous.MessageID + " delivery " + resolution.ID + ": " + resolution.Reason}
		return insertTaskAudit(ctx, q, attempt.TaskID, attempt.TaskRevision, "message_delivery_"+resolution.State, mutation, now)
	})
}

func messageDeliveriesFromRows(rows []gen.AdaptiveTaskMessageDelivery) ([]domain.TaskMessageDelivery, error) {
	items := make([]domain.TaskMessageDelivery, 0, len(rows))
	for _, row := range rows {
		item, err := messageDeliveryFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// ListTaskMessageDeliveries returns the bounded native attempt history.
func (s *Store) ListTaskMessageDeliveries(ctx context.Context, messageID string) ([]domain.TaskMessageDelivery, error) {
	rows, err := s.qr.ListTaskMessageDeliveries(ctx, messageID)
	if err != nil {
		return nil, err
	}
	return messageDeliveriesFromRows(rows)
}

// ListUnresolvedTaskMessageDeliveries pages claims requiring restart reconciliation.
func (s *Store) ListUnresolvedTaskMessageDeliveries(ctx context.Context, after string, limit int) ([]domain.TaskMessageDelivery, error) {
	if limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListUnresolvedTaskMessageDeliveries(ctx, gen.ListUnresolvedTaskMessageDeliveriesParams{ID: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return messageDeliveriesFromRows(rows)
}

// TaskMessageDispatchCursor restores fair scanning past temporarily blocked work.
func (s *Store) TaskMessageDispatchCursor(ctx context.Context) (int64, error) {
	return s.qr.TaskMessageDispatchCursor(ctx)
}

// SetTaskMessageDispatchCursor checkpoints scheduling, never delivery success.
func (s *Store) SetTaskMessageDispatchCursor(ctx context.Context, after int64) error {
	if after < 0 {
		return ports.ErrTaskInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	changed, err := s.qw.SetTaskMessageDispatchCursor(ctx, after)
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("task message dispatch checkpoint is missing")
	}
	return nil
}
