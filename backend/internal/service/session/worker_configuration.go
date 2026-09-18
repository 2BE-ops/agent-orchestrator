package session

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
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
