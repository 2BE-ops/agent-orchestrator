package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.RegistryStore = (*Store)(nil)

// CreateRegistryEntry atomically creates identity, first version, pins and audit.
func (s *Store) CreateRegistryEntry(ctx context.Context, id string, kind domain.RegistryKind, metadata domain.RegistryMetadata, definition domain.RegistryDefinition, mutation domain.RegistryMutation) (domain.RegistryEntry, error) {
	if strings.TrimSpace(id) == "" || len(id) > 200 || strings.ContainsRune(id, 0) {
		return domain.RegistryEntry{}, fmt.Errorf("invalid registry id")
	}
	if err := metadata.Validate(); err != nil {
		return domain.RegistryEntry{}, err
	}
	if err := mutation.Validate(); err != nil {
		return domain.RegistryEntry{}, err
	}
	if mutation.ExpectedRevision != 0 {
		return domain.RegistryEntry{}, ports.ErrRegistryConflict
	}
	content, hash, err := definition.MarshalContent(kind)
	if err != nil {
		return domain.RegistryEntry{}, err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.RegistryEntry{}, err
	}
	defer s.writeMu.Unlock()
	now := time.Now().UTC()
	var result domain.RegistryEntry
	err = s.inTx(ctx, "create registry entry", func(q *gen.Queries) error {
		if err := q.CreateRegistryEntry(ctx, gen.CreateRegistryEntryParams{
			ID: id, Kind: string(kind), Name: metadata.Name, Description: metadata.Description,
			Origin: string(mutation.Actor.Origin), CreatedBy: mutation.Actor.ID, Enabled: boolInt(metadata.Enabled),
			ManagerCanSelect: boolInt(metadata.Policy.ManagerCanSelect), ManagerCanModify: boolInt(metadata.Policy.ManagerCanModify),
			ManagerCanVersion: boolInt(metadata.Policy.ManagerCanVersion), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			if isSQLiteUnique(err) {
				return ports.ErrRegistryConflict
			}
			return err
		}
		if err := insertRegistryVersion(ctx, q, domain.RegistryVersion{
			EntryID: id, Number: 1, Definition: definition, ContentHash: hash, Actor: mutation.Actor, Reason: mutation.Reason, CreatedAt: now,
		}, kind, content); err != nil {
			return err
		}
		if err := insertRegistryAudit(ctx, q, id, 1, 1, "created", mutation, now); err != nil {
			return err
		}
		row, err := q.GetRegistryEntry(ctx, id)
		result = registryEntryFromGen(row)
		return err
	})
	if err != nil {
		return domain.RegistryEntry{}, err
	}
	return result, nil
}

// GetRegistryEntry reads metadata without resolving a floating version.
func (s *Store) GetRegistryEntry(ctx context.Context, id string) (domain.RegistryEntry, error) {
	row, err := s.qr.GetRegistryEntry(ctx, id)
	if err != nil {
		return domain.RegistryEntry{}, registryReadError(err)
	}
	return registryEntryFromGen(row), nil
}

// ListRegistryEntries uses stable ID cursors and bounded pages.
func (s *Store) ListRegistryEntries(ctx context.Context, kind domain.RegistryKind, afterID string, limit int) ([]domain.RegistryEntry, error) {
	if kind != domain.RegistryAgentType && kind != domain.RegistrySkill {
		return nil, fmt.Errorf("invalid registry kind")
	}
	if err := registryPage(limit); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListRegistryEntries(ctx, gen.ListRegistryEntriesParams{Kind: string(kind), AfterID: afterID, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.RegistryEntry, 0, len(rows))
	for _, row := range rows {
		result = append(result, registryEntryFromGen(row))
	}
	return result, nil
}

// GetRegistryVersion reads exactly the requested historical version.
func (s *Store) GetRegistryVersion(ctx context.Context, id string, number int64) (domain.RegistryVersion, error) {
	row, err := s.qr.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: id, Number: number})
	if err != nil {
		return domain.RegistryVersion{}, registryReadError(err)
	}
	return registryVersionFromGen(row)
}

// ListRegistryVersions returns immutable history, in creation order.
func (s *Store) ListRegistryVersions(ctx context.Context, id string, afterVersion int64, limit int) ([]domain.RegistryVersion, error) {
	if err := registryPage(limit); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListRegistryVersions(ctx, gen.ListRegistryVersionsParams{EntryID: id, AfterVersion: afterVersion, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.RegistryVersion, 0, len(rows))
	for _, row := range rows {
		version, err := registryVersionFromGen(row)
		if err != nil {
			return nil, err
		}
		result = append(result, version)
	}
	return result, nil
}

// AppendRegistryVersion retains the active version until a separate activation.
// Ownership is checked inside the write transaction, closing the policy race.
func (s *Store) AppendRegistryVersion(ctx context.Context, id string, definition domain.RegistryDefinition, mutation domain.RegistryMutation) (domain.RegistryVersion, error) {
	if err := mutation.Validate(); err != nil {
		return domain.RegistryVersion{}, err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.RegistryVersion{}, err
	}
	defer s.writeMu.Unlock()
	var result domain.RegistryVersion
	err := s.inTx(ctx, "append registry version", func(q *gen.Queries) error {
		entry, err := registryMutationEntry(ctx, q, id, mutation)
		if err != nil {
			return err
		}
		if mutation.Actor.Origin == domain.RegistryManager && !entry.Metadata.Policy.ManagerCanVersion {
			return ports.ErrRegistryForbidden
		}
		content, hash, err := definition.MarshalContent(entry.Kind)
		if err != nil {
			return err
		}
		number, err := q.NextRegistryVersion(ctx, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		result = domain.RegistryVersion{EntryID: id, Number: number, ParentVersion: entry.ActiveVersion,
			Definition: definition.NormalizeLists(), ContentHash: hash, Actor: mutation.Actor, Reason: mutation.Reason, CreatedAt: now}
		if err := insertRegistryVersion(ctx, q, result, entry.Kind, content); err != nil {
			return err
		}
		count, err := q.AdvanceRegistryRevision(ctx, gen.AdvanceRegistryRevisionParams{ID: id, Revision: entry.Revision, UpdatedAt: now})
		if err := registryCAS(count, err); err != nil {
			return err
		}
		return insertRegistryAudit(ctx, q, id, entry.Revision+1, number, "version_created", mutation, now)
	})
	if err != nil {
		return domain.RegistryVersion{}, err
	}
	return result, nil
}

// UpdateRegistryMetadata cannot change origin, history or manager ownership flags
// on behalf of the manager. Disabling retains historical versions and pins.
func (s *Store) UpdateRegistryMetadata(ctx context.Context, id string, metadata domain.RegistryMetadata, mutation domain.RegistryMutation) (domain.RegistryEntry, error) {
	if err := metadata.Validate(); err != nil {
		return domain.RegistryEntry{}, err
	}
	return s.mutateRegistryEntry(ctx, id, mutation, "metadata_updated", func(q *gen.Queries, entry domain.RegistryEntry, now time.Time) error {
		if mutation.Actor.Origin == domain.RegistryManager && (!entry.Metadata.Policy.ManagerCanModify || entry.Metadata.Policy != metadata.Policy) {
			return ports.ErrRegistryForbidden
		}
		count, err := q.UpdateRegistryMetadata(ctx, gen.UpdateRegistryMetadataParams{
			ID: id, Revision: entry.Revision, Name: metadata.Name, Description: metadata.Description, Enabled: boolInt(metadata.Enabled),
			ManagerCanSelect: boolInt(metadata.Policy.ManagerCanSelect), ManagerCanModify: boolInt(metadata.Policy.ManagerCanModify),
			ManagerCanVersion: boolInt(metadata.Policy.ManagerCanVersion), UpdatedAt: now,
		})
		return registryCAS(count, err)
	})
}

// ActivateRegistryVersion selects an immutable version for future launches.
// Manager promotion requires a separate explicit project approval policy; this
// primitive only permits user/system activation until that gate is integrated.
func (s *Store) ActivateRegistryVersion(ctx context.Context, id string, number int64, mutation domain.RegistryMutation) (domain.RegistryEntry, error) {
	return s.mutateRegistryEntry(ctx, id, mutation, "version_activated", func(q *gen.Queries, entry domain.RegistryEntry, now time.Time) error {
		if mutation.Actor.Origin == domain.RegistryManager {
			return ports.ErrRegistryForbidden
		}
		if _, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: id, Number: number}); err != nil {
			return registryReadError(err)
		}
		count, err := q.ActivateRegistryVersion(ctx, gen.ActivateRegistryVersionParams{ID: id, Revision: entry.Revision, ActiveVersion: number, UpdatedAt: now})
		return registryCAS(count, err)
	})
}

func (s *Store) mutateRegistryEntry(ctx context.Context, id string, mutation domain.RegistryMutation, action string, apply func(*gen.Queries, domain.RegistryEntry, time.Time) error) (domain.RegistryEntry, error) {
	if err := mutation.Validate(); err != nil {
		return domain.RegistryEntry{}, err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.RegistryEntry{}, err
	}
	defer s.writeMu.Unlock()
	var result domain.RegistryEntry
	err := s.inTx(ctx, action, func(q *gen.Queries) error {
		entry, err := registryMutationEntry(ctx, q, id, mutation)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := apply(q, entry, now); err != nil {
			return err
		}
		row, err := q.GetRegistryEntry(ctx, id)
		if err != nil {
			return err
		}
		if err := insertRegistryAudit(ctx, q, id, row.Revision, row.ActiveVersion, action, mutation, now); err != nil {
			return err
		}
		result = registryEntryFromGen(row)
		return err
	})
	if err != nil {
		return domain.RegistryEntry{}, err
	}
	return result, nil
}

// ListRegistryAudit returns durable, ordered semantic history with bounded pages.
func (s *Store) ListRegistryAudit(ctx context.Context, id string, afterSeq int64, limit int) ([]domain.RegistryAudit, error) {
	if err := registryPage(limit); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListRegistryAudit(ctx, gen.ListRegistryAuditParams{EntryID: id, AfterSeq: afterSeq, PageLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.RegistryAudit, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.RegistryAudit{Sequence: row.Seq, EntryID: row.EntryID, Revision: row.Revision, VersionNumber: row.VersionNumber,
			Action: row.Action, Actor: domain.RegistryActor{Origin: domain.RegistryOrigin(row.ActorOrigin), ID: row.ActorID}, Reason: row.Reason, CreatedAt: row.CreatedAt})
	}
	return result, nil
}

func insertRegistryVersion(ctx context.Context, q *gen.Queries, version domain.RegistryVersion, kind domain.RegistryKind, content []byte) error {
	if version.Definition.AgentType != nil {
		for index, pin := range version.Definition.AgentType.Skills {
			skill, err := q.GetRegistryEntry(ctx, pin.ID)
			if err != nil {
				return registryReadError(err)
			}
			if skill.Kind != string(domain.RegistrySkill) || skill.Enabled == 0 {
				return fmt.Errorf("%w: pinned skill must be an enabled skill", ports.ErrRegistryInvalid)
			}
			if version.Actor.Origin == domain.RegistryManager && skill.ManagerCanSelect == 0 {
				return ports.ErrRegistryForbidden
			}
			if _, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: pin.ID, Number: pin.Version}); err != nil {
				return registryReadError(err)
			}
			if err := q.CreateRegistrySkillPin(ctx, gen.CreateRegistrySkillPinParams{EntryID: version.EntryID, Version: version.Number,
				Position: int64(index), SkillID: pin.ID, SkillVersion: pin.Version}); err != nil {
				return err
			}
		}
	}
	return q.CreateRegistryVersion(ctx, gen.CreateRegistryVersionParams{
		EntryID: version.EntryID, Number: version.Number, Kind: string(kind), ParentVersion: sql.NullInt64{Int64: version.ParentVersion, Valid: version.ParentVersion != 0},
		Definition: string(content), ContentHash: version.ContentHash, ActorOrigin: string(version.Actor.Origin), ActorID: version.Actor.ID, Reason: version.Reason, CreatedAt: version.CreatedAt,
	})
}

func insertRegistryAudit(ctx context.Context, q *gen.Queries, id string, revision, versionNumber int64, action string, mutation domain.RegistryMutation, now time.Time) error {
	return q.CreateRegistryAudit(ctx, gen.CreateRegistryAuditParams{EntryID: id, Revision: revision, VersionNumber: versionNumber, Action: action,
		ActorOrigin: string(mutation.Actor.Origin), ActorID: mutation.Actor.ID, Reason: mutation.Reason, CreatedAt: now})
}

func registryMutationEntry(ctx context.Context, q *gen.Queries, id string, mutation domain.RegistryMutation) (domain.RegistryEntry, error) {
	row, err := q.GetRegistryEntry(ctx, id)
	if err != nil {
		return domain.RegistryEntry{}, registryReadError(err)
	}
	if row.Revision != mutation.ExpectedRevision {
		return domain.RegistryEntry{}, ports.ErrRegistryConflict
	}
	return registryEntryFromGen(row), nil
}

func registryEntryFromGen(row gen.AdaptiveRegistry) domain.RegistryEntry {
	return domain.RegistryEntry{ID: row.ID, Kind: domain.RegistryKind(row.Kind), Origin: domain.RegistryOrigin(row.Origin), CreatedBy: row.CreatedBy,
		Metadata: domain.RegistryMetadata{Name: row.Name, Description: row.Description, Enabled: row.Enabled != 0,
			Policy: domain.RegistryPolicy{ManagerCanSelect: row.ManagerCanSelect != 0, ManagerCanModify: row.ManagerCanModify != 0, ManagerCanVersion: row.ManagerCanVersion != 0}},
		Revision: row.Revision, ActiveVersion: row.ActiveVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func registryVersionFromGen(row gen.AdaptiveRegistryVersion) (domain.RegistryVersion, error) {
	var definition domain.RegistryDefinition
	if err := json.Unmarshal([]byte(row.Definition), &definition); err != nil {
		return domain.RegistryVersion{}, fmt.Errorf("decode registry version: %w", err)
	}
	hash := sha256.Sum256([]byte(row.Definition))
	if hex.EncodeToString(hash[:]) != row.ContentHash {
		return domain.RegistryVersion{}, fmt.Errorf("registry content hash mismatch")
	}
	return domain.RegistryVersion{EntryID: row.EntryID, Number: row.Number, ParentVersion: row.ParentVersion.Int64,
		Definition: definition, ContentHash: row.ContentHash, Actor: domain.RegistryActor{Origin: domain.RegistryOrigin(row.ActorOrigin), ID: row.ActorID},
		Reason: row.Reason, CreatedAt: row.CreatedAt}, nil
}

func registryReadError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrRegistryNotFound
	}
	return err
}

func registryCAS(count int64, err error) error {
	if err != nil {
		return err
	}
	if count != 1 {
		return ports.ErrRegistryConflict
	}
	return nil
}

func registryPage(limit int) error {
	if limit < 1 || limit > 200 {
		return fmt.Errorf("registry page limit must be between 1 and 200")
	}
	return nil
}
