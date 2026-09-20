package agentmanager

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// InboxDispatchStore combines durable routing intent, sealed input and native
// ownership reads. The service never opens storage or starts processes itself.
type InboxDispatchStore interface {
	ports.AgentManagerStore
	ports.AgentManagerInboxStore
	ports.AgentManagerContextStore
	ports.AgentManagerDeliveryStore
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	GetAdaptiveTask(context.Context, string) (domain.AdaptiveTask, error)
}

// InboxDispatcher is one fair, bounded consumer run after native reconciliation.
type InboxDispatcher struct {
	store       InboxDispatchStore
	controllers *Manager
	transport   ports.AgentManagerTransport
	tools       domain.AgentManagerToolPaths
	log         *slog.Logger
}

// NewInboxDispatcher validates native tool routing before any admission effects.
func NewInboxDispatcher(store InboxDispatchStore, controllers *Manager, transport ports.AgentManagerTransport, paths domain.AgentManagerToolPaths, logger *slog.Logger) (*InboxDispatcher, error) {
	if err := paths.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &InboxDispatcher{store: store, controllers: controllers, transport: transport, tools: paths, log: logger}, nil
}

// Run recovers unknown send claims once, then scans at most sixteen requests per
// cycle. A durable cursor lets unrelated projects proceed past blocked owners.
func (d *InboxDispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	recovered := false
	for {
		if ctx.Err() != nil {
			return
		}
		if !recovered {
			if err := d.reconcile(ctx); err != nil {
				d.log.Warn("Manager inbox recovery deferred", "error", err)
			} else {
				recovered = true
			}
		}
		if recovered {
			if err := d.dispatch(ctx); err != nil && ctx.Err() == nil {
				d.log.Warn("Manager inbox dispatch deferred", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *InboxDispatcher) reconcile(ctx context.Context) error {
	after := ""
	for {
		items, err := d.store.ListUnresolvedAgentManagerDeliveries(ctx, after, 100)
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := d.store.ResolveAgentManagerDelivery(ctx, domain.AgentManagerDeliveryResolution{ID: item.ID, State: "uncertain", Reason: "Daemon restarted before the native Manager send outcome was recorded"}); err != nil {
				return err
			}
			after = item.ID
		}
		if len(items) < 100 {
			return nil
		}
	}
}

func (d *InboxDispatcher) dispatch(ctx context.Context) error {
	after, err := d.store.AgentManagerDispatchCursor(ctx)
	if err != nil {
		return err
	}
	items, err := d.store.ListDispatchableAgentManagerRequests(ctx, after, 16)
	if err != nil {
		return err
	}
	for _, item := range items {
		attemptCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := d.deliver(attemptCtx, item)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			d.log.Warn("Manager inbox request deferred", "requestID", item.ID, "error", err)
		}
		if err := d.store.SetAgentManagerDispatchCursor(ctx, item.Sequence); err != nil {
			return err
		}
	}
	if len(items) < 16 {
		return d.store.SetAgentManagerDispatchCursor(ctx, 0)
	}
	return nil
}

func (d *InboxDispatcher) closeRequest(ctx context.Context, request domain.AgentManagerRequest, outcome, reason string) error {
	err := d.store.ResolveAgentManagerRequest(ctx, request.ProjectID, domain.AgentManagerRequestResolution{RequestID: request.ID, Outcome: outcome, Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: "manager-inbox"}, Reason: reason, CreatedAt: time.Now().UTC()})
	if errors.Is(err, ports.ErrAgentManagerConflict) {
		return nil
	}
	return err
}

func (d *InboxDispatcher) deliver(ctx context.Context, request domain.AgentManagerRequest) error {
	configuration, err := d.store.GetAgentManager(ctx, request.ProjectID)
	if err != nil {
		return err
	}
	task, err := d.store.GetAdaptiveTask(ctx, request.TaskID)
	if err != nil {
		return err
	}
	if configuration.Number != request.ConfigurationVersion || configuration.ContentHash != request.ConfigurationHash || !configuration.Definition.Enabled || task.Revision != request.TaskRevision {
		return d.closeRequest(ctx, request, "superseded", "Pinned routing intent no longer matches current task or Manager governance")
	}
	controller, err := d.controllers.CurrentController(ctx, request.ProjectID)
	if err != nil {
		return err
	}
	if controller == nil {
		receipt, err := d.controllers.StartController(ctx, domain.AdaptiveActor{Kind: "SYSTEM", ID: "manager-inbox"}, request.ProjectID, ControllerStartInput{ID: uuid.NewString(), ConfigurationVersion: request.ConfigurationVersion, Reason: "Start configured Manager for durable inbox work"})
		if err != nil {
			return err
		}
		controller = &receipt.State
	}
	if controller.Controller.ConfigurationVersion != request.ConfigurationVersion {
		return d.closeRequest(ctx, request, "needs_human", "Current Manager retains an earlier governing configuration; reconcile its native ownership before replacement")
	}
	if controller.Dispatch == nil || controller.PendingOperation != nil {
		return nil
	}
	rec, found, err := d.store.GetSession(ctx, controller.Dispatch.SessionID)
	if err != nil {
		return err
	}
	if !found || rec.IsTerminated || rec.Kind != domain.KindAgentManager {
		return nil
	}
	ready, err := d.transport.AgentManagerTargetReady(ctx, rec.ID)
	if err != nil {
		d.log.Debug("Manager readiness unknown", "sessionID", rec.ID, "error", err)
		return nil
	}
	if !ready {
		return nil
	}
	input := domain.AgentManagerContextSeal{ID: uuid.NewString(), ProjectID: request.ProjectID, RequestID: request.ID, SessionID: rec.ID, SourceOwner: rec.ControllerOwner(), Now: time.Now().UTC(), Tools: &d.tools}
	delivery, created, err := d.store.BeginAgentManagerDelivery(ctx, input, uuid.NewString())
	if errors.Is(err, ports.ErrAgentManagerForbidden) {
		return d.closeRequest(ctx, request, "needs_human", "Pinned Manager clearance or existing conversation engagement cannot receive this routing input")
	}
	if errors.Is(err, ports.ErrTaskLeaseFenced) {
		return d.closeRequest(ctx, request, "cancelled", "Task or ancestor was cancelled before Manager input delivery")
	}
	if errors.Is(err, ports.ErrAgentManagerFenced) || errors.Is(err, ports.ErrAgentManagerConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	sealed, err := d.store.GetAgentManagerContext(ctx, request.ProjectID, request.ID, delivery.ContextID)
	observed := ports.AgentManagerTransportResult{State: "not_sent", Reason: "Sealed input could not be read before any native transport call"}
	if err == nil {
		observed = d.transport.DeliverAgentManagerContext(ctx, delivery, sealed)
	}
	resolution := domain.AgentManagerDeliveryResolution{ID: delivery.ID, State: observed.State, Reason: observed.Reason}
	if err := resolution.Validate(); err != nil {
		resolution.State, resolution.Reason = "uncertain", "Native Manager transport returned an invalid observation"
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return d.store.ResolveAgentManagerDelivery(commitCtx, resolution)
}
