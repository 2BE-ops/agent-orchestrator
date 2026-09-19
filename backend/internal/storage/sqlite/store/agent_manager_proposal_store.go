package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.AgentManagerProposalStore = (*Store)(nil)

func managerProposalFromRow(row gen.AdaptiveAgentManagerProposal) (domain.AgentManagerProposal, error) {
	var proposal domain.AgentManagerProposal
	if err := json.Unmarshal([]byte(row.Snapshot), &proposal); err != nil {
		return proposal, err
	}
	if err := proposal.Validate(); err != nil {
		return proposal, err
	}
	if proposal.ID != row.ID || proposal.RequestID != row.RequestID || proposal.Number != row.Number || proposal.ControllerID != row.ControllerID || string(proposal.SessionID) != row.SessionID || proposal.ContentHash != row.ContentHash || !proposal.CreatedAt.Equal(row.CreatedAt) {
		return proposal, fmt.Errorf("manager proposal identity mismatch")
	}
	return proposal, nil
}

// SubmitAgentManagerProposal preserves native output and bounded parse failures.
// Successful parsing is not validation of permission, compatibility or execution.
func (s *Store) SubmitAgentManagerProposal(ctx context.Context, input domain.AgentManagerProposalSubmission) (domain.AgentManagerProposal, bool, error) {
	if err := input.Validate(); err != nil {
		return domain.AgentManagerProposal{}, false, fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
	}
	generation := resultGeneration(input.SourceOwner)
	if generation == "" || input.SourceOwner.IsTerminated {
		return domain.AgentManagerProposal{}, false, ports.ErrAgentManagerFenced
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.AgentManagerProposal{}, false, err
	}
	defer s.writeMu.Unlock()
	var proposal domain.AgentManagerProposal
	created := false
	err := s.inTx(ctx, "retain Manager native proposal", func(q *gen.Queries) error {
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
		prior, err := q.GetAgentManagerProposalByKey(ctx, gen.GetAgentManagerProposalByKeyParams{RequestID: input.RequestID, IdempotencyKey: input.IdempotencyKey})
		if err == nil {
			proposal, err = managerProposalFromRow(prior)
			if err != nil {
				return err
			}
			var owner domain.SessionControllerOwner
			if err := json.Unmarshal([]byte(prior.SourceOwner), &owner); err != nil {
				return err
			}
			if proposal.SessionID != input.SessionID || proposal.NativeGeneration != generation || proposal.Raw != input.Raw || owner.Mode != input.SourceOwner.Mode || owner.Harness != input.SourceOwner.Harness {
				return ports.ErrAgentManagerConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		configuration, controller, snapshot, err := managerRequestNativeOwner(ctx, q, request, input.SessionID, input.SourceOwner, input.Now)
		if err != nil {
			return err
		}
		history, err := q.ListAgentManagerProposals(ctx, request.ID)
		if err != nil {
			return err
		}
		if len(history) >= configuration.Definition.Policy.MaxProposalAttempts {
			return ports.ErrAgentManagerFenced
		}
		for _, previous := range history {
			p, err := managerProposalFromRow(previous)
			if err != nil {
				return err
			}
			// An unassessed valid selection waits for the deterministic decision
			// boundary. Repeated native messages cannot replace it under a new key.
			if p.Definition != nil {
				return ports.ErrAgentManagerConflict
			}
			if input.Now.Before(p.CreatedAt) {
				return ports.ErrAgentManagerInvalid
			}
		}
		definition, parseErr := domain.ParseAgentManagerProposal(input.Raw)
		proposal = domain.AgentManagerProposal{ID: input.ID, RequestID: request.ID, Number: int64(len(history) + 1), ControllerID: controller.ID, SessionID: input.SessionID, NativeGeneration: generation, ConfigurationHash: snapshot.ContentHash, RequestHash: request.ContentHash, Raw: input.Raw, Definition: definition, CreatedAt: input.Now}
		if parseErr != nil {
			proposal.ValidationError = parseErr.Error()
		}
		proposal.ContentHash = proposal.Hash()
		if err := proposal.Validate(); err != nil {
			return err
		}
		encoded, err := json.Marshal(proposal)
		if err != nil {
			return err
		}
		owner, err := json.Marshal(input.SourceOwner)
		if err != nil {
			return err
		}
		if err := q.InsertAgentManagerProposal(ctx, gen.InsertAgentManagerProposalParams{ID: proposal.ID, RequestID: request.ID, Number: proposal.Number, IdempotencyKey: input.IdempotencyKey, ControllerID: controller.ID, SessionID: string(input.SessionID), SourceOwner: string(owner), Snapshot: string(encoded), ContentHash: proposal.ContentHash, CreatedAt: input.Now}); err != nil {
			return err
		}
		actor := domain.AdaptiveActor{Kind: "AGENT_MANAGER", ID: controller.ID, SessionID: input.SessionID}
		if err := insertManagerInboxAudit(ctx, q, request, "proposal_received", actor, "Retained native Manager proposal "+proposal.ID, input.Now); err != nil {
			return err
		}
		exhausted := parseErr != nil && len(history)+1 >= configuration.Definition.Policy.MaxProposalAttempts
		escalated := definition != nil && definition.Action == "needs_human"
		if exhausted || escalated {
			reason := "Manager proposal correction limit exhausted"
			if escalated {
				reason = "Manager requested human routing assistance; inspect proposal " + proposal.ID
			}
			validator := domain.AdaptiveActor{Kind: "SYSTEM", ID: "manager-proposal-validator"}
			encodedActor, err := json.Marshal(validator)
			if err != nil {
				return err
			}
			if err := q.InsertAgentManagerRequestResolution(ctx, gen.InsertAgentManagerRequestResolutionParams{RequestID: request.ID, Outcome: "needs_human", Actor: string(encodedActor), Reason: reason, CreatedAt: input.Now}); err != nil {
				return err
			}
			if err := insertManagerInboxAudit(ctx, q, request, "request_needs_human", validator, reason, input.Now); err != nil {
				return err
			}
		}
		created = true
		return nil
	})
	if err != nil {
		return domain.AgentManagerProposal{}, false, err
	}
	return proposal, created, nil
}

// GetAgentManagerProposal reads exact native output within its project/request.
func (s *Store) GetAgentManagerProposal(ctx context.Context, project domain.ProjectID, requestID, id string) (domain.AgentManagerProposal, error) {
	if _, err := s.GetAgentManagerRequest(ctx, project, requestID); err != nil {
		return domain.AgentManagerProposal{}, err
	}
	row, err := s.qr.GetAgentManagerProposal(ctx, id)
	if err != nil {
		return domain.AgentManagerProposal{}, agentManagerReadError(err)
	}
	if row.RequestID != requestID {
		return domain.AgentManagerProposal{}, ports.ErrAgentManagerNotFound
	}
	return managerProposalFromRow(row)
}

// ListAgentManagerProposals returns the bounded correction history (at most five).
func (s *Store) ListAgentManagerProposals(ctx context.Context, project domain.ProjectID, requestID string) ([]domain.AgentManagerProposal, error) {
	if _, err := s.GetAgentManagerRequest(ctx, project, requestID); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListAgentManagerProposals(ctx, requestID)
	if err != nil {
		return nil, err
	}
	items := make([]domain.AgentManagerProposal, 0, len(rows))
	for _, row := range rows {
		item, err := managerProposalFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
