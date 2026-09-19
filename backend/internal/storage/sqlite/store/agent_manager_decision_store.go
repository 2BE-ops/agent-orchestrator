package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.AgentManagerDecisionStore = (*Store)(nil)

func managerDecisionFromRow(row gen.AdaptiveAgentManagerDecision) (domain.AgentManagerDecision, error) {
	var decision domain.AgentManagerDecision
	if err := json.Unmarshal([]byte(row.Snapshot), &decision); err != nil {
		return decision, err
	}
	if err := decision.Validate(); err != nil {
		return decision, err
	}
	if decision.ProposalID != row.ProposalID || decision.RequestID != row.RequestID || decision.Outcome != row.Outcome || decision.ContentHash != row.ContentHash || !decision.CreatedAt.Equal(row.CreatedAt) {
		return decision, fmt.Errorf("manager decision identity mismatch")
	}
	return decision, nil
}

// RecordAgentManagerDecision atomically retains deterministic observations and
// resolves accepted routing. Repeating a proposal ID inspects its first decision;
// a changed observation never replaces history or authorizes a second dispatch.
func (s *Store) RecordAgentManagerDecision(ctx context.Context, input domain.AgentManagerAssessment) (domain.AgentManagerDecision, bool, error) {
	if err := input.Validate(); err != nil {
		return domain.AgentManagerDecision{}, false, fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.AgentManagerDecision{}, false, err
	}
	defer s.writeMu.Unlock()
	var decision domain.AgentManagerDecision
	created := false
	err := s.inTx(ctx, "record Manager selection decision", func(q *gen.Queries) error {
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
		prior, err := q.GetAgentManagerDecision(ctx, input.ProposalID)
		if err == nil {
			decision, err = managerDecisionFromRow(prior)
			if err != nil {
				return err
			}
			if decision.RequestID != request.ID {
				return ports.ErrAgentManagerNotFound
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := q.GetAgentManagerRequestResolution(ctx, request.ID); err == nil {
			return ports.ErrAgentManagerFenced
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		configuration, err := enabledManagerConfiguration(ctx, q, request.ProjectID)
		if err != nil {
			return err
		}
		if configuration.Number != request.ConfigurationVersion || configuration.ContentHash != request.ConfigurationHash {
			return ports.ErrAgentManagerFenced
		}
		task, err := q.GetAdaptiveTask(ctx, request.TaskID)
		if err != nil {
			return err
		}
		if task.Revision != request.TaskRevision {
			return ports.ErrAgentManagerFenced
		}
		if err := requireTaskRunIntent(ctx, q, task.ID); err != nil {
			return err
		}
		proposalRow, err := q.GetAgentManagerProposal(ctx, input.ProposalID)
		if err != nil {
			return agentManagerReadError(err)
		}
		proposal, err := managerProposalFromRow(proposalRow)
		if err != nil {
			return err
		}
		if proposal.RequestID != request.ID {
			return ports.ErrAgentManagerNotFound
		}
		if proposal.ContextID == "" || proposal.Definition == nil || proposal.Definition.Action != "select_existing" || proposal.RequestHash != request.ContentHash {
			return ports.ErrAgentManagerInvalid
		}
		now := time.Now().UTC()
		if input.ObservedAt.Before(proposal.CreatedAt) || now.Before(input.ObservedAt) {
			return ports.ErrAgentManagerInvalid
		}
		if !managerAssessmentMatchesProposal(input.Candidates, *proposal.Definition) {
			return ports.ErrAgentManagerInvalid
		}
		projectRow, err := q.GetProject(ctx, request.ProjectID)
		if err != nil {
			return err
		}
		project := projectRowFromGen(projectRow)
		_, projectHash, err := domain.TaskContent(project.Config)
		if err != nil {
			return err
		}
		if projectHash != input.ProjectConfigurationHash {
			return ports.ErrAgentManagerConflict
		}
		if input.Candidates[0].Eligible {
			revisionRow, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: request.TaskID, Number: request.TaskRevision})
			if err != nil {
				return err
			}
			revision, err := taskRevisionFromRow(revisionRow)
			if err != nil {
				return err
			}
			if revision.ContentHash != request.TaskContentHash {
				return ports.ErrAgentManagerFenced
			}
			if err := validateManagerChoice(ctx, q, input.Candidates[0], revision.Definition, project); err != nil {
				return err
			}
		}
		decision = domain.AgentManagerDecision{SchemaVersion: 1, ProjectID: request.ProjectID, RequestID: request.ID, ProposalID: proposal.ID, RequestHash: request.ContentHash, ProposalHash: proposal.ContentHash, ConfigurationHash: configuration.ContentHash, ProjectConfigurationHash: projectHash, Optimization: configuration.Definition.Policy.Optimization, Classification: proposal.Classification, EngagementID: proposal.EngagementID, Candidates: input.Candidates, Outcome: "rejected", ObservedAt: input.ObservedAt, CreatedAt: now}
		if input.Candidates[0].Eligible {
			decision.Outcome = "accepted"
		}
		decision.ContentHash = decision.Hash()
		if err := decision.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
		}
		encoded, err := json.Marshal(decision)
		if err != nil {
			return err
		}
		if err := q.InsertAgentManagerDecision(ctx, gen.InsertAgentManagerDecisionParams{ProposalID: proposal.ID, RequestID: request.ID, Outcome: decision.Outcome, Snapshot: string(encoded), ContentHash: decision.ContentHash, CreatedAt: now}); err != nil {
			return err
		}
		actor := domain.AdaptiveActor{Kind: "SYSTEM", ID: "manager-selector"}
		if err := insertManagerInboxAudit(ctx, q, request, "decision_"+decision.Outcome, actor, "Assessed native Manager proposal "+proposal.ID, now); err != nil {
			return err
		}
		if decision.Outcome == "accepted" || proposal.Number >= int64(configuration.Definition.Policy.MaxProposalAttempts) {
			outcome, reason := "selected", "Recorded validated Manager selection "+proposal.ID
			if decision.Outcome == "rejected" {
				outcome, reason = "needs_human", "Manager semantic correction limit exhausted"
			}
			encodedActor, err := json.Marshal(actor)
			if err != nil {
				return err
			}
			if err := q.InsertAgentManagerRequestResolution(ctx, gen.InsertAgentManagerRequestResolutionParams{RequestID: request.ID, Outcome: outcome, Actor: string(encodedActor), Reason: reason, CreatedAt: now}); err != nil {
				return err
			}
			if err := insertManagerInboxAudit(ctx, q, request, "request_"+outcome, actor, reason, now); err != nil {
				return err
			}
		}
		created = true
		return nil
	})
	if err != nil {
		return domain.AgentManagerDecision{}, false, err
	}
	return decision, created, nil
}

func managerAssessmentMatchesProposal(candidates []domain.AgentManagerCandidate, proposal domain.AgentManagerProposalDefinition) bool {
	refs := []domain.SkillVersionRef{{ID: proposal.AgentTypeID, Version: proposal.AgentTypeVersion}}
	for _, candidate := range proposal.Candidates {
		ref := domain.SkillVersionRef{ID: candidate.AgentTypeID, Version: candidate.Version}
		if !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	if len(candidates) != len(refs) {
		return false
	}
	for i, ref := range refs {
		if candidates[i].AgentType.ID != ref.ID || candidates[i].AgentType.Version != ref.Version {
			return false
		}
	}
	return true
}

// validateManagerChoice rechecks mutable permissions and exact content pins in
// the commit transaction. Native readiness remains an observation; the scheduler
// must repeat native checks before side effects. No LLM-supplied clearance,
// capability, Skill, provider or inherited native option can bypass this gate.
func validateManagerChoice(ctx context.Context, q *gen.Queries, candidate domain.AgentManagerCandidate, task domain.TaskDefinition, project domain.ProjectRecord) error {
	if err := validateWorkerReference(ctx, q, candidate.AgentType, domain.RegistryAgentType, domain.RegistryManager); err != nil {
		return err
	}
	entry, err := q.GetRegistryEntry(ctx, candidate.AgentType.ID)
	if err != nil {
		return err
	}
	if entry.Revision != candidate.MetadataRevision || entry.Name != candidate.AgentType.Name {
		return ports.ErrAgentManagerConflict
	}
	row, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: entry.ID, Number: candidate.AgentType.Version})
	if err != nil {
		return err
	}
	version, err := registryVersionFromGen(row)
	if err != nil {
		return err
	}
	if version.Definition.AgentType == nil {
		return ports.ErrAgentManagerInvalid
	}
	definition := domain.ResolveWorkerOptions(*version.Definition.AgentType, domain.WorkerOverrides{}, project.Config, candidate.SessionMode)
	if definition.MaxContextClass.Effective() != candidate.MaxContextClass || !definition.MaxContextClass.Allows(task.Classification) {
		return ports.ErrAgentManagerForbidden
	}
	if definition.Harness != candidate.Harness || definition.SessionMode != candidate.SessionMode || definition.Config != candidate.Config || definition.ProviderBindingID != candidate.ProviderBindingID || definition.ProviderBindingRequired || len(definition.Skills) != len(candidate.Skills) {
		return ports.ErrAgentManagerInvalid
	}
	capabilities := append([]string{}, definition.Capabilities...)
	for i, pin := range definition.Skills {
		skill := candidate.Skills[i]
		if pin.ID != skill.Reference.ID || pin.Version != skill.Reference.Version {
			return ports.ErrAgentManagerInvalid
		}
		if err := validateWorkerReference(ctx, q, skill.Reference, domain.RegistrySkill, domain.RegistryManager); err != nil {
			return err
		}
		skillEntry, err := q.GetRegistryEntry(ctx, pin.ID)
		if err != nil {
			return err
		}
		if skillEntry.Revision != skill.MetadataRevision || skillEntry.Name != skill.Reference.Name {
			return ports.ErrAgentManagerConflict
		}
		skillRow, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: pin.ID, Number: pin.Version})
		if err != nil {
			return err
		}
		skillVersion, err := registryVersionFromGen(skillRow)
		if err != nil {
			return err
		}
		if skillVersion.Definition.Skill == nil {
			return ports.ErrAgentManagerInvalid
		}
		for _, capability := range skillVersion.Definition.Skill.Capabilities {
			if !slices.Contains(capabilities, capability) {
				capabilities = append(capabilities, capability)
			}
		}
	}
	if !slices.Equal(capabilities, candidate.Capabilities) {
		return ports.ErrAgentManagerInvalid
	}
	for _, required := range task.RequiredCapabilities {
		if !slices.Contains(capabilities, required) {
			return ports.ErrAgentManagerForbidden
		}
	}
	if candidate.ProviderBindingID != "" {
		binding, err := q.GetProviderBinding(ctx, candidate.ProviderBindingID)
		if err != nil {
			return registryReadError(err)
		}
		if binding.Enabled == 0 || binding.Harness != string(candidate.Harness) || (binding.ProjectID != "" && binding.ProjectID != project.ID) {
			return ports.ErrRegistryForbidden
		}
		if binding.Revision != candidate.BindingRevision {
			return ports.ErrAgentManagerConflict
		}
	} else if candidate.BindingRevision != 0 {
		return ports.ErrAgentManagerInvalid
	}
	return nil
}

// GetAgentManagerDecision inspects exact history even after policy or ownership changes.
func (s *Store) GetAgentManagerDecision(ctx context.Context, project domain.ProjectID, requestID, proposalID string) (domain.AgentManagerDecision, bool, error) {
	if _, err := s.GetAgentManagerProposal(ctx, project, requestID, proposalID); err != nil {
		return domain.AgentManagerDecision{}, false, err
	}
	row, err := s.qr.GetAgentManagerDecision(ctx, proposalID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentManagerDecision{}, false, nil
	}
	if err != nil {
		return domain.AgentManagerDecision{}, false, err
	}
	decision, err := managerDecisionFromRow(row)
	return decision, err == nil, err
}

// ListAgentManagerDecisions returns at most five retained proposal assessments.
func (s *Store) ListAgentManagerDecisions(ctx context.Context, project domain.ProjectID, requestID string) ([]domain.AgentManagerDecision, error) {
	if _, err := s.GetAgentManagerRequest(ctx, project, requestID); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListAgentManagerDecisions(ctx, requestID)
	if err != nil {
		return nil, err
	}
	items := make([]domain.AgentManagerDecision, 0, len(rows))
	for _, row := range rows {
		decision, err := managerDecisionFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, decision)
	}
	return items, nil
}
