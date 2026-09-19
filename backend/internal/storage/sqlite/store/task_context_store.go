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

var _ ports.TaskContextStore = (*Store)(nil)

func taskContextFromRow(row gen.AdaptiveTaskContext) (domain.TaskContextSnapshot, error) {
	var snapshot domain.TaskContextSnapshot
	if err := json.Unmarshal([]byte(row.Snapshot), &snapshot); err != nil {
		return snapshot, err
	}
	if snapshot.AttemptID != row.AttemptID || string(snapshot.SessionID) != row.SessionID || snapshot.ExecutionOperationID != row.ExecutionOperationID || snapshot.ConfigurationHash != row.ConfigurationHash || snapshot.ContentHash != row.ContentHash {
		return snapshot, fmt.Errorf("task context identity is inconsistent")
	}
	return snapshot, snapshot.Validate()
}

// GetTaskContext returns immutable historical content even after invalidation of
// its sources. Not-found is distinct from operational or integrity failure.
func (s *Store) GetTaskContext(ctx context.Context, attemptID string) (domain.TaskContextSnapshot, bool, error) {
	row, err := s.qr.GetTaskContext(ctx, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskContextSnapshot{}, false, nil
	}
	if err != nil {
		return domain.TaskContextSnapshot{}, false, err
	}
	snapshot, err := taskContextFromRow(row)
	return snapshot, err == nil, err
}

// GetTaskContextBySession supports exact replay during native restoration.
func (s *Store) GetTaskContextBySession(ctx context.Context, id domain.SessionID) (domain.TaskContextSnapshot, bool, error) {
	row, err := s.qr.GetTaskContextBySession(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskContextSnapshot{}, false, nil
	}
	if err != nil {
		return domain.TaskContextSnapshot{}, false, err
	}
	snapshot, err := taskContextFromRow(row)
	return snapshot, err == nil, err
}

// SaveTaskContext freezes exact inputs before launch; late cancellation, source
// invalidation or changed ownership fail the transaction without a partial seal.
func (s *Store) SaveTaskContext(ctx context.Context, token domain.TaskLeaseToken, snapshot domain.TaskContextSnapshot) error {
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
	}
	if token.AttemptID != snapshot.AttemptID {
		return ports.ErrTaskLeaseFenced
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "seal task context", func(q *gen.Queries) error {
		lease, err := q.GetTaskLease(ctx, token.AttemptID)
		if err != nil {
			return taskReadError(err)
		}
		if taskLeaseFenced(lease, token) {
			return ports.ErrTaskLeaseFenced
		}
		if row, err := q.GetTaskContext(ctx, snapshot.AttemptID); err == nil {
			if row.ContentHash != snapshot.ContentHash {
				return ports.ErrTaskConflict
			}
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !snapshot.CreatedAt.Before(lease.ExpiresAt) || snapshot.CreatedAt.Before(lease.HeartbeatAt) {
			return ports.ErrTaskLeaseFenced
		}
		if err := requireTaskRunIntent(ctx, q, lease.TaskID); err != nil {
			return err
		}
		op, err := q.PendingTaskAttemptExecution(ctx, token.AttemptID)
		if err != nil {
			return taskReadError(err)
		}
		if op.ID != snapshot.ExecutionOperationID || op.Kind != "dispatch" || op.SessionID != string(snapshot.SessionID) || op.Generation != token.Generation || op.HolderID != token.HolderID || snapshot.CreatedAt.Before(op.CreatedAt) {
			return ports.ErrTaskLeaseFenced
		}
		attemptRow, err := q.GetTaskAttempt(ctx, token.AttemptID)
		if err != nil {
			return taskReadError(err)
		}
		attempt, err := taskAttemptFromRow(attemptRow)
		if err != nil {
			return err
		}
		if attempt.TaskID != snapshot.Task.TaskID || attempt.TaskRevision != snapshot.Task.Revision || attempt.CriteriaVersion != snapshot.CriteriaVersion {
			return ports.ErrTaskConflict
		}
		dispatch, err := q.GetTaskWorkerDispatch(ctx, token.AttemptID)
		if err != nil {
			return taskReadError(err)
		}
		if dispatch.SessionID != string(snapshot.SessionID) || dispatch.ConfigurationHash != snapshot.ConfigurationHash {
			return ports.ErrTaskConflict
		}
		configRow, err := q.GetWorkerConfiguration(ctx, string(snapshot.SessionID))
		if err != nil {
			return err
		}
		config, err := workerConfigurationFromRow(configRow)
		if err != nil {
			return err
		}
		if config.ContentHash != snapshot.ConfigurationHash {
			return ports.ErrTaskConflict
		}
		if err := validateContextClassifications(ctx, q, snapshot); err != nil {
			return err
		}
		if err := validateTaskContextSources(ctx, q, attempt, config, snapshot); err != nil {
			return err
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if err := q.InsertTaskContext(ctx, gen.InsertTaskContextParams{AttemptID: snapshot.AttemptID, SessionID: string(snapshot.SessionID), ExecutionOperationID: snapshot.ExecutionOperationID, ConfigurationHash: snapshot.ConfigurationHash, Snapshot: string(encoded), ContentHash: snapshot.ContentHash, CreatedAt: snapshot.CreatedAt}); err != nil {
			return err
		}
		if err := insertInitialTaskDelegation(ctx, q, snapshot); err != nil {
			return err
		}
		return insertTaskAudit(ctx, q, attempt.TaskID, attempt.TaskRevision, "context_sealed", domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "SYSTEM", ID: token.HolderID}, Reason: "Sealed bounded worker context " + snapshot.ContentHash}, snapshot.CreatedAt)
	})
}

func contextContentMatches(source domain.ContextSource, content any) bool {
	encoded, _, err := domain.TaskContent(content)
	return err == nil && source.Disposition == "inline" && source.Content == string(encoded)
}

func validateTaskContextSources(ctx context.Context, q *gen.Queries, attempt domain.TaskAttempt, config domain.WorkerConfiguration, snapshot domain.TaskContextSnapshot) error {
	task, err := q.GetAdaptiveTask(ctx, attempt.TaskID)
	if err != nil {
		return err
	}
	revisionRow, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: attempt.TaskID, Number: attempt.TaskRevision})
	if err != nil {
		return err
	}
	revision, err := taskRevisionFromRow(revisionRow)
	if err != nil {
		return err
	}
	if snapshot.Task.ContentHash != revision.ContentHash {
		return ports.ErrTaskConflict
	}
	dependencies := map[string]domain.TaskRevisionRef{}
	for _, ref := range attempt.Dependencies {
		dependencies[ref.TaskID] = ref
	}
	files := map[string]bool{}
	for _, path := range revision.Definition.ContextFiles {
		files[path] = true
	}
	refs := map[string]domain.WorkerDefinitionRef{"agent_type:" + config.AgentType.ID: config.AgentType}
	for _, skill := range config.Skills {
		refs["skill:"+skill.Reference.ID] = skill.Reference
	}
	for _, source := range snapshot.Sources {
		if source.Disposition == "omitted" {
			continue
		}
		switch source.Kind {
		case "task":
			if source.ID != attempt.TaskID || source.Version != attempt.TaskRevision || source.SourceHash != revision.ContentHash || !contextContentMatches(source, revision.Definition) {
				return ports.ErrTaskConflict
			}
		case "criteria":
			row, err := q.GetAdaptiveTaskCriteria(ctx, gen.GetAdaptiveTaskCriteriaParams{TaskID: attempt.TaskID, Number: attempt.CriteriaVersion})
			if err != nil {
				return err
			}
			var definition domain.AcceptanceCriteria
			if err := json.Unmarshal([]byte(row.Definition), &definition); err != nil {
				return err
			}
			if source.ID != attempt.TaskID || source.Version != attempt.CriteriaVersion || source.SourceHash != row.ContentHash || !contextContentMatches(source, definition) {
				return ports.ErrTaskConflict
			}
		case "parent", "dependency":
			if source.Kind == "parent" && source.ID != revision.Definition.ParentID {
				return ports.ErrTaskConflict
			}
			if source.Kind == "dependency" {
				ref, ok := dependencies[source.ID]
				if !ok || ref.Revision != source.Version || ref.ContentHash != source.SourceHash {
					return ports.ErrTaskConflict
				}
			}
			row, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: source.ID, Number: source.Version})
			if err != nil {
				return taskReadError(err)
			}
			version, err := taskRevisionFromRow(row)
			if err != nil {
				return err
			}
			if source.SourceHash != version.ContentHash || !contextContentMatches(source, version.Definition) {
				return ports.ErrTaskConflict
			}
		case "knowledge":
			identity, err := q.GetProjectKnowledge(ctx, source.ID)
			if err != nil {
				return taskReadError(err)
			}
			if identity.ProjectID != task.ProjectID || identity.Version != source.Version {
				return ports.ErrTaskConflict
			}
			row, err := q.GetKnowledgeVersion(ctx, gen.GetKnowledgeVersionParams{KnowledgeID: source.ID, Number: source.Version})
			if err != nil {
				return taskReadError(err)
			}
			version, err := knowledgeVersionFromRow(row)
			if err != nil {
				return err
			}
			if version.Definition.Status != "accepted" || source.SourceHash != version.ContentHash || !contextContentMatches(source, version.Definition) {
				return ports.ErrTaskConflict
			}
		case "agent_type", "skill":
			key := source.Kind + ":" + source.ID
			ref, ok := refs[key]
			if !ok || ref.Version != source.Version || ref.ContentHash != source.SourceHash || source.Disposition != "reference" {
				return ports.ErrTaskConflict
			}
			delete(refs, key)
		case "result":
			row, err := q.GetTaskResult(ctx, source.ID)
			if err != nil {
				return taskReadError(err)
			}
			result, err := taskResultFromRow(row)
			if err != nil {
				return err
			}
			if result.TaskID == attempt.TaskID {
				prior, err := q.GetTaskAttempt(ctx, result.AttemptID)
				if err != nil {
					return taskReadError(err)
				}
				if prior.Number >= attempt.Number {
					return ports.ErrTaskConflict
				}
			} else if ref, ok := dependencies[result.TaskID]; !ok || result.TaskRevision != ref.Revision {
				return ports.ErrTaskConflict
			}
			if source.Version != result.Number || source.SourceHash != result.ContentHash || !contextContentMatches(source, result.ContextFacts()) {
				return ports.ErrTaskConflict
			}
		case "interface_contract":
			row, err := q.GetTaskMessage(ctx, source.ID)
			if err != nil {
				return taskReadError(err)
			}
			message, err := taskMessageFromRow(row)
			if err != nil {
				return err
			}
			if string(message.ProjectID) != task.ProjectID || message.Definition.TargetTaskID != attempt.TaskID || message.Definition.Kind != "interface_contract" || source.Version != 1 || source.SourceHash != message.ContentHash || !contextContentMatches(source, message) {
				return ports.ErrTaskConflict
			}
		case "file":
			if !files[source.ID] || source.Disposition != "inline" || source.SourceHash != source.ContentHash {
				return ports.ErrTaskConflict
			}
		default:
			return ports.ErrTaskInvalid
		}
	}
	if len(refs) != 0 {
		return fmt.Errorf("%w: context must retain all configured Type and Skill references", ports.ErrTaskInvalid)
	}
	return nil
}
