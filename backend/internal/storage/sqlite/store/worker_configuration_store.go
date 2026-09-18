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

var _ ports.WorkerConfigurationStore = (*Store)(nil)

// CreateConfiguredSession commits the session seed and immutable launch facts
// together, rechecking mutable availability/policy within the same transaction.
func (s *Store) CreateConfiguredSession(ctx context.Context, rec domain.SessionRecord, snapshot domain.WorkerConfiguration) (domain.SessionRecord, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.SessionRecord{}, err
	}
	defer s.writeMu.Unlock()
	var created domain.SessionRecord
	err := s.inTx(ctx, "create configured session", func(q *gen.Queries) error {
		var err error
		created, err = createConfiguredSession(ctx, q, rec, snapshot)
		return err
	})
	if err != nil {
		return domain.SessionRecord{}, err
	}
	return created, nil
}

// The task dispatch transaction uses the same seed/configuration boundary.
func createConfiguredSession(ctx context.Context, q *gen.Queries, rec domain.SessionRecord, snapshot domain.WorkerConfiguration) (domain.SessionRecord, error) {
	if err := snapshot.Validate(); err != nil {
		return domain.SessionRecord{}, err
	}
	if rec.Kind != domain.KindWorker || rec.Harness != snapshot.Effective.Harness || rec.Mode != snapshot.Effective.SessionMode || rec.Metadata.Permissions != snapshot.Effective.Config.Permissions {
		return domain.SessionRecord{}, fmt.Errorf("session seed does not match worker configuration")
	}
	if err := validateWorkerReference(ctx, q, snapshot.AgentType, domain.RegistryAgentType, snapshot.Origin); err != nil {
		return domain.SessionRecord{}, err
	}
	for _, skill := range snapshot.Skills {
		if err := validateWorkerReference(ctx, q, skill.Reference, domain.RegistrySkill, snapshot.Origin); err != nil {
			return domain.SessionRecord{}, err
		}
	}
	if snapshot.Provider != nil {
		row, err := q.GetProviderBinding(ctx, snapshot.Provider.ID)
		if err != nil {
			return domain.SessionRecord{}, registryReadError(err)
		}
		binding := providerBindingFromGen(row)
		if !binding.Enabled || binding.Revision != snapshot.Provider.Revision || binding.Harness != snapshot.Effective.Harness || binding.Provider != snapshot.Provider.Provider || (binding.ProjectID != "" && binding.ProjectID != string(rec.ProjectID)) {
			return domain.SessionRecord{}, ports.ErrRegistryConflict
		}
	}
	created, err := createSessionRow(ctx, q, rec)
	if err != nil {
		return domain.SessionRecord{}, err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return domain.SessionRecord{}, err
	}
	err = q.InsertWorkerConfiguration(ctx, gen.InsertWorkerConfigurationParams{SessionID: string(created.ID), AgentTypeID: snapshot.AgentType.ID, AgentTypeVersion: snapshot.AgentType.Version, Configuration: string(encoded), ContentHash: snapshot.ContentHash, CreatedAt: snapshot.CreatedAt})
	return created, err
}

func validateWorkerReference(ctx context.Context, q *gen.Queries, ref domain.WorkerDefinitionRef, kind domain.RegistryKind, origin domain.RegistryOrigin) error {
	entry, err := q.GetRegistryEntry(ctx, ref.ID)
	if err != nil {
		return registryReadError(err)
	}
	if entry.Kind != string(kind) || entry.Enabled == 0 {
		return ports.ErrRegistryInvalid
	}
	if origin == domain.RegistryManager && entry.ManagerCanSelect == 0 {
		return ports.ErrRegistryForbidden
	}
	version, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: ref.ID, Number: ref.Version})
	if err != nil {
		return registryReadError(err)
	}
	if version.ContentHash != ref.ContentHash {
		return ports.ErrRegistryConflict
	}
	return nil
}

// GetWorkerConfiguration reads and verifies retained launch content. Registry
// edits/disables do not invalidate history; fresh launch checks are separate.
func (s *Store) GetWorkerConfiguration(ctx context.Context, id domain.SessionID) (domain.WorkerConfiguration, bool, error) {
	row, err := s.qr.GetWorkerConfiguration(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkerConfiguration{}, false, nil
	}
	if err != nil {
		return domain.WorkerConfiguration{}, false, err
	}
	snapshot, err := workerConfigurationFromRow(row)
	return snapshot, err == nil, err
}

func workerConfigurationFromRow(row gen.AdaptiveWorkerConfiguration) (domain.WorkerConfiguration, error) {
	var snapshot domain.WorkerConfiguration
	if err := json.Unmarshal([]byte(row.Configuration), &snapshot); err != nil {
		return snapshot, err
	}
	if snapshot.ContentHash != row.ContentHash || snapshot.AgentType.ID != row.AgentTypeID || snapshot.AgentType.Version != row.AgentTypeVersion {
		return snapshot, fmt.Errorf("stored worker configuration identity is inconsistent")
	}
	if err := snapshot.Validate(); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}
