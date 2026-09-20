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

var _ ports.TaskDelegationStore = (*Store)(nil)

func taskDelegationFromRow(row gen.AdaptiveTaskDelegation) (domain.TaskDelegation, error) {
	var d domain.TaskDelegation
	if err := json.Unmarshal([]byte(row.Snapshot), &d); err != nil {
		return d, err
	}
	if d.AttemptID != row.AttemptID || d.Number != row.Number || d.ExecutionOperationID != row.ExecutionOperationID || string(d.SessionID) != row.SessionID || d.ConfigurationHash != row.ConfigurationHash || d.ContextHash != row.ContextHash || d.ContentHash != row.ContentHash || !d.CreatedAt.Equal(row.CreatedAt) {
		return d, fmt.Errorf("delegation identity is inconsistent")
	}
	return d, d.Validate()
}

// GetTaskDelegation reads a retained version without consulting current Types.
func (s *Store) GetTaskDelegation(ctx context.Context, attemptID string, number int64) (domain.TaskDelegation, error) {
	if attemptID == "" || len(attemptID) > 200 || number < 1 || number > 1000 {
		return domain.TaskDelegation{}, ports.ErrTaskInvalid
	}
	row, err := s.qr.GetTaskDelegation(ctx, gen.GetTaskDelegationParams{AttemptID: attemptID, Number: number})
	if err != nil {
		return domain.TaskDelegation{}, taskReadError(err)
	}
	return taskDelegationFromRow(row)
}

// ListTaskDelegations bounds inspection independently of retained history size.
func (s *Store) ListTaskDelegations(ctx context.Context, attemptID string, after int64, limit int) ([]domain.TaskDelegation, error) {
	if attemptID == "" || len(attemptID) > 200 || after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListTaskDelegations(ctx, gen.ListTaskDelegationsParams{AttemptID: attemptID, Number: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]domain.TaskDelegation, 0, len(rows))
	for _, row := range rows {
		d, err := taskDelegationFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, nil
}

// insertInitialTaskDelegation runs in the manifest seal transaction. Legacy
// snapshots do not invent the full instructions they never retained.
func insertInitialTaskDelegation(ctx context.Context, q *gen.Queries, snapshot domain.TaskContextSnapshot) error {
	if snapshot.SchemaVersion < 2 {
		return nil
	}
	d := domain.TaskDelegation{SchemaVersion: 1, AttemptID: snapshot.AttemptID, Number: 1, SessionID: snapshot.SessionID,
		ExecutionOperationID: snapshot.ExecutionOperationID, ConfigurationHash: snapshot.ConfigurationHash, ContextHash: snapshot.ContentHash,
		MaxContextClass: snapshot.MaxContextClass, Classification: snapshot.Classification, EngagementID: snapshot.EngagementID,
		SystemPrompt: snapshot.SystemPrompt, Prompt: snapshot.Prompt, CreatedAt: snapshot.CreatedAt}
	d.ContentHash = d.Hash()
	if err := d.Validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return q.InsertTaskDelegation(ctx, gen.InsertTaskDelegationParams{AttemptID: d.AttemptID, Number: d.Number, ExecutionOperationID: d.ExecutionOperationID,
		SessionID: string(d.SessionID), ConfigurationHash: d.ConfigurationHash, ContextHash: d.ContextHash, Snapshot: string(encoded), ContentHash: d.ContentHash, CreatedAt: d.CreatedAt})
}

// AppendTaskDelegation journals the output instructions a replacement native
// generation restored. The retained snapshot's exact bytes are copied into a
// new immutable version bound to the restore execution operation: sealed
// context is never rewritten and a live configuration can never substitute for
// it. Replaying the same operation returns the existing receipt; legacy
// schema-v1 contexts predate journalled instructions and stay unreadable
// instead of inventing text.
func (s *Store) AppendTaskDelegation(ctx context.Context, op domain.TaskExecutionOperation, snapshot domain.TaskContextSnapshot, now time.Time) (domain.TaskDelegation, bool, error) {
	if op.Kind != "restore" || op.SessionID == "" || op.ID == "" || op.Lease.AttemptID == "" {
		return domain.TaskDelegation{}, false, fmt.Errorf("%w: only a restore execution operation can journal replacement instructions", ports.ErrTaskInvalid)
	}
	if snapshot.SchemaVersion < 2 {
		return domain.TaskDelegation{}, false, nil
	}
	if snapshot.AttemptID != op.Lease.AttemptID || snapshot.SessionID != op.SessionID {
		return domain.TaskDelegation{}, false, fmt.Errorf("%w: retained context does not belong to this execution", ports.ErrTaskForbidden)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.TaskDelegation{}, false, err
	}
	defer s.writeMu.Unlock()
	var appended domain.TaskDelegation
	created := false
	err := s.inTx(ctx, "append task delegation", func(q *gen.Queries) error {
		row, err := q.GetTaskDelegationByOperation(ctx, op.ID)
		switch {
		case err == nil:
			appended, err = taskDelegationFromRow(row)
			return err
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		highest, err := q.MaxTaskDelegationNumber(ctx, snapshot.AttemptID)
		if err != nil {
			return err
		}
		if highest >= 1000 {
			return fmt.Errorf("%w: delegation history is bounded at 1000 versions", ports.ErrTaskInvalid)
		}
		d := domain.TaskDelegation{SchemaVersion: 1, AttemptID: snapshot.AttemptID, Number: highest + 1, SessionID: snapshot.SessionID,
			ExecutionOperationID: op.ID, ConfigurationHash: snapshot.ConfigurationHash, ContextHash: snapshot.ContentHash,
			MaxContextClass: snapshot.MaxContextClass, Classification: snapshot.Classification, EngagementID: snapshot.EngagementID,
			SystemPrompt: snapshot.SystemPrompt, Prompt: snapshot.Prompt, CreatedAt: now}
		d.ContentHash = d.Hash()
		if err := d.Validate(); err != nil {
			return err
		}
		encoded, err := json.Marshal(d)
		if err != nil {
			return err
		}
		if err := q.InsertTaskDelegation(ctx, gen.InsertTaskDelegationParams{AttemptID: d.AttemptID, Number: d.Number, ExecutionOperationID: d.ExecutionOperationID,
			SessionID: string(d.SessionID), ConfigurationHash: d.ConfigurationHash, ContextHash: d.ContextHash, Snapshot: string(encoded), ContentHash: d.ContentHash, CreatedAt: d.CreatedAt}); err != nil {
			return err
		}
		appended, created = d, true
		return nil
	})
	return appended, created, err
}
