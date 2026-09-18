package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.ProjectKnowledgeStore = (*Store)(nil)

func knowledgeReadError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrKnowledgeNotFound
	}
	return err
}

func knowledgeFromRow(row gen.ProjectKnowledge) domain.ProjectKnowledge {
	return domain.ProjectKnowledge{ID: row.ID, ProjectID: domain.ProjectID(row.ProjectID), Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func knowledgeVersionFromRow(row gen.ProjectKnowledgeVersion) (domain.KnowledgeVersion, error) {
	v := domain.KnowledgeVersion{KnowledgeID: row.KnowledgeID, Number: row.Number, ContentHash: row.ContentHash, Reason: row.Reason, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Definition), &v.Definition); err != nil {
		return v, err
	}
	if err := json.Unmarshal([]byte(row.Actor), &v.Actor); err != nil {
		return v, err
	}
	if err := v.Definition.Validate(); err != nil {
		return v, err
	}
	_, hash, err := domain.TaskContent(v.Definition)
	if err != nil {
		return v, err
	}
	if hash != v.ContentHash {
		return v, fmt.Errorf("knowledge content hash mismatch")
	}
	return v, nil
}

func validateKnowledgeMutation(d domain.KnowledgeDefinition, m domain.KnowledgeMutation) error {
	if err := d.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrKnowledgeInvalid, err)
	}
	// Reuse provenance bounds while checking actual role authority separately.
	actor := m.Actor
	actor.Kind = "SYSTEM"
	if err := (domain.TaskMutation{Actor: actor, Reason: m.Reason, ExpectedRevision: m.ExpectedVersion}).ValidatePlanning(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrKnowledgeInvalid, err)
	}
	switch m.Actor.Kind {
	case "USER", "SYSTEM":
	case "WORKER", "ORCHESTRATOR":
		if m.Actor.SessionID == "" || m.ExpectedVersion != 0 || d.Status != "candidate" || d.Pinned {
			return ports.ErrKnowledgeForbidden
		}
	default:
		return ports.ErrKnowledgeForbidden
	}
	return nil
}

func validateKnowledgeScope(ctx context.Context, q *gen.Queries, id string, projectID domain.ProjectID, d domain.KnowledgeDefinition, mutation domain.KnowledgeMutation) error {
	project, err := q.GetProject(ctx, projectID)
	if err != nil {
		return knowledgeReadError(err)
	}
	if project.ArchivedAt.Valid {
		return fmt.Errorf("%w: project is archived", ports.ErrKnowledgeInvalid)
	}
	if mutation.Actor.Kind == "WORKER" || mutation.Actor.Kind == "ORCHESTRATOR" {
		row, err := q.GetSession(ctx, mutation.Actor.SessionID)
		if errors.Is(err, sql.ErrNoRows) {
			return ports.ErrKnowledgeForbidden
		}
		if err != nil {
			return err
		}
		rec := rowToRecord(row)
		expectedKind := domain.KindWorker
		if mutation.Actor.Kind == "ORCHESTRATOR" {
			expectedKind = domain.KindOrchestrator
		}
		if rec.ProjectID != projectID || rec.Kind != expectedKind || rec.IsTerminated {
			return ports.ErrKnowledgeForbidden
		}
	}
	for _, taskID := range d.TaskIDs {
		task, err := q.GetAdaptiveTask(ctx, taskID)
		if err != nil {
			return knowledgeReadError(err)
		}
		if task.ProjectID != string(projectID) {
			return ports.ErrKnowledgeForbidden
		}
	}
	ownWorkerSource := false
	for _, source := range d.Sources {
		if source.TaskID != "" {
			task, err := q.GetAdaptiveTask(ctx, source.TaskID)
			if err != nil {
				return knowledgeReadError(err)
			}
			if task.ProjectID != string(projectID) {
				return ports.ErrKnowledgeForbidden
			}
		}
		if source.SessionID != "" {
			row, err := q.GetSession(ctx, source.SessionID)
			if err != nil {
				return knowledgeReadError(err)
			}
			if rowToRecord(row).ProjectID != projectID {
				return ports.ErrKnowledgeForbidden
			}
		}
		if source.AttemptID != "" {
			attempt, err := q.GetTaskAttempt(ctx, source.AttemptID)
			if err != nil {
				return knowledgeReadError(err)
			}
			if attempt.TaskID != source.TaskID {
				return ports.ErrKnowledgeForbidden
			}
			if source.SessionID != "" {
				dispatch, err := q.GetTaskWorkerDispatch(ctx, source.AttemptID)
				if err != nil {
					return knowledgeReadError(err)
				}
				if dispatch.SessionID != string(source.SessionID) {
					return ports.ErrKnowledgeForbidden
				}
			}
		}
		if mutation.Actor.Kind == "WORKER" && source.Kind == "worker" {
			if source.SessionID != mutation.Actor.SessionID {
				return ports.ErrKnowledgeForbidden
			}
			lease, err := q.GetTaskLease(ctx, source.AttemptID)
			if err != nil {
				return knowledgeReadError(err)
			}
			if lease.ReleasedAt.Valid {
				return ports.ErrKnowledgeForbidden
			}
			ownWorkerSource = true
		}
	}
	if mutation.Actor.Kind == "WORKER" && !ownWorkerSource {
		return ports.ErrKnowledgeForbidden
	}
	if replacement := d.SupersededBy; replacement != nil {
		if replacement.ID == id {
			return ports.ErrKnowledgeInvalid
		}
		identity, err := q.GetProjectKnowledge(ctx, replacement.ID)
		if err != nil {
			return knowledgeReadError(err)
		}
		if identity.ProjectID != string(projectID) {
			return ports.ErrKnowledgeForbidden
		}
		row, err := q.GetKnowledgeVersion(ctx, gen.GetKnowledgeVersionParams{KnowledgeID: replacement.ID, Number: replacement.Version})
		if err != nil {
			return knowledgeReadError(err)
		}
		version, err := knowledgeVersionFromRow(row)
		if err != nil {
			return err
		}
		if version.Definition.Status != "accepted" {
			return fmt.Errorf("%w: replacement must reference accepted knowledge", ports.ErrKnowledgeInvalid)
		}
	}
	return nil
}

func insertKnowledgeVersion(ctx context.Context, q *gen.Queries, id string, number int64, definition domain.KnowledgeDefinition, mutation domain.KnowledgeMutation, now time.Time) (domain.KnowledgeVersion, error) {
	content, hash, err := domain.TaskContent(definition)
	if err != nil {
		return domain.KnowledgeVersion{}, err
	}
	actor, err := json.Marshal(mutation.Actor)
	if err != nil {
		return domain.KnowledgeVersion{}, err
	}
	err = q.InsertKnowledgeVersion(ctx, gen.InsertKnowledgeVersionParams{KnowledgeID: id, Number: number, Definition: string(content), ContentHash: hash, Actor: string(actor), Reason: mutation.Reason, CreatedAt: now})
	return domain.KnowledgeVersion{KnowledgeID: id, Number: number, Definition: definition, ContentHash: hash, Actor: mutation.Actor, Reason: mutation.Reason, CreatedAt: now}, err
}

// CreateProjectKnowledge persists a bounded claim with immutable provenance.
func (s *Store) CreateProjectKnowledge(ctx context.Context, id string, projectID domain.ProjectID, definition domain.KnowledgeDefinition, mutation domain.KnowledgeMutation) (domain.ProjectKnowledge, error) {
	if err := validateKnowledgeMutation(definition, mutation); err != nil {
		return domain.ProjectKnowledge{}, err
	}
	if mutation.ExpectedVersion != 0 {
		return domain.ProjectKnowledge{}, ports.ErrKnowledgeConflict
	}
	if strings.TrimSpace(id) == "" || len(id) > 200 || strings.ContainsRune(id, 0) || projectID == "" {
		return domain.ProjectKnowledge{}, ports.ErrKnowledgeInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.ProjectKnowledge{}, err
	}
	defer s.writeMu.Unlock()
	now := time.Now().UTC()
	result := domain.ProjectKnowledge{ID: id, ProjectID: projectID, Version: 1, CreatedAt: now, UpdatedAt: now}
	err := s.inTx(ctx, "create project knowledge", func(q *gen.Queries) error {
		if err := validateKnowledgeScope(ctx, q, id, projectID, definition, mutation); err != nil {
			return err
		}
		count, err := q.CountProjectKnowledge(ctx, string(projectID))
		if err != nil {
			return err
		}
		if count >= 10000 {
			return fmt.Errorf("%w: project knowledge history is limited to 10000 identities", ports.ErrKnowledgeInvalid)
		}
		if err := q.InsertProjectKnowledge(ctx, gen.InsertProjectKnowledgeParams{ID: id, ProjectID: string(projectID), CreatedAt: now, UpdatedAt: now}); err != nil {
			if isSQLiteUnique(err) {
				return ports.ErrKnowledgeConflict
			}
			return err
		}
		_, err = insertKnowledgeVersion(ctx, q, id, 1, definition, mutation, now)
		return err
	})
	return result, err
}

// ReviseProjectKnowledge retains previous claims even when invalidated/deleted.
func (s *Store) ReviseProjectKnowledge(ctx context.Context, id string, definition domain.KnowledgeDefinition, mutation domain.KnowledgeMutation) (domain.KnowledgeVersion, error) {
	if err := validateKnowledgeMutation(definition, mutation); err != nil {
		return domain.KnowledgeVersion{}, err
	}
	if mutation.ExpectedVersion < 1 {
		return domain.KnowledgeVersion{}, ports.ErrKnowledgeConflict
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.KnowledgeVersion{}, err
	}
	defer s.writeMu.Unlock()
	var result domain.KnowledgeVersion
	err := s.inTx(ctx, "revise project knowledge", func(q *gen.Queries) error {
		row, err := q.GetProjectKnowledge(ctx, id)
		if err != nil {
			return knowledgeReadError(err)
		}
		if row.Version != mutation.ExpectedVersion {
			return ports.ErrKnowledgeConflict
		}
		if err := validateKnowledgeScope(ctx, q, id, domain.ProjectID(row.ProjectID), definition, mutation); err != nil {
			return err
		}
		now := time.Now().UTC()
		result, err = insertKnowledgeVersion(ctx, q, id, row.Version+1, definition, mutation, now)
		if err != nil {
			return err
		}
		changed, err := q.ActivateKnowledgeVersion(ctx, gen.ActivateKnowledgeVersionParams{ID: id, Version: result.Number, Version_2: row.Version, UpdatedAt: now})
		if err != nil {
			return err
		}
		if changed != 1 {
			return ports.ErrKnowledgeConflict
		}
		return nil
	})
	return result, err
}

// GetProjectKnowledge reads identity independently from historical versions.
func (s *Store) GetProjectKnowledge(ctx context.Context, id string) (domain.ProjectKnowledge, error) {
	row, err := s.qr.GetProjectKnowledge(ctx, id)
	return knowledgeFromRow(row), knowledgeReadError(err)
}

// GetKnowledgeVersion verifies exact historical content before returning it.
func (s *Store) GetKnowledgeVersion(ctx context.Context, id string, version int64) (domain.KnowledgeVersion, error) {
	row, err := s.qr.GetKnowledgeVersion(ctx, gen.GetKnowledgeVersionParams{KnowledgeID: id, Number: version})
	if err != nil {
		return domain.KnowledgeVersion{}, knowledgeReadError(err)
	}
	return knowledgeVersionFromRow(row)
}

// ListKnowledgeVersions returns bounded history in monotonic version order.
func (s *Store) ListKnowledgeVersions(ctx context.Context, id string, after int64, limit int) ([]domain.KnowledgeVersion, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrKnowledgeInvalid
	}
	rows, err := s.qr.ListKnowledgeVersions(ctx, gen.ListKnowledgeVersionsParams{KnowledgeID: id, Number: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.KnowledgeVersion, 0, len(rows))
	for _, row := range rows {
		version, err := knowledgeVersionFromRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, version)
	}
	return result, nil
}

// ListProjectKnowledge searches bounded current content; history remains exact.
func (s *Store) ListProjectKnowledge(ctx context.Context, filter domain.KnowledgeFilter) ([]domain.ProjectKnowledge, error) {
	if filter.Limit < 1 || filter.Limit > 100 || len(filter.After) > 200 || len(filter.Search) > 200 || len(filter.Status) > 30 || len(filter.Kind) > 30 {
		return nil, ports.ErrKnowledgeInvalid
	}
	rows, err := s.qr.ListProjectKnowledge(ctx, gen.ListProjectKnowledgeParams{ProjectID: string(filter.ProjectID), AfterID: filter.After, PageLimit: int64(filter.Limit), Status: filter.Status, Kind: filter.Kind, Search: filter.Search})
	if err != nil {
		return nil, err
	}
	result := make([]domain.ProjectKnowledge, 0, len(rows))
	for _, row := range rows {
		result = append(result, knowledgeFromRow(row))
	}
	return result, nil
}
