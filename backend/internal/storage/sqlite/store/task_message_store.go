package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.TaskMessageStore = (*Store)(nil)

func taskMessageFromRow(row gen.AdaptiveTaskMessage) (domain.TaskMessage, error) {
	m := domain.TaskMessage{ID: row.ID, Sequence: row.Sequence, ProjectID: domain.ProjectID(row.ProjectID), TaskID: row.TaskID, AttemptID: row.AttemptID, SessionID: domain.SessionID(row.SessionID), NativeGeneration: row.NativeGeneration, TaskRevision: row.TaskRevision, CriteriaVersion: row.CriteriaVersion, ConfigurationHash: row.ConfigurationHash, ConfigurationSequence: row.ConfigurationSequence, ContextHash: row.ContextHash, ContentHash: row.ContentHash, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Definition), &m.Definition); err != nil {
		return m, err
	}
	if err := m.Definition.Validate(); err != nil {
		return m, err
	}
	_, hash, err := domain.TaskContent(m.Definition)
	if err != nil {
		return m, err
	}
	if hash != m.ContentHash || m.Definition.TargetTaskID != row.TargetTaskID || m.Definition.CorrelationID != row.CorrelationID || m.Definition.ReplyToID != row.ReplyToID.String || m.Definition.ResultID != row.ResultID.String {
		return m, fmt.Errorf("task message content or references are inconsistent")
	}
	return m, nil
}

func taskMessagesFromRows(rows []gen.AdaptiveTaskMessage) ([]domain.TaskMessage, error) {
	items := make([]domain.TaskMessage, 0, len(rows))
	for _, row := range rows {
		item, err := taskMessageFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// SubmitTaskMessage persists coordination before any transport effect. Exact
// retries acknowledge the retained message; they never enqueue a second copy.
func (s *Store) SubmitTaskMessage(ctx context.Context, input domain.TaskMessageSubmission) (domain.TaskMessage, bool, error) {
	var result domain.TaskMessage
	if err := input.Validate(); err != nil {
		return result, false, fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
	}
	generation := resultGeneration(input.SourceOwner)
	if generation == "" {
		return result, false, ports.ErrTaskLeaseFenced
	}
	definition, hash, err := domain.TaskContent(input.Definition)
	if err != nil {
		return result, false, err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return result, false, err
	}
	defer s.writeMu.Unlock()
	created := false
	err = s.inTx(ctx, "submit typed task message", func(q *gen.Queries) error {
		previous, err := q.GetTaskMessageByKey(ctx, gen.GetTaskMessageByKeyParams{AttemptID: input.AttemptID, IdempotencyKey: input.IdempotencyKey})
		if err == nil {
			var owner domain.SessionControllerOwner
			if err := json.Unmarshal([]byte(previous.SourceOwner), &owner); err != nil {
				return err
			}
			if previous.SessionID != string(input.SessionID) || previous.NativeGeneration != generation || previous.ContentHash != hash || owner.Mode != input.SourceOwner.Mode || owner.Harness != input.SourceOwner.Harness {
				return ports.ErrTaskConflict
			}
			result, err = taskMessageFromRow(previous)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		facts, err := readTaskWorkerFacts(ctx, q, input.AttemptID, input.SessionID, input.SourceOwner, input.ExpectedActivation)
		if err != nil {
			return err
		}
		target, err := q.GetAdaptiveTask(ctx, input.Definition.TargetTaskID)
		if err != nil {
			return taskReadError(err)
		}
		if target.ProjectID != facts.task.ProjectID || target.ID == facts.task.ID {
			return fmt.Errorf("%w: target must be another task in the same project", ports.ErrTaskInvalid)
		}
		if input.Definition.ResultID != "" {
			claim, err := q.GetTaskResult(ctx, input.Definition.ResultID)
			if err != nil {
				return taskReadError(err)
			}
			if claim.AttemptID != input.AttemptID || claim.SessionID != string(input.SessionID) {
				return fmt.Errorf("%w: result must belong to the sending attempt", ports.ErrTaskInvalid)
			}
		}
		if input.Definition.ReplyToID != "" {
			previous, err := q.GetTaskMessage(ctx, input.Definition.ReplyToID)
			if err != nil {
				return taskReadError(err)
			}
			question, err := taskMessageFromRow(previous)
			if err != nil {
				return err
			}
			if previous.ProjectID != facts.task.ProjectID || previous.TargetTaskID != facts.task.ID || previous.TaskID != target.ID || previous.CorrelationID != input.Definition.CorrelationID || (input.Definition.Kind == "answer" && question.Definition.Kind != "question") {
				return fmt.Errorf("%w: reply must retain thread and sender/recipient scope", ports.ErrTaskInvalid)
			}
		}
		count, err := q.CountTaskMessages(ctx, input.AttemptID)
		if err != nil {
			return err
		}
		projectCount, err := q.CountProjectTaskMessages(ctx, facts.task.ProjectID)
		if err != nil {
			return err
		}
		if count >= 256 || projectCount >= 10000 {
			return fmt.Errorf("%w: message history bound reached", ports.ErrTaskConflict)
		}
		owner, err := json.Marshal(input.SourceOwner)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		row, err := q.InsertTaskMessage(ctx, gen.InsertTaskMessageParams{ID: input.ID, ProjectID: facts.task.ProjectID, TaskID: facts.task.ID, AttemptID: input.AttemptID, SessionID: string(input.SessionID), NativeGeneration: generation, SourceOwner: string(owner), TaskRevision: facts.attempt.TaskRevision, CriteriaVersion: facts.attempt.CriteriaVersion, ConfigurationHash: facts.configuration.ContentHash, ConfigurationSequence: facts.activation, ContextHash: facts.context.ContentHash, TargetTaskID: target.ID, CorrelationID: input.Definition.CorrelationID, ReplyToID: sql.NullString{String: input.Definition.ReplyToID, Valid: input.Definition.ReplyToID != ""}, ResultID: sql.NullString{String: input.Definition.ResultID, Valid: input.Definition.ResultID != ""}, IdempotencyKey: input.IdempotencyKey, Definition: string(definition), ContentHash: hash, CreatedAt: now})
		if err != nil {
			return err
		}
		result, err = taskMessageFromRow(row)
		if err != nil {
			return err
		}
		mutation := domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "WORKER", ID: string(input.SessionID), SessionID: input.SessionID}, Reason: "Persisted typed " + input.Definition.Kind + " message " + input.ID}
		if err := insertTaskAudit(ctx, q, facts.task.ID, facts.attempt.TaskRevision, "message_submitted", mutation, now); err != nil {
			return err
		}
		created = true
		return nil
	})
	return result, created && err == nil, err
}

// GetTaskMessage reads retained content and validates its immutable hash.
func (s *Store) GetTaskMessage(ctx context.Context, id string) (domain.TaskMessage, error) {
	row, err := s.qr.GetTaskMessage(ctx, id)
	if err != nil {
		return domain.TaskMessage{}, taskReadError(err)
	}
	return taskMessageFromRow(row)
}

// ListTaskMessages is the shared sender/recipient/project observation timeline.
func (s *Store) ListTaskMessages(ctx context.Context, projectID domain.ProjectID, taskID string, after int64, limit int) ([]domain.TaskMessage, error) {
	if projectID == "" || after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListTaskMessages(ctx, gen.ListTaskMessagesParams{ProjectID: string(projectID), TaskID: taskID, AfterSequence: after, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return taskMessagesFromRows(rows)
}

// ListPendingTaskMessages excludes claimed, uncertain and exhausted deliveries.
func (s *Store) ListPendingTaskMessages(ctx context.Context, after int64, limit int) ([]domain.TaskMessage, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListPendingTaskMessages(ctx, gen.ListPendingTaskMessagesParams{Sequence: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return taskMessagesFromRows(rows)
}
