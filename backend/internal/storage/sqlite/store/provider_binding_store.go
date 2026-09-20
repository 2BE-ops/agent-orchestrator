package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// CreateProviderBinding stores a native reference and audit in one transaction.
func (s *Store) CreateProviderBinding(ctx context.Context, binding domain.ProviderBinding, mutation domain.RegistryMutation) (domain.ProviderBinding, error) {
	if err := binding.Validate(); err != nil {
		return domain.ProviderBinding{}, err
	}
	if strings.TrimSpace(binding.ID) == "" || len(binding.ID) > 200 {
		return domain.ProviderBinding{}, fmt.Errorf("invalid provider binding id")
	}
	if err := bindingMutation(mutation); err != nil {
		return domain.ProviderBinding{}, err
	}
	if mutation.ExpectedRevision != 0 {
		return domain.ProviderBinding{}, ports.ErrRegistryConflict
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.ProviderBinding{}, err
	}
	defer s.writeMu.Unlock()
	now := time.Now().UTC()
	var result domain.ProviderBinding
	err := s.inTx(ctx, "create provider binding", func(q *gen.Queries) error {
		if err := q.CreateProviderBinding(ctx, gen.CreateProviderBindingParams{ID: binding.ID, Name: binding.Name, Harness: string(binding.Harness), Provider: binding.Provider, ProjectID: binding.ProjectID, Enabled: boolInt(binding.Enabled), CreatedAt: now, UpdatedAt: now}); err != nil {
			if isSQLiteUnique(err) {
				return ports.ErrRegistryConflict
			}
			return err
		}
		if err := q.CreateProviderBindingAudit(ctx, gen.CreateProviderBindingAuditParams{BindingID: binding.ID, Revision: 1, Action: "created", ActorID: mutation.Actor.ID, Reason: mutation.Reason, Name: binding.Name, Enabled: boolInt(binding.Enabled), CreatedAt: now}); err != nil {
			return err
		}
		row, err := q.GetProviderBinding(ctx, binding.ID)
		result = providerBindingFromGen(row)
		return err
	})
	return result, err
}

// GetProviderBinding resolves the exact local reference, including disabled ones.
func (s *Store) GetProviderBinding(ctx context.Context, id string) (domain.ProviderBinding, error) {
	row, err := s.qr.GetProviderBinding(ctx, id)
	return providerBindingFromGen(row), registryReadError(err)
}

// ListProviderBindings returns a bounded stable page across harnesses.
func (s *Store) ListProviderBindings(ctx context.Context, after string, limit int) ([]domain.ProviderBinding, error) {
	if err := registryPage(limit); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListProviderBindings(ctx, gen.ListProviderBindingsParams{AfterID: after, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.ProviderBinding, 0, len(rows))
	for _, row := range rows {
		result = append(result, providerBindingFromGen(row))
	}
	return result, nil
}

// UpdateProviderBinding changes only the label/enabled state. The native target
// remains immutable, preventing a name edit from silently rerouting workers.
func (s *Store) UpdateProviderBinding(ctx context.Context, id, name string, enabled bool, mutation domain.RegistryMutation) (domain.ProviderBinding, error) {
	if err := bindingMutation(mutation); err != nil {
		return domain.ProviderBinding{}, err
	}
	if mutation.ExpectedRevision < 1 {
		return domain.ProviderBinding{}, ports.ErrRegistryConflict
	}
	if err := (domain.RegistryMetadata{Name: name}).Validate(); err != nil {
		return domain.ProviderBinding{}, err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.ProviderBinding{}, err
	}
	defer s.writeMu.Unlock()
	var result domain.ProviderBinding
	err := s.inTx(ctx, "update provider binding", func(q *gen.Queries) error {
		current, err := q.GetProviderBinding(ctx, id)
		if err != nil {
			return registryReadError(err)
		}
		if current.Revision != mutation.ExpectedRevision {
			return ports.ErrRegistryConflict
		}
		now := time.Now().UTC()
		updated, err := q.UpdateProviderBinding(ctx, gen.UpdateProviderBindingParams{ID: id, Name: name, Enabled: boolInt(enabled), ExpectedRevision: mutation.ExpectedRevision, UpdatedAt: now})
		if err != nil {
			return err
		}
		if updated != 1 {
			return ports.ErrRegistryConflict
		}
		if err := q.CreateProviderBindingAudit(ctx, gen.CreateProviderBindingAuditParams{BindingID: id, Revision: current.Revision + 1, Action: "updated", ActorID: mutation.Actor.ID, Reason: mutation.Reason, Name: name, Enabled: boolInt(enabled), CreatedAt: now}); err != nil {
			return err
		}
		row, err := q.GetProviderBinding(ctx, id)
		result = providerBindingFromGen(row)
		return err
	})
	return result, err
}

// ListProviderBindingAudit exposes retained changes without native credentials.
func (s *Store) ListProviderBindingAudit(ctx context.Context, id string, after int64, limit int) ([]domain.ProviderBindingAudit, error) {
	if err := registryPage(limit); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListProviderBindingAudit(ctx, gen.ListProviderBindingAuditParams{BindingID: id, AfterSeq: after, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.ProviderBindingAudit, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.ProviderBindingAudit{Sequence: row.Seq, BindingID: row.BindingID, Revision: row.Revision, Action: row.Action, ActorID: row.ActorID, Reason: row.Reason, Name: row.Name, Enabled: row.Enabled != 0, CreatedAt: row.CreatedAt})
	}
	return result, nil
}

func bindingMutation(mutation domain.RegistryMutation) error {
	if err := mutation.Validate(); err != nil {
		return err
	}
	if mutation.Actor.Origin != domain.RegistryUser {
		return ports.ErrRegistryForbidden
	}
	return nil
}

func providerBindingFromGen(row gen.AdaptiveProviderBinding) domain.ProviderBinding {
	return domain.ProviderBinding{ID: row.ID, Name: row.Name, Harness: domain.AgentHarness(row.Harness), Provider: row.Provider, ProjectID: row.ProjectID, Enabled: row.Enabled != 0, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
