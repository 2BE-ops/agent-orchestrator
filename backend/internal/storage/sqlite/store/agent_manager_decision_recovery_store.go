package store

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.AgentManagerDecisionRecoveryStore = (*Store)(nil)

// ListUnassessedAgentManagerProposals supplies references across projects, without
// assuming any native owner is still alive or treating parser output as applied.
func (s *Store) ListUnassessedAgentManagerProposals(ctx context.Context, after string, limit int) ([]ports.AgentManagerUnassessedProposal, error) {
	if limit < 1 || limit > 100 {
		return nil, ports.ErrAgentManagerInvalid
	}
	if after != "" {
		if err := validateAgentManagerProject(domain.ProjectID(after)); err != nil {
			return nil, err
		}
	}
	rows, err := s.qr.ListUnassessedAgentManagerProposals(ctx, gen.ListUnassessedAgentManagerProposalsParams{AfterProposalID: after, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]ports.AgentManagerUnassessedProposal, 0, len(rows))
	for _, row := range rows {
		items = append(items, ports.AgentManagerUnassessedProposal{ProjectID: domain.ProjectID(row.ProjectID), RequestID: row.RequestID, ProposalID: row.ProposalID})
	}
	return items, nil
}

// AgentManagerDecisionCursor reads a scan checkpoint independently of inbox delivery.
func (s *Store) AgentManagerDecisionCursor(ctx context.Context) (string, error) {
	return s.qr.AgentManagerDecisionCursor(ctx)
}

// SetAgentManagerDecisionCursor advances even after a temporarily blocked proposal.
func (s *Store) SetAgentManagerDecisionCursor(ctx context.Context, after string) error {
	if after != "" {
		if err := validateAgentManagerProject(domain.ProjectID(after)); err != nil {
			return err
		}
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	changed, err := s.qw.SetAgentManagerDecisionCursor(ctx, after)
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("manager decision checkpoint is missing")
	}
	return nil
}
