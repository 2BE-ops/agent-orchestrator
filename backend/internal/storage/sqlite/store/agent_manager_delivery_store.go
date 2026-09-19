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

var _ ports.AgentManagerDeliveryStore = (*Store)(nil)

// ValidateAgentManagerDelivery is the last-moment native pre-write check. Task
// cancellation, changed governance and replaced owners fence a previously valid
// reservation. It never changes the claim or treats a read as a send receipt.
func (s *Store) ValidateAgentManagerDelivery(ctx context.Context, id string) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "validate Manager delivery authority", func(q *gen.Queries) error {
		row, err := q.GetAgentManagerDelivery(ctx, id)
		if err != nil {
			return agentManagerReadError(err)
		}
		delivery, err := managerDeliveryFromRow(row)
		if err != nil {
			return err
		}
		if delivery.State != "dispatching" {
			return ports.ErrAgentManagerFenced
		}
		requestRow, err := q.GetAgentManagerRequest(ctx, delivery.RequestID)
		if err != nil {
			return err
		}
		request, err := managerRequestFromRow(requestRow)
		if err != nil {
			return err
		}
		_, controller, _, err := managerRequestNativeOwner(ctx, q, request, delivery.SessionID, delivery.Owner, time.Now().UTC())
		if err != nil {
			return err
		}
		if controller.ID != delivery.ControllerID {
			return ports.ErrAgentManagerFenced
		}
		return nil
	})
}

func managerDeliveryFromRow(row gen.AdaptiveAgentManagerDelivery) (domain.AgentManagerDelivery, error) {
	d := domain.AgentManagerDelivery{ID: row.ID, RequestID: row.RequestID, ContextID: row.ContextID, ControllerID: row.ControllerID, Number: row.Number, SessionID: domain.SessionID(row.SessionID), DeliveryKey: row.DeliveryKey, State: row.State, Reason: row.Reason, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	err := json.Unmarshal([]byte(row.Owner), &d.Owner)
	d.NativeGeneration = resultGeneration(d.Owner)
	return d, err
}

// BeginAgentManagerDelivery seals the input and reserves a send in one transaction.
// Only created=true permits native I/O. Unknown writes hold the whole conversation.
func (s *Store) BeginAgentManagerDelivery(ctx context.Context, input domain.AgentManagerContextSeal, deliveryID string) (domain.AgentManagerDelivery, bool, error) {
	if err := input.Validate(); err != nil {
		return domain.AgentManagerDelivery{}, false, ports.ErrAgentManagerInvalid
	}
	if err := (domain.AgentManagerDeliveryResolution{ID: deliveryID, State: "not_sent", Reason: "Validate identity"}).Validate(); err != nil {
		return domain.AgentManagerDelivery{}, false, ports.ErrAgentManagerInvalid
	}
	if resultGeneration(input.SourceOwner) == "" || input.SourceOwner.IsTerminated {
		return domain.AgentManagerDelivery{}, false, ports.ErrAgentManagerFenced
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.AgentManagerDelivery{}, false, err
	}
	defer s.writeMu.Unlock()
	var result domain.AgentManagerDelivery
	created := false
	err := s.inTx(ctx, "reserve Manager delivery", func(q *gen.Queries) error {
		requestRow, err := q.GetAgentManagerRequest(ctx, input.RequestID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if requestRow.ProjectID != string(input.ProjectID) {
			return ports.ErrAgentManagerNotFound
		}
		request, err := managerRequestFromRow(requestRow)
		if err != nil {
			return err
		}
		previous, err := q.GetAgentManagerDelivery(ctx, deliveryID)
		if err == nil {
			result, err = managerDeliveryFromRow(previous)
			if err != nil {
				return err
			}
			if result.RequestID != input.RequestID || result.ContextID != input.ID || result.SessionID != input.SessionID || result.Owner != input.SourceOwner {
				return ports.ErrAgentManagerConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, controller, _, err := managerRequestNativeOwner(ctx, q, request, input.SessionID, input.SourceOwner, input.Now)
		if err != nil {
			return err
		}
		busy, err := q.AgentManagerConversationBusy(ctx, controller.ID)
		if err != nil {
			return err
		}
		if busy {
			return ports.ErrAgentManagerFenced
		}
		history, err := q.ListAgentManagerDeliveries(ctx, request.ID)
		if err != nil {
			return err
		}
		if len(history) >= 4 {
			return fmt.Errorf("%w: Manager delivery retry bound reached", ports.ErrAgentManagerConflict)
		}
		for _, prior := range history {
			if prior.State != "not_sent" {
				return ports.ErrAgentManagerConflict
			}
		}
		sealed, _, err := sealManagerContext(ctx, q, input)
		if err != nil {
			return err
		}
		owner, err := json.Marshal(input.SourceOwner)
		if err != nil {
			return err
		}
		result = domain.AgentManagerDelivery{ID: deliveryID, RequestID: request.ID, ContextID: sealed.ID, ControllerID: controller.ID, Number: int64(len(history) + 1), SessionID: input.SessionID, Owner: input.SourceOwner, NativeGeneration: sealed.NativeGeneration, DeliveryKey: "adaptive-manager:" + request.ID, State: "dispatching", Reason: "Reserved before native delivery", CreatedAt: input.Now, UpdatedAt: input.Now}
		if err := q.InsertAgentManagerDelivery(ctx, gen.InsertAgentManagerDeliveryParams{ID: result.ID, RequestID: result.RequestID, ContextID: result.ContextID, ControllerID: result.ControllerID, Number: result.Number, SessionID: string(result.SessionID), Owner: string(owner), DeliveryKey: result.DeliveryKey, CreatedAt: input.Now, UpdatedAt: input.Now}); err != nil {
			return err
		}
		if err := insertManagerInboxAudit(ctx, q, request, "delivery_reserved", domain.AdaptiveActor{Kind: "SYSTEM", ID: controller.ID}, "Reserved Manager delivery "+deliveryID, input.Now); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return domain.AgentManagerDelivery{}, false, err
	}
	return result, created, nil
}

// ResolveAgentManagerDelivery retains one terminal transport observation. It
// cannot resolve routing intent or authorize uncertain work to be repeated.
func (s *Store) ResolveAgentManagerDelivery(ctx context.Context, resolution domain.AgentManagerDeliveryResolution) error {
	if err := resolution.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "resolve Manager delivery", func(q *gen.Queries) error {
		previous, err := q.GetAgentManagerDelivery(ctx, resolution.ID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if previous.State != "dispatching" {
			if previous.State == resolution.State && previous.Reason == resolution.Reason {
				return nil
			}
			return ports.ErrAgentManagerConflict
		}
		now := time.Now().UTC()
		changed, err := q.ResolveAgentManagerDelivery(ctx, gen.ResolveAgentManagerDeliveryParams{ID: resolution.ID, State: resolution.State, Reason: resolution.Reason, UpdatedAt: now})
		if err != nil {
			return err
		}
		if changed != 1 {
			return ports.ErrAgentManagerConflict
		}
		row, err := q.GetAgentManagerRequest(ctx, previous.RequestID)
		if err != nil {
			return err
		}
		request, err := managerRequestFromRow(row)
		if err != nil {
			return err
		}
		actor := domain.AdaptiveActor{Kind: "SYSTEM", ID: previous.ControllerID}
		if err := insertManagerInboxAudit(ctx, q, request, "delivery_"+resolution.State, actor, "Manager delivery "+resolution.ID+": "+resolution.Reason, now); err != nil {
			return err
		}
		if previous.Number == 4 && resolution.State == "not_sent" {
			if _, err := q.GetAgentManagerRequestResolution(ctx, request.ID); err == nil {
				return nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			encoded, err := json.Marshal(actor)
			if err != nil {
				return err
			}
			reason := "Manager native delivery retry limit exhausted; inspect retained transport history"
			if err := q.InsertAgentManagerRequestResolution(ctx, gen.InsertAgentManagerRequestResolutionParams{RequestID: request.ID, Outcome: "needs_human", Actor: string(encoded), Reason: reason, CreatedAt: now}); err != nil {
				return err
			}
			return insertManagerInboxAudit(ctx, q, request, "request_needs_human", actor, reason, now)
		}
		return nil
	})
}

func managerDeliveriesFromRows(rows []gen.AdaptiveAgentManagerDelivery) ([]domain.AgentManagerDelivery, error) {
	items := make([]domain.AgentManagerDelivery, 0, len(rows))
	for _, row := range rows {
		item, err := managerDeliveryFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// ListAgentManagerDeliveries reads at most four transport attempts per request.
func (s *Store) ListAgentManagerDeliveries(ctx context.Context, project domain.ProjectID, requestID string) ([]domain.AgentManagerDelivery, error) {
	if _, err := s.GetAgentManagerRequest(ctx, project, requestID); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListAgentManagerDeliveries(ctx, requestID)
	if err != nil {
		return nil, err
	}
	return managerDeliveriesFromRows(rows)
}

// ListUnresolvedAgentManagerDeliveries supplies restart reconciliation claims.
func (s *Store) ListUnresolvedAgentManagerDeliveries(ctx context.Context, after string, limit int) ([]domain.AgentManagerDelivery, error) {
	if limit < 1 || limit > 100 {
		return nil, ports.ErrAgentManagerInvalid
	}
	rows, err := s.qr.ListUnresolvedAgentManagerDeliveries(ctx, gen.ListUnresolvedAgentManagerDeliveriesParams{ID: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return managerDeliveriesFromRows(rows)
}

// ListDispatchableAgentManagerRequests scans fairly without inferring readiness.
func (s *Store) ListDispatchableAgentManagerRequests(ctx context.Context, after int64, limit int) ([]domain.AgentManagerRequest, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrAgentManagerInvalid
	}
	rows, err := s.qr.ListDispatchableAgentManagerRequests(ctx, gen.ListDispatchableAgentManagerRequestsParams{Sequence: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]domain.AgentManagerRequest, 0, len(rows))
	for _, row := range rows {
		item, err := managerRequestFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// AgentManagerDispatchCursor is a durable scan checkpoint, never a success flag.
func (s *Store) AgentManagerDispatchCursor(ctx context.Context) (int64, error) {
	return s.qr.AgentManagerDispatchCursor(ctx)
}

// SetAgentManagerDispatchCursor prevents blocked requests from starving others.
func (s *Store) SetAgentManagerDispatchCursor(ctx context.Context, after int64) error {
	if after < 0 {
		return ports.ErrAgentManagerInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	changed, err := s.qw.SetAgentManagerDispatchCursor(ctx, after)
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("manager dispatch checkpoint is missing")
	}
	return nil
}
