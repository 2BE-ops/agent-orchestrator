package session

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// WorkerConfiguration reads immutable launch facts independently of current
// registry defaults. Legacy sessions return nil without guessing their history.
func (s *Service) WorkerConfiguration(ctx context.Context, id domain.SessionID) (*domain.WorkerConfiguration, error) {
	_, found, err := s.store.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	store, ok := s.store.(interface {
		GetWorkerConfiguration(context.Context, domain.SessionID) (domain.WorkerConfiguration, bool, error)
	})
	if !ok {
		return nil, apierr.NotImplemented("WORKER_CONFIGURATION_UNAVAILABLE", "Worker configuration history is unavailable")
	}
	snapshot, found, err := store.GetWorkerConfiguration(ctx, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &snapshot, nil
}

// WorkerExecutionSummary keeps history pages bounded without repeating retained
// instructions and Skill resources for every activation.
type WorkerExecutionSummary struct {
	Activation        domain.WorkerExecutionActivation `json:"activation"`
	Actor             domain.RegistryActor             `json:"actor"`
	Reason            string                           `json:"reason"`
	SourceKind        string                           `json:"sourceKind"`
	SourceID          string                           `json:"sourceId"`
	Harness           domain.AgentHarness              `json:"harness"`
	SessionMode       domain.SessionMode               `json:"sessionMode"`
	Config            domain.AgentConfig               `json:"config"`
	ProviderBindingID string                           `json:"providerBindingId,omitempty"`
	ContentHash       string                           `json:"contentHash"`
	PreparedAt        time.Time                        `json:"preparedAt"`
}

// WorkerExecutionPage includes the current configuration and chronological,
// immutable activation summaries through that configuration's sequence.
type WorkerExecutionPage struct {
	Current         *domain.WorkerConfiguration `json:"current"`
	CurrentSequence int64                       `json:"currentSequence"`
	Events          []WorkerExecutionSummary    `json:"events"`
	NextCursor      int64                       `json:"nextCursor,omitempty"`
}

// WorkerExecutions returns bounded history, including rollback destinations.
func (s *Service) WorkerExecutions(ctx context.Context, id domain.SessionID, after int64, limit int) (WorkerExecutionPage, error) {
	page := WorkerExecutionPage{Events: []WorkerExecutionSummary{}}
	if after < 0 || limit < 1 || limit > 100 {
		return page, apierr.Invalid("INVALID_WORKER_HISTORY_PAGE", "Cursor must be non-negative and limit between 1 and 100", nil)
	}
	original, err := s.WorkerConfiguration(ctx, id)
	if err != nil || original == nil {
		return page, err
	}
	store, ok := s.store.(ports.WorkerExecutionStore)
	if !ok {
		return page, apierr.NotImplemented("WORKER_CONFIGURATION_UNAVAILABLE", "Worker execution history is unavailable")
	}
	current, sequence, found, err := store.GetEffectiveWorkerConfiguration(ctx, id)
	if err != nil {
		return page, err
	}
	if !found {
		return page, apierr.Conflict("WORKER_CONFIGURATION_CHANGED", "Worker configuration disappeared during the read", nil)
	}
	page.Current, page.CurrentSequence = &current, sequence
	events, err := store.ListWorkerExecutions(ctx, id, after, limit)
	if err != nil {
		return page, err
	}
	for _, event := range events {
		if event.Sequence > sequence {
			break
		}
		operation, err := store.GetWorkerExecution(ctx, id, event.OperationID)
		if err != nil {
			return page, err
		}
		configuration := original
		if event.ExecutionID != "" {
			execution := operation
			if event.ExecutionID != operation.ID {
				execution, err = store.GetWorkerExecution(ctx, id, event.ExecutionID)
				if err != nil {
					return page, err
				}
			}
			configuration = &execution.Configuration
		}
		page.Events = append(page.Events, WorkerExecutionSummary{Activation: event, Actor: operation.Actor, Reason: operation.Reason, SourceKind: operation.SourceKind, SourceID: operation.SourceID, Harness: configuration.Effective.Harness, SessionMode: configuration.Effective.SessionMode, Config: configuration.Effective.Config, ProviderBindingID: configuration.Effective.ProviderBindingID, ContentHash: configuration.ContentHash, PreparedAt: operation.CreatedAt})
	}
	if len(page.Events) == limit && page.Events[len(page.Events)-1].Activation.Sequence < sequence {
		page.NextCursor = page.Events[len(page.Events)-1].Activation.Sequence
	}
	return page, nil
}

// WorkerExecution returns exact retained content for one session-owned change.
func (s *Service) WorkerExecution(ctx context.Context, id domain.SessionID, executionID string) (domain.WorkerExecution, error) {
	if _, err := s.WorkerConfiguration(ctx, id); err != nil {
		return domain.WorkerExecution{}, err
	}
	store, ok := s.store.(ports.WorkerExecutionStore)
	if !ok {
		return domain.WorkerExecution{}, apierr.NotImplemented("WORKER_CONFIGURATION_UNAVAILABLE", "Worker execution history is unavailable")
	}
	execution, err := store.GetWorkerExecution(ctx, id, executionID)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ports.ErrRegistryNotFound) {
		return domain.WorkerExecution{}, apierr.NotFound("WORKER_EXECUTION_NOT_FOUND", "Unknown worker execution")
	}
	return execution, err
}
