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

var _ ports.AgentManagerRegistryStore = (*Store)(nil)

func managerRegistryReceiptFromRow(row gen.AdaptiveAgentManagerRegistryAction) (domain.AgentManagerRegistryReceipt, error) {
	var receipt domain.AgentManagerRegistryReceipt
	if err := json.Unmarshal([]byte(row.Snapshot), &receipt); err != nil {
		return receipt, err
	}
	if err := receipt.Validate(); err != nil {
		return receipt, err
	}
	if receipt.ID != row.ID || string(receipt.ProjectID) != row.ProjectID || receipt.RequestID != row.RequestID || receipt.Action.Action != row.Action || string(receipt.Action.Kind) != row.Kind || receipt.Target.ID != row.EntryID || receipt.Target.Version != row.Version || receipt.ContentHash != row.ContentHash || !receipt.CreatedAt.Equal(row.CreatedAt) {
		return receipt, fmt.Errorf("manager registry receipt identity mismatch")
	}
	return receipt, nil
}

// ApplyAgentManagerRegistryAction performs the same registry writes as manual
// authoring while atomically enforcing native ownership, governance and quotas.
func (s *Store) ApplyAgentManagerRegistryAction(ctx context.Context, input domain.AgentManagerRegistrySubmission) (domain.AgentManagerRegistryReceipt, bool, error) {
	if err := input.Validate(); err != nil {
		return domain.AgentManagerRegistryReceipt{}, false, fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
	}
	generation := resultGeneration(input.SourceOwner)
	if generation == "" || input.SourceOwner.IsTerminated {
		return domain.AgentManagerRegistryReceipt{}, false, ports.ErrAgentManagerFenced
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.AgentManagerRegistryReceipt{}, false, err
	}
	defer s.writeMu.Unlock()
	var receipt domain.AgentManagerRegistryReceipt
	created := false
	err := s.inTx(ctx, "apply governed Manager registry action", func(q *gen.Queries) error {
		row, err := q.GetAgentManagerRequest(ctx, input.RequestID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if row.ProjectID != string(input.ProjectID) {
			return ports.ErrAgentManagerNotFound
		}
		request, err := managerRequestFromRow(row)
		if err != nil {
			return err
		}
		prior, err := q.GetAgentManagerRegistryActionByKey(ctx, gen.GetAgentManagerRegistryActionByKeyParams{RequestID: request.ID, IdempotencyKey: input.IdempotencyKey})
		if err == nil {
			receipt, err = managerRegistryReceiptFromRow(prior)
			if err != nil {
				return err
			}
			var owner domain.SessionControllerOwner
			if err := json.Unmarshal([]byte(prior.SourceOwner), &owner); err != nil {
				return err
			}
			_, submittedHash, err := domain.TaskContent(input.Action)
			if err != nil {
				return err
			}
			_, retainedHash, err := domain.TaskContent(receipt.Action)
			if err != nil {
				return err
			}
			if receipt.SessionID != input.SessionID || receipt.NativeGeneration != generation || owner != input.SourceOwner || submittedHash != retainedHash {
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
		sealed, conversation, err := managerReceivedContext(ctx, q, request, controller.ID, input.SessionID, generation, snapshot.ContentHash, input.Now)
		if err != nil {
			return err
		}
		// Instructions and registry descriptions are reusable technical material.
		// Receiving clearance is not permission to downgrade authored content.
		if conversation.Classification != domain.ContextTechnical {
			return fmt.Errorf("%w: classified native output requires review before reusable authoring", ports.ErrAgentManagerForbidden)
		}
		if err := managerRegistryQuota(ctx, q, request, configuration.Definition.Policy, input.Action); err != nil {
			return err
		}
		mutation := domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryManager, ID: controller.ID}, ExpectedRevision: input.Action.ExpectedRevision, Reason: input.Action.Reason}
		entry, version, err := applyManagerRegistryDefinition(ctx, q, input, mutation)
		if err != nil {
			return err
		}
		receipt = domain.AgentManagerRegistryReceipt{
			SchemaVersion: 1, ID: input.ID, ProjectID: request.ProjectID, RequestID: request.ID,
			RequestHash: request.ContentHash, ConfigurationHash: configuration.ContentHash, ControllerID: controller.ID,
			SessionID: input.SessionID, NativeGeneration: generation, WorkerConfigurationHash: snapshot.ContentHash,
			ContextID: sealed.ID, ContextHash: sealed.ContentHash, ConversationContextHash: conversation.ContentHash,
			Classification: conversation.Classification, EngagementID: conversation.EngagementID, Action: input.Action,
			Target:           domain.WorkerDefinitionRef{ID: entry.ID, Version: version.Number, Name: entry.Metadata.Name, ContentHash: version.ContentHash},
			MetadataRevision: entry.Revision, CreatedAt: input.Now,
		}
		receipt.ContentHash = receipt.Hash()
		if err := receipt.Validate(); err != nil {
			return err
		}
		encoded, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		owner, err := json.Marshal(input.SourceOwner)
		if err != nil {
			return err
		}
		if err := q.InsertAgentManagerRegistryAction(ctx, gen.InsertAgentManagerRegistryActionParams{ID: receipt.ID, ProjectID: string(request.ProjectID), RequestID: request.ID, IdempotencyKey: input.IdempotencyKey, Action: input.Action.Action, Kind: string(input.Action.Kind), EntryID: entry.ID, Version: version.Number, SourceOwner: string(owner), Snapshot: string(encoded), ContentHash: receipt.ContentHash, CreatedAt: input.Now}); err != nil {
			return err
		}
		actor := domain.AdaptiveActor{Kind: "AGENT_MANAGER", ID: controller.ID, SessionID: input.SessionID}
		if err := insertManagerInboxAudit(ctx, q, request, "registry_"+input.Action.Action, actor, "Applied governed registry action "+receipt.ID, input.Now); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return domain.AgentManagerRegistryReceipt{}, false, err
	}
	return receipt, created, nil
}

func managerRegistryQuota(ctx context.Context, q *gen.Queries, request domain.AgentManagerRequest, policy domain.AgentManagerPolicy, action domain.AgentManagerRegistryAction) error {
	count, err := q.CountAgentManagerRegistryActions(ctx, request.ID)
	if err != nil {
		return err
	}
	if count >= 64 {
		return fmt.Errorf("%w: request registry action limit reached", ports.ErrAgentManagerForbidden)
	}
	if action.Action == "append_version" {
		if !policy.AllowCreateVersions {
			return ports.ErrAgentManagerForbidden
		}
		// The entry-wide count includes other projects and older internal Manager
		// writes; changing configuration or controller never resets this limit.
		count, err = q.CountAgentManagerAppendedVersions(ctx, action.EntryID)
		if err != nil {
			return err
		}
		if count >= int64(policy.MaxVersionsPerEntry) {
			return fmt.Errorf("%w: Manager version limit reached", ports.ErrAgentManagerForbidden)
		}
		return nil
	}
	allowed, limit := policy.AllowCreateTypes, policy.MaxCreatedTypes
	if action.Kind == domain.RegistrySkill {
		allowed, limit = policy.AllowCreateSkills, policy.MaxCreatedSkills
	}
	if !allowed {
		return ports.ErrAgentManagerForbidden
	}
	count, err = q.CountAgentManagerCreatedEntries(ctx, gen.CountAgentManagerCreatedEntriesParams{ProjectID: string(request.ProjectID), Kind: string(action.Kind)})
	if err != nil {
		return err
	}
	if count >= int64(limit) {
		return fmt.Errorf("%w: Manager creation limit reached", ports.ErrAgentManagerForbidden)
	}
	return nil
}

func applyManagerRegistryDefinition(ctx context.Context, q *gen.Queries, input domain.AgentManagerRegistrySubmission, mutation domain.RegistryMutation) (domain.RegistryEntry, domain.RegistryVersion, error) {
	var entry domain.RegistryEntry
	var version domain.RegistryVersion
	if input.Action.Action == "create" {
		content, hash, err := input.Action.Definition.MarshalContent(input.Action.Kind)
		if err != nil {
			return entry, version, err
		}
		metadata := domain.RegistryMetadata{Name: input.Action.Name, Description: input.Action.Description, Enabled: true,
			Policy: domain.RegistryPolicy{ManagerCanSelect: true, ManagerCanModify: true, ManagerCanVersion: true}}
		entry, err = createRegistryEntry(ctx, q, domain.RegistryCreate{ID: input.CreatedEntryID, Kind: input.Action.Kind, Metadata: metadata, Definition: input.Action.Definition}, mutation, content, hash, nil, input.Now)
		if err != nil {
			return entry, version, err
		}
		version = domain.RegistryVersion{EntryID: entry.ID, Number: 1, ContentHash: hash}
		return entry, version, nil
	}
	entry, err := registryMutationEntry(ctx, q, input.Action.EntryID, mutation)
	if err != nil {
		return entry, version, err
	}
	if entry.Kind != input.Action.Kind {
		return entry, version, ports.ErrRegistryNotFound
	}
	if !entry.Metadata.Enabled {
		return entry, version, ports.ErrRegistryForbidden
	}
	version, err = appendRegistryVersion(ctx, q, entry.ID, input.Action.Definition, mutation, input.Now)
	if err != nil {
		return entry, version, err
	}
	entry.Revision++
	return entry, version, nil
}

// GetAgentManagerRegistryReceipt keeps old exact versions inspectable after
// disabling definitions, changing governance or replacing native controllers.
func (s *Store) GetAgentManagerRegistryReceipt(ctx context.Context, project domain.ProjectID, requestID, id string) (domain.AgentManagerRegistryReceipt, error) {
	if _, err := s.GetAgentManagerRequest(ctx, project, requestID); err != nil {
		return domain.AgentManagerRegistryReceipt{}, err
	}
	row, err := s.qr.GetAgentManagerRegistryAction(ctx, id)
	if err != nil {
		return domain.AgentManagerRegistryReceipt{}, agentManagerReadError(err)
	}
	if row.RequestID != requestID || row.ProjectID != string(project) {
		return domain.AgentManagerRegistryReceipt{}, ports.ErrAgentManagerNotFound
	}
	return managerRegistryReceiptFromRow(row)
}

// ListAgentManagerRegistryReceipts pages a request's retained actions (at most
// 64 per request); each call may fetch up to 100 receipts.
func (s *Store) ListAgentManagerRegistryReceipts(ctx context.Context, project domain.ProjectID, requestID, afterID string, limit int) ([]domain.AgentManagerRegistryReceipt, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: registry receipt page limit must be between 1 and 100", ports.ErrAgentManagerInvalid)
	}
	if afterID != "" {
		if err := validateAgentManagerProject(domain.ProjectID(afterID)); err != nil {
			return nil, err
		}
	}
	if _, err := s.GetAgentManagerRequest(ctx, project, requestID); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListAgentManagerRegistryActions(ctx, gen.ListAgentManagerRegistryActionsParams{RequestID: requestID, AfterID: afterID, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]domain.AgentManagerRegistryReceipt, 0, len(rows))
	for _, row := range rows {
		receipt, err := managerRegistryReceiptFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, receipt)
	}
	return items, nil
}
