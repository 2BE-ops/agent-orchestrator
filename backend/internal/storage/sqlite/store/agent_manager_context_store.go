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

var _ ports.AgentManagerContextStore = (*Store)(nil)

func managerContextFromRow(row gen.AdaptiveAgentManagerContext) (domain.AgentManagerContext, error) {
	var sealed domain.AgentManagerContext
	if err := json.Unmarshal([]byte(row.Snapshot), &sealed); err != nil {
		return sealed, err
	}
	if err := sealed.Validate(); err != nil {
		return sealed, err
	}
	if sealed.ID != row.ID || sealed.RequestID != row.RequestID || sealed.Number != row.Number || sealed.ControllerID != row.ControllerID || string(sealed.SessionID) != row.SessionID || sealed.NativeGeneration != row.NativeGeneration || string(sealed.Classification) != row.Classification || sealed.EngagementID != row.EngagementID || sealed.PreviousContextHash != row.PreviousContextHash || sealed.ContentHash != row.ContentHash || !sealed.CreatedAt.Equal(row.CreatedAt) {
		return sealed, fmt.Errorf("manager context identity mismatch")
	}
	return sealed, nil
}

// SealAgentManagerContext loads exact request content only after checking live
// ownership and the pinned Type clearance. The append-only conversation chain
// retains scope and sensitivity even when a native generation is replaced.
func (s *Store) SealAgentManagerContext(ctx context.Context, input domain.AgentManagerContextSeal) (domain.AgentManagerContext, bool, error) {
	if err := input.Validate(); err != nil {
		return domain.AgentManagerContext{}, false, fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
	}
	generation := resultGeneration(input.SourceOwner)
	if generation == "" || input.SourceOwner.IsTerminated {
		return domain.AgentManagerContext{}, false, ports.ErrAgentManagerFenced
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.AgentManagerContext{}, false, err
	}
	defer s.writeMu.Unlock()
	var sealed domain.AgentManagerContext
	created := false
	err := s.inTx(ctx, "seal Manager context", func(q *gen.Queries) error {
		var err error
		sealed, created, err = sealManagerContext(ctx, q, input)
		return err
	})
	if err != nil {
		return domain.AgentManagerContext{}, false, err
	}
	return sealed, created, nil
}

// sealManagerContext participates in both an explicit seal and an atomic native
// delivery reservation; neither path commits partial context or audit state.
func sealManagerContext(ctx context.Context, q *gen.Queries, input domain.AgentManagerContextSeal) (domain.AgentManagerContext, bool, error) {
	generation := resultGeneration(input.SourceOwner)
	var sealed domain.AgentManagerContext
	created := false
	err := func() error {
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
		prior, err := q.GetAgentManagerContext(ctx, input.ID)
		if err == nil {
			sealed, err = managerContextFromRow(prior)
			if err != nil {
				return err
			}
			var owner domain.SessionControllerOwner
			if err := json.Unmarshal([]byte(prior.SourceOwner), &owner); err != nil {
				return err
			}
			if sealed.RequestID != request.ID || sealed.SessionID != input.SessionID || owner != input.SourceOwner {
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
		versionRow, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: snapshot.AgentType.ID, Number: snapshot.AgentType.Version})
		if err != nil {
			return err
		}
		version, err := registryVersionFromGen(versionRow)
		if err != nil {
			return err
		}
		if version.ContentHash != snapshot.AgentType.ContentHash || version.Definition.AgentType == nil || version.Definition.AgentType.MaxContextClass.Effective() != snapshot.Effective.MaxContextClass.Effective() {
			return ports.ErrAgentManagerFenced
		}
		clearance := version.Definition.AgentType.MaxContextClass.Effective()
		taskRow, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: request.TaskID, Number: request.TaskRevision})
		if err != nil {
			return err
		}
		task, err := taskRevisionFromRow(taskRow)
		if err != nil {
			return err
		}
		criteriaRow, err := q.GetAdaptiveTaskCriteria(ctx, gen.GetAdaptiveTaskCriteriaParams{TaskID: request.TaskID, Number: request.CriteriaVersion})
		if err != nil {
			return err
		}
		criteria, err := taskCriteriaFromRow(criteriaRow)
		if err != nil {
			return err
		}
		if task.ContentHash != request.TaskContentHash || task.CriteriaVersion != request.CriteriaVersion || criteria.ContentHash != request.CriteriaContentHash {
			return ports.ErrAgentManagerFenced
		}
		class, engagement := task.Definition.Classification.Effective(), task.Definition.EngagementID
		if !domain.CanEmbedContext(clearance, engagement, class, engagement) {
			return fmt.Errorf("%w: Manager task exceeds pinned context clearance", ports.ErrAgentManagerForbidden)
		}
		sealed = domain.AgentManagerContext{ID: input.ID, SchemaVersion: 1, RequestID: request.ID, ControllerID: controller.ID, SessionID: input.SessionID, NativeGeneration: generation, ConfigurationHash: snapshot.ContentHash, RequestHash: request.ContentHash, MaxContextClass: clearance, Classification: class, EngagementID: engagement, Policy: configuration.Definition.Policy, CreatedAt: input.Now}
		previousRow, err := q.LatestAgentManagerContext(ctx, controller.ID)
		if err == nil {
			previous, err := managerContextFromRow(previousRow)
			if err != nil {
				return err
			}
			if input.Now.Before(previous.CreatedAt) {
				return ports.ErrAgentManagerInvalid
			}
			if previous.EngagementID != "" {
				if engagement != "" && engagement != previous.EngagementID {
					return fmt.Errorf("%w: Manager conversation belongs to another engagement", ports.ErrAgentManagerForbidden)
				}
				sealed.EngagementID = previous.EngagementID
			}
			if previous.Classification.Allows(class) {
				sealed.Classification = previous.Classification
			}
			sealed.PreviousContextHash = previous.ContentHash
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		history, err := q.ListAgentManagerContexts(ctx, request.ID)
		if err != nil {
			return err
		}
		count, err := q.CountAgentManagerContexts(ctx, controller.ID)
		if err != nil {
			return err
		}
		if len(history) >= 32 || count >= 10000 {
			return fmt.Errorf("%w: Manager context history bound reached", ports.ErrAgentManagerConflict)
		}
		sealed.Number = int64(len(history) + 1)
		for _, source := range []struct {
			kind       string
			number     int64
			hash       string
			definition any
		}{{"task", task.Number, task.ContentHash, task.Definition}, {"criteria", criteria.Number, criteria.ContentHash, criteria.Definition}} {
			content, hash, err := domain.TaskContent(source.definition)
			if err != nil {
				return err
			}
			sealed.Sources = append(sealed.Sources, domain.ContextSource{Kind: source.kind, ID: task.TaskID, Version: source.number, SourceHash: source.hash, Content: string(content), ContentHash: hash, Classification: class, EngagementID: engagement, Disposition: "inline", Reason: "Exact routing request revision"})
		}
		sealed.Prompt, err = sealed.RenderPrompt()
		if err != nil {
			return err
		}
		sealed.ContentHash = sealed.Hash()
		if err := sealed.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
		}
		encoded, err := json.Marshal(sealed)
		if err != nil {
			return err
		}
		owner, err := json.Marshal(input.SourceOwner)
		if err != nil {
			return err
		}
		if err := q.InsertAgentManagerContext(ctx, gen.InsertAgentManagerContextParams{ID: sealed.ID, RequestID: request.ID, Number: sealed.Number, ControllerID: controller.ID, SessionID: string(input.SessionID), NativeGeneration: generation, SourceOwner: string(owner), Classification: string(sealed.Classification), EngagementID: sealed.EngagementID, PreviousContextHash: sealed.PreviousContextHash, Snapshot: string(encoded), ContentHash: sealed.ContentHash, CreatedAt: input.Now}); err != nil {
			return err
		}
		if err := insertManagerInboxAudit(ctx, q, request, "context_sealed", domain.AdaptiveActor{Kind: "SYSTEM", ID: controller.ID}, "Sealed Manager context "+sealed.ID, input.Now); err != nil {
			return err
		}
		created = true
		return nil
	}()
	if err != nil {
		return domain.AgentManagerContext{}, false, err
	}
	return sealed, created, nil
}

// GetAgentManagerContext keeps exact receipts inspectable after task/policy edits.
func (s *Store) GetAgentManagerContext(ctx context.Context, project domain.ProjectID, requestID, id string) (domain.AgentManagerContext, error) {
	if _, err := s.GetAgentManagerRequest(ctx, project, requestID); err != nil {
		return domain.AgentManagerContext{}, err
	}
	row, err := s.qr.GetAgentManagerContext(ctx, id)
	if err != nil {
		return domain.AgentManagerContext{}, agentManagerReadError(err)
	}
	if row.RequestID != requestID {
		return domain.AgentManagerContext{}, ports.ErrAgentManagerNotFound
	}
	return managerContextFromRow(row)
}

// ListAgentManagerContexts returns at most 32 immutable input versions.
func (s *Store) ListAgentManagerContexts(ctx context.Context, project domain.ProjectID, requestID string) ([]domain.AgentManagerContext, error) {
	if _, err := s.GetAgentManagerRequest(ctx, project, requestID); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListAgentManagerContexts(ctx, requestID)
	if err != nil {
		return nil, err
	}
	items := make([]domain.AgentManagerContext, 0, len(rows))
	for _, row := range rows {
		item, err := managerContextFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
