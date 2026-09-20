// Package taskmessage delivers persisted coordination through AO's native
// session boundary. It never executes worker content or changes task planning.
package taskmessage

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Store is the message journal plus the target's exclusive worker association.
type Store interface {
	ports.TaskMessageStore
	GetActiveTaskLease(context.Context, string) (domain.TaskLease, bool, error)
	GetTaskWorkerDispatch(context.Context, string) (domain.TaskWorkerDispatch, bool, error)
}

// Dispatcher is one daemon-owned, bounded, fair outbox consumer.
type Dispatcher struct {
	store     Store
	transport ports.TaskMessageTransport
	log       *slog.Logger
}

// New binds the durable journal to the existing mode-aware native transport.
func New(store Store, transport ports.TaskMessageTransport, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{store: store, transport: transport, log: logger}
}

// Run first closes pre-existing unknown claims, then scans at most 16 candidates
// per cycle. Durable cursor checkpoints keep a blocked branch from starving
// unrelated work. Each native attempt has its own finite deadline.
func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	recovered := false
	for {
		if ctx.Err() != nil {
			return
		}
		if !recovered {
			if err := d.reconcile(ctx); err != nil {
				d.log.Warn("task message recovery deferred", "error", err)
			} else {
				recovered = true
			}
		}
		if recovered {
			if err := d.dispatch(ctx); err != nil && ctx.Err() == nil {
				d.log.Warn("task message dispatch deferred", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) reconcile(ctx context.Context) error {
	after := ""
	for {
		items, err := d.store.ListUnresolvedTaskMessageDeliveries(ctx, after, 100)
		if err != nil {
			return err
		}
		for _, item := range items {
			// There is no TUI recipient acknowledgement. Even Chat must retain
			// ambiguity until its durable turn can be positively reconciled.
			if err := d.store.ResolveTaskMessageDelivery(ctx, domain.TaskMessageDeliveryResolution{ID: item.ID, State: "uncertain", Reason: "Daemon restarted before the native delivery outcome was recorded"}); err != nil {
				return err
			}
			after = item.ID
		}
		if len(items) < 100 {
			return nil
		}
	}
}

func (d *Dispatcher) dispatch(ctx context.Context) error {
	after, err := d.store.TaskMessageDispatchCursor(ctx)
	if err != nil {
		return err
	}
	items, err := d.store.ListPendingTaskMessages(ctx, after, 16)
	if err != nil {
		return err
	}
	for _, item := range items {
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := d.deliver(attemptCtx, item)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			d.log.Warn("task message delivery deferred", "messageID", item.ID, "error", err)
		}
		if err := d.store.SetTaskMessageDispatchCursor(ctx, item.Sequence); err != nil {
			return err
		}
	}
	if len(items) < 16 {
		return d.store.SetTaskMessageDispatchCursor(ctx, 0)
	}
	return nil
}

func (d *Dispatcher) deliver(ctx context.Context, message domain.TaskMessage) error {
	lease, found, err := d.store.GetActiveTaskLease(ctx, message.Definition.TargetTaskID)
	if err != nil {
		return err
	}
	if !found || !time.Now().Before(lease.ExpiresAt) {
		return nil
	}
	target, found, err := d.store.GetTaskWorkerDispatch(ctx, lease.AttemptID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	ready, err := d.transport.TaskMessageTargetReady(ctx, target.SessionID)
	if err != nil {
		// Readiness probes do not prove death or consume a native send attempt.
		d.log.Debug("task message target readiness unknown", "sessionID", target.SessionID, "error", err)
		return nil
	}
	if !ready {
		return nil
	}
	delivery, created, err := d.store.BeginTaskMessageDelivery(ctx, message.ID, uuid.NewString())
	if errors.Is(err, ports.ErrTaskConflict) || errors.Is(err, ports.ErrTaskLeaseFenced) || errors.Is(err, ports.ErrTaskNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	observed := d.transport.DeliverTaskMessage(ctx, delivery, message)
	resolution := domain.TaskMessageDeliveryResolution{ID: delivery.ID, State: observed.State, Reason: observed.Reason}
	if err := resolution.Validate(); err != nil {
		resolution.State, resolution.Reason = "uncertain", "Native transport returned an invalid delivery observation"
	}
	// Persist even if the native call exhausted its context. A failed commit
	// leaves dispatching, which startup reconciliation never blindly resends.
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return d.store.ResolveTaskMessageDelivery(commitCtx, resolution)
}
