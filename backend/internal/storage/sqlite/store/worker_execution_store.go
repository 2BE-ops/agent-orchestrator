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

var _ ports.WorkerExecutionStore = (*Store)(nil)

// PrepareWorkerExecution records an immutable change against its current owner
// and activation sequence. Retried source actions cannot change their payload.
func (s *Store) PrepareWorkerExecution(ctx context.Context, owner domain.SessionControllerOwner, execution domain.WorkerExecution) (domain.WorkerExecution, error) {
	if err := execution.Validate(); err != nil {
		return execution, err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return execution, err
	}
	defer s.writeMu.Unlock()
	var prepared domain.WorkerExecution
	err := s.inTx(ctx, "prepare worker execution", func(q *gen.Queries) error {
		row, err := q.GetSession(ctx, execution.SessionID)
		if err != nil {
			return err
		}
		if rowToRecord(row).ControllerOwner() != owner {
			return ports.ErrRegistryConflict
		}
		root, err := q.GetWorkerConfiguration(ctx, string(execution.SessionID))
		if err != nil {
			return err
		}
		original, err := workerConfigurationFromRow(root)
		if err != nil {
			return err
		}
		if original.AgentType != execution.Configuration.AgentType {
			return fmt.Errorf("worker execution cannot rewrite original Agent Type provenance")
		}
		existing, err := q.GetWorkerExecutionBySource(ctx, gen.GetWorkerExecutionBySourceParams{SessionID: string(execution.SessionID), SourceKind: execution.SourceKind, SourceID: execution.SourceID})
		if err == nil {
			prepared, err = workerExecutionFromRow(existing)
			if err != nil {
				return err
			}
			if prepared.PreviousActivation != execution.PreviousActivation || prepared.Configuration.ContentHash != execution.Configuration.ContentHash || prepared.Actor != execution.Actor || prepared.Reason != execution.Reason {
				return ports.ErrRegistryConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		active, err := currentWorkerActivation(ctx, q, execution.SessionID)
		if err != nil {
			return err
		}
		if active.Seq != execution.PreviousActivation {
			return ports.ErrRegistryConflict
		}
		if err := insertWorkerExecution(ctx, q, execution); err != nil {
			return err
		}
		prepared = execution
		return nil
	})
	return prepared, err
}

func insertWorkerExecution(ctx context.Context, q *gen.Queries, e domain.WorkerExecution) error {
	if err := e.Validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(e.Configuration)
	if err != nil {
		return err
	}
	return q.InsertWorkerExecution(ctx, gen.InsertWorkerExecutionParams{ID: e.ID, SessionID: string(e.SessionID), SourceKind: e.SourceKind, SourceID: e.SourceID, PreviousActivation: e.PreviousActivation, Configuration: string(encoded), ContentHash: e.Configuration.ContentHash, Origin: string(e.Actor.Origin), ActorID: e.Actor.ID, Reason: e.Reason, CreatedAt: e.CreatedAt})
}

func workerExecutionFromRow(row gen.AdaptiveWorkerExecution) (domain.WorkerExecution, error) {
	e := domain.WorkerExecution{ID: row.ID, SessionID: domain.SessionID(row.SessionID), SourceKind: row.SourceKind, SourceID: row.SourceID, PreviousActivation: row.PreviousActivation, Actor: domain.RegistryActor{Origin: domain.RegistryOrigin(row.Origin), ID: row.ActorID}, Reason: row.Reason, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Configuration), &e.Configuration); err != nil {
		return e, err
	}
	if e.Configuration.ContentHash != row.ContentHash {
		return e, fmt.Errorf("worker execution content hash is inconsistent")
	}
	return e, e.Validate()
}

func currentWorkerActivation(ctx context.Context, q *gen.Queries, id domain.SessionID) (gen.AdaptiveWorkerExecutionActivation, error) {
	row, err := q.GetCurrentWorkerActivation(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return gen.AdaptiveWorkerExecutionActivation{}, nil
	}
	return row, err
}

func effectiveWorkerConfiguration(ctx context.Context, q *gen.Queries, id domain.SessionID) (domain.WorkerConfiguration, int64, bool, error) {
	active, err := currentWorkerActivation(ctx, q, id)
	if err != nil {
		return domain.WorkerConfiguration{}, 0, false, err
	}
	if active.ExecutionID.Valid {
		row, err := q.GetWorkerExecution(ctx, gen.GetWorkerExecutionParams{ID: active.ExecutionID.String, SessionID: string(id)})
		if err != nil {
			return domain.WorkerConfiguration{}, 0, false, err
		}
		e, err := workerExecutionFromRow(row)
		return e.Configuration, active.Seq, err == nil, err
	}
	row, err := q.GetWorkerConfiguration(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkerConfiguration{}, 0, false, nil
	}
	if err != nil {
		return domain.WorkerConfiguration{}, 0, false, err
	}
	snapshot, err := workerConfigurationFromRow(row)
	return snapshot, active.Seq, err == nil, err
}

// GetEffectiveWorkerConfiguration returns the last activated configuration or
// original launch, plus the sequence callers must fence when preparing changes.
func (s *Store) GetEffectiveWorkerConfiguration(ctx context.Context, id domain.SessionID) (domain.WorkerConfiguration, int64, bool, error) {
	return effectiveWorkerConfiguration(ctx, s.qr, id)
}

// GetWorkerExecution returns sealed segment content scoped to its owning session.
func (s *Store) GetWorkerExecution(ctx context.Context, id domain.SessionID, executionID string) (domain.WorkerExecution, error) {
	row, err := s.qr.GetWorkerExecution(ctx, gen.GetWorkerExecutionParams{ID: executionID, SessionID: string(id)})
	if err != nil {
		return domain.WorkerExecution{}, err
	}
	return workerExecutionFromRow(row)
}

// ListWorkerExecutions pages immutable activation and rollback events in order.
func (s *Store) ListWorkerExecutions(ctx context.Context, id domain.SessionID, after int64, limit int) ([]domain.WorkerExecutionActivation, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("invalid worker history page")
	}
	rows, err := s.qr.ListWorkerActivations(ctx, gen.ListWorkerActivationsParams{SessionID: string(id), Seq: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.WorkerExecutionActivation, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.WorkerExecutionActivation{Sequence: row.Seq, SessionID: id, ExecutionID: row.ExecutionID.String, OperationID: row.OperationID, Action: row.Action, CreatedAt: row.CreatedAt})
	}
	return result, nil
}

// activateWorkerExecution is called inside the ownership/settings transaction.
// A failure therefore rolls back both the configuration and controller change.
func activateWorkerExecution(ctx context.Context, q *gen.Queries, e domain.WorkerExecution, rollback bool, now time.Time) error {
	current, err := currentWorkerActivation(ctx, q, e.SessionID)
	if err != nil {
		return err
	}
	if current.OperationID == e.ID && ((rollback && current.Action == "rolled_back") || (!rollback && current.Action == "applied")) {
		return nil
	}
	target := sql.NullString{String: e.ID, Valid: true}
	action := "applied"
	if rollback {
		if current.ExecutionID.String != e.ID {
			return ports.ErrRegistryConflict
		}
		action = "rolled_back"
		target = sql.NullString{}
		if e.PreviousActivation != 0 {
			previous, err := q.GetWorkerActivation(ctx, gen.GetWorkerActivationParams{SessionID: string(e.SessionID), Seq: e.PreviousActivation})
			if err != nil {
				return err
			}
			target = previous.ExecutionID
		}
	} else if current.Seq != e.PreviousActivation {
		return ports.ErrRegistryConflict
	}
	configuration := e.Configuration
	if rollback {
		if target.Valid {
			row, err := q.GetWorkerExecution(ctx, gen.GetWorkerExecutionParams{ID: target.String, SessionID: string(e.SessionID)})
			if err != nil {
				return err
			}
			previous, err := workerExecutionFromRow(row)
			if err != nil {
				return err
			}
			configuration = previous.Configuration
		} else {
			row, err := q.GetWorkerConfiguration(ctx, string(e.SessionID))
			if err != nil {
				return err
			}
			configuration, err = workerConfigurationFromRow(row)
			if err != nil {
				return err
			}
		}
	}
	owner, err := q.GetSession(ctx, e.SessionID)
	if err != nil {
		return err
	}
	if owner.Harness != configuration.Effective.Harness || domain.NormalizeSessionMode(owner.SessionMode) != configuration.Effective.SessionMode {
		return ports.ErrRegistryConflict
	}
	return q.InsertWorkerActivation(ctx, gen.InsertWorkerActivationParams{SessionID: string(e.SessionID), ExecutionID: target, OperationID: e.ID, Action: action, CreatedAt: now})
}

func switchWorkerInterfaceExecution(ctx context.Context, q *gen.Queries, id domain.SessionID, target domain.SessionMode, rollback bool, now time.Time) error {
	_, _, configured, err := effectiveWorkerConfiguration(ctx, q, id)
	if err != nil || !configured {
		return err
	}
	transition, err := q.GetActiveSessionInterfaceTransition(ctx, id)
	if err != nil {
		return fmt.Errorf("read worker interface transition: %w", err)
	}
	row, err := q.GetWorkerExecutionBySource(ctx, gen.GetWorkerExecutionBySourceParams{SessionID: string(id), SourceKind: "interface_transition", SourceID: transition.ID})
	if err != nil {
		return fmt.Errorf("read prepared worker interface configuration: %w", err)
	}
	execution, err := workerExecutionFromRow(row)
	if err != nil {
		return err
	}
	if !rollback && execution.Configuration.Effective.SessionMode != target {
		return ports.ErrRegistryConflict
	}
	return activateWorkerExecution(ctx, q, execution, rollback, now)
}
