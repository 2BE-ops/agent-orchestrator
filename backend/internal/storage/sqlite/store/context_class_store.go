package store

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// GetTaskContextPolicy never consults active_version or mutable Type metadata.
func (s *Store) GetTaskContextPolicy(ctx context.Context, attemptID string) (domain.TaskContextPolicy, error) {
	policy, _, _, err := taskContextPolicy(ctx, s.qr, attemptID)
	return policy, err
}

func taskContextPolicy(ctx context.Context, q *gen.Queries, attemptID string) (domain.TaskContextPolicy, domain.TaskAttempt, domain.TaskRevision, error) {
	var policy domain.TaskContextPolicy
	var attempt domain.TaskAttempt
	var revision domain.TaskRevision
	row, err := q.GetTaskAttempt(ctx, attemptID)
	if err != nil {
		return policy, attempt, revision, taskReadError(err)
	}
	attempt, err = taskAttemptFromRow(row)
	if err != nil {
		return policy, attempt, revision, err
	}
	r, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: attempt.TaskID, Number: attempt.TaskRevision})
	if err != nil {
		return policy, attempt, revision, taskReadError(err)
	}
	revision, err = taskRevisionFromRow(r)
	if err != nil {
		return policy, attempt, revision, err
	}
	dispatch, err := q.GetTaskWorkerDispatch(ctx, attemptID)
	if err != nil {
		return policy, attempt, revision, taskReadError(err)
	}
	configRow, err := q.GetWorkerConfiguration(ctx, dispatch.SessionID)
	if err != nil {
		return policy, attempt, revision, err
	}
	config, err := workerConfigurationFromRow(configRow)
	if err != nil {
		return policy, attempt, revision, err
	}
	versionRow, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: config.AgentType.ID, Number: config.AgentType.Version})
	if err != nil {
		return policy, attempt, revision, err
	}
	version, err := registryVersionFromGen(versionRow)
	if err != nil {
		return policy, attempt, revision, err
	}
	if config.ContentHash != dispatch.ConfigurationHash || version.ContentHash != config.AgentType.ContentHash || version.Definition.AgentType == nil || config.Effective.MaxContextClass.Effective() != version.Definition.AgentType.MaxContextClass.Effective() {
		return policy, attempt, revision, ports.ErrTaskConflict
	}
	policy = domain.TaskContextPolicy{MaxContextClass: version.Definition.AgentType.MaxContextClass.Effective(), Classification: revision.Definition.Classification.Effective(), EngagementID: revision.Definition.EngagementID}
	if !domain.CanEmbedContext(policy.MaxContextClass, policy.EngagementID, policy.Classification, policy.EngagementID) {
		return policy, attempt, revision, fmt.Errorf("%w: task exceeds pinned context clearance", ports.ErrTaskForbidden)
	}
	return policy, attempt, revision, nil
}

func taskOutputScope(ctx context.Context, q *gen.Queries, attemptID string) (domain.ContextClass, string, error) {
	row, err := q.GetTaskContext(ctx, attemptID)
	if err != nil {
		return "", "", taskReadError(err)
	}
	snapshot, err := taskContextFromRow(row)
	return snapshot.Classification.Effective(), snapshot.EngagementID, err
}

// validateContextClassifications checks source labels against durable content,
// including omitted-source metadata. A caller cannot launder a sealed result or
// relabel a knowledge entry, even with a self-consistent forged manifest hash.
func validateContextClassifications(ctx context.Context, q *gen.Queries, snapshot domain.TaskContextSnapshot) error {
	policy, _, revision, err := taskContextPolicy(ctx, q, snapshot.AttemptID)
	if err != nil {
		return err
	}
	if snapshot.MaxContextClass.Effective() != policy.MaxContextClass || snapshot.EngagementID != policy.EngagementID {
		return ports.ErrTaskForbidden
	}
	aggregate := policy.Classification
	for _, source := range snapshot.Sources {
		class, engagement := domain.ContextTechnical, ""
		switch source.Kind {
		case "task", "criteria", "file":
			class, engagement = policy.Classification, policy.EngagementID
		case "parent", "dependency":
			row, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: source.ID, Number: source.Version})
			if err != nil {
				return taskReadError(err)
			}
			version, err := taskRevisionFromRow(row)
			if err != nil {
				return err
			}
			class, engagement = version.Definition.Classification.Effective(), version.Definition.EngagementID
		case "knowledge":
			row, err := q.GetKnowledgeVersion(ctx, gen.GetKnowledgeVersionParams{KnowledgeID: source.ID, Number: source.Version})
			if err != nil {
				return taskReadError(err)
			}
			version, err := knowledgeVersionFromRow(row)
			if err != nil {
				return err
			}
			class, engagement = version.Definition.Classification.Effective(), version.Definition.EngagementID
		case "result":
			row, err := q.GetTaskResult(ctx, source.ID)
			if err != nil {
				return taskReadError(err)
			}
			class, engagement, err = taskOutputScope(ctx, q, row.AttemptID)
			if err != nil {
				return err
			}
		case "interface_contract":
			row, err := q.GetTaskMessage(ctx, source.ID)
			if err != nil {
				return taskReadError(err)
			}
			class, engagement, err = taskOutputScope(ctx, q, row.AttemptID)
			if err != nil {
				return err
			}
		case "agent_type", "skill", "selection":
		default:
			return ports.ErrTaskInvalid
		}
		if source.Classification.Effective() != class || source.EngagementID != engagement || !domain.CanEmbedContext(policy.MaxContextClass, revision.Definition.EngagementID, class, engagement) {
			return fmt.Errorf("%w: source exceeds pinned clearance or misstates classification", ports.ErrTaskForbidden)
		}
		if !aggregate.Allows(class) {
			aggregate = class
		}
	}
	if snapshot.Classification.Effective() != aggregate {
		return fmt.Errorf("%w: context misstates sealed classification", ports.ErrTaskForbidden)
	}
	return nil
}
