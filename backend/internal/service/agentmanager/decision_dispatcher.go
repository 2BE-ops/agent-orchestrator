package agentmanager

import (
	"context"
	"log/slog"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// DecisionDispatcher recovers a proposal committed before its synchronous
// assessment/response. It shares the exact service path used by native tools.
type DecisionDispatcher struct {
	store   ports.AgentManagerDecisionRecoveryStore
	manager *Manager
	log     *slog.Logger
}

// NewDecisionDispatcher connects an independent fair scan, without native sends.
func NewDecisionDispatcher(store ports.AgentManagerDecisionRecoveryStore, manager *Manager, logger *slog.Logger) *DecisionDispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &DecisionDispatcher{store: store, manager: manager, log: logger}
}

// Run reconciles eight retained proposals per cycle after daemon startup recovery.
func (d *DecisionDispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := d.dispatch(ctx); err != nil && ctx.Err() == nil {
			d.log.Warn("Manager decision recovery deferred", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *DecisionDispatcher) dispatch(ctx context.Context) error {
	after, err := d.store.AgentManagerDecisionCursor(ctx)
	if err != nil {
		return err
	}
	items, err := d.store.ListUnassessedAgentManagerProposals(ctx, after, 8)
	if err != nil {
		return err
	}
	for _, item := range items {
		_, err := d.manager.AssessProposal(ctx, item.ProjectID, item.RequestID, item.ProposalID)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			d.log.Warn("Manager proposal assessment deferred", "proposalID", item.ProposalID, "error", err)
		}
		if err := d.store.SetAgentManagerDecisionCursor(ctx, item.ProposalID); err != nil {
			return err
		}
	}
	if len(items) < 8 {
		return d.store.SetAgentManagerDecisionCursor(ctx, "")
	}
	return nil
}
