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

var _ ports.TaskResultStore = (*Store)(nil)

func resultGeneration(owner domain.SessionControllerOwner) string {
	if owner.Mode == domain.SessionModeChat {
		return owner.ControllerGeneration
	}
	return owner.RuntimeLaunchID
}

func taskResultFromRow(row gen.AdaptiveTaskResult) (domain.TaskResult, error) {
	result := domain.TaskResult{ID: row.ID, TaskID: row.TaskID, AttemptID: row.AttemptID, Number: row.Number, SessionID: domain.SessionID(row.SessionID), NativeGeneration: row.NativeGeneration, TaskRevision: row.TaskRevision, CriteriaVersion: row.CriteriaVersion, ConfigurationHash: row.ConfigurationHash, ConfigurationSequence: row.ConfigurationSequence, ContextHash: row.ContextHash, ContentHash: row.ContentHash, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Definition), &result.Definition); err != nil {
		return result, err
	}
	if err := result.Definition.Validate(); err != nil {
		return result, err
	}
	_, hash, err := domain.TaskContent(result.Definition)
	if err != nil {
		return result, err
	}
	if hash != result.ContentHash {
		return result, fmt.Errorf("worker result content hash mismatch")
	}
	return result, nil
}

// SubmitTaskResult serializes claims with lease, native and configuration facts.
// Expiry or cancellation alone does not discard output from the still-owned
// controller. Termination, release, replacement or unresolved native work fence
// new claims. An exact historical retry only acknowledges existing content.
func (s *Store) SubmitTaskResult(ctx context.Context, input domain.TaskResultSubmission) (domain.TaskResult, bool, error) {
	var result domain.TaskResult
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
	err = s.inTx(ctx, "submit structured worker result", func(q *gen.Queries) error {
		previous, err := q.GetTaskResultByKey(ctx, gen.GetTaskResultByKeyParams{AttemptID: input.AttemptID, IdempotencyKey: input.IdempotencyKey})
		if err == nil {
			var owner domain.SessionControllerOwner
			if err := json.Unmarshal([]byte(previous.SourceOwner), &owner); err != nil {
				return err
			}
			if previous.SessionID != string(input.SessionID) || previous.NativeGeneration != generation || previous.Number != input.ExpectedVersion+1 || previous.ContentHash != hash || owner.Mode != input.SourceOwner.Mode || owner.Harness != input.SourceOwner.Harness {
				return ports.ErrTaskConflict
			}
			result, err = taskResultFromRow(previous)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		facts, err := readTaskWorkerFacts(ctx, q, input.AttemptID, input.SessionID, input.SourceOwner, input.ExpectedActivation)
		if err != nil {
			return err
		}
		attempt, frozen, configuration, activation := facts.attempt, facts.context, facts.configuration, facts.activation
		latest, err := q.LatestTaskResult(ctx, input.AttemptID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if latest.Number != input.ExpectedVersion {
			return ports.ErrTaskConflict
		}
		owner, err := json.Marshal(input.SourceOwner)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		result = domain.TaskResult{ID: input.ID, TaskID: attempt.TaskID, AttemptID: input.AttemptID, Number: latest.Number + 1, SessionID: input.SessionID, NativeGeneration: generation, TaskRevision: attempt.TaskRevision, CriteriaVersion: attempt.CriteriaVersion, ConfigurationHash: configuration.ContentHash, ConfigurationSequence: activation, ContextHash: frozen.ContentHash, Definition: input.Definition, ContentHash: hash, CreatedAt: now}
		if err := q.InsertTaskResult(ctx, gen.InsertTaskResultParams{ID: result.ID, AttemptID: result.AttemptID, TaskID: result.TaskID, Number: result.Number, SessionID: string(result.SessionID), NativeGeneration: generation, SourceOwner: string(owner), TaskRevision: result.TaskRevision, CriteriaVersion: result.CriteriaVersion, ConfigurationHash: result.ConfigurationHash, ConfigurationSequence: result.ConfigurationSequence, ContextHash: result.ContextHash, IdempotencyKey: input.IdempotencyKey, Definition: string(definition), ContentHash: hash, CreatedAt: now}); err != nil {
			return err
		}
		mutation := domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "WORKER", ID: string(input.SessionID), SessionID: input.SessionID}, Reason: fmt.Sprintf("Worker submitted %s result version %d; claims await independent evaluation", input.Definition.ClaimedOutcome, result.Number)}
		if err := insertTaskAudit(ctx, q, result.TaskID, result.TaskRevision, "result_submitted", mutation, now); err != nil {
			return err
		}
		created = true
		return nil
	})
	return result, created && err == nil, err
}

// GetTaskResult returns immutable claims, including after worker termination.
func (s *Store) GetTaskResult(ctx context.Context, id string) (domain.TaskResult, error) {
	row, err := s.qr.GetTaskResult(ctx, id)
	if err != nil {
		return domain.TaskResult{}, taskReadError(err)
	}
	return taskResultFromRow(row)
}

// LatestTaskResult distinguishes no submission from failed history reads.
func (s *Store) LatestTaskResult(ctx context.Context, attemptID string) (domain.TaskResult, bool, error) {
	row, err := s.qr.LatestTaskResult(ctx, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskResult{}, false, nil
	}
	if err != nil {
		return domain.TaskResult{}, false, err
	}
	result, err := taskResultFromRow(row)
	return result, err == nil, err
}

// ListTaskResults pages revisions of one attempt without conflating evaluations.
func (s *Store) ListTaskResults(ctx context.Context, attemptID string, after int64, limit int) ([]domain.TaskResult, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListTaskResults(ctx, gen.ListTaskResultsParams{AttemptID: attemptID, Number: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	results := make([]domain.TaskResult, 0, len(rows))
	for _, row := range rows {
		result, err := taskResultFromRow(row)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}
