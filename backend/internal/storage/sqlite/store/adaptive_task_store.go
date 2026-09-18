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

var _ ports.AdaptiveTaskStore = (*Store)(nil)

func taskReadError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrTaskNotFound
	}
	return err
}

func validateTaskMutation(m domain.TaskMutation) error {
	if m.Actor.Kind != "USER" && m.Actor.Kind != "ORCHESTRATOR" && m.Actor.Kind != "SYSTEM" {
		return ports.ErrTaskForbidden
	}
	if err := m.ValidatePlanning(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
	}
	return nil
}

func validateTaskActor(ctx context.Context, q *gen.Queries, projectID string, actor domain.AdaptiveActor) error {
	project, err := q.GetProject(ctx, domain.ProjectID(projectID))
	if err != nil {
		return taskReadError(err)
	}
	if project.ArchivedAt.Valid {
		return fmt.Errorf("%w: project is archived", ports.ErrTaskInvalid)
	}
	if actor.Kind == "ORCHESTRATOR" {
		row, err := q.GetSession(ctx, actor.SessionID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ports.ErrTaskForbidden
			}
			return err
		}
		rec := rowToRecord(row)
		if string(rec.ProjectID) != projectID || rec.Kind != domain.KindOrchestrator || rec.IsTerminated {
			return ports.ErrTaskForbidden
		}
	}
	return nil
}

func taskFromRow(row gen.AdaptiveTask) (domain.AdaptiveTask, error) {
	task := domain.AdaptiveTask{ID: row.ID, ProjectID: domain.ProjectID(row.ProjectID), Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	err := json.Unmarshal([]byte(row.CreatedBy), &task.CreatedBy)
	return task, err
}

func taskRevisionContent(definition domain.TaskDefinition, criteria int64) ([]byte, string, error) {
	return domain.TaskContent(struct {
		Definition      domain.TaskDefinition `json:"definition"`
		CriteriaVersion int64                 `json:"criteriaVersion"`
	}{definition, criteria})
}

func taskRevisionFromRow(row gen.AdaptiveTaskRevision) (domain.TaskRevision, error) {
	r := domain.TaskRevision{TaskID: row.TaskID, Number: row.Number, CriteriaVersion: row.CriteriaVersion.Int64, ContentHash: row.ContentHash, Reason: row.Reason, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Definition), &r.Definition); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(row.Actor), &r.Actor); err != nil {
		return r, err
	}
	if err := r.Definition.Validate(); err != nil {
		return r, err
	}
	_, hash, err := taskRevisionContent(r.Definition, r.CriteriaVersion)
	if err != nil {
		return r, err
	}
	if hash != r.ContentHash {
		return r, fmt.Errorf("task revision content hash mismatch")
	}
	return r, nil
}

func insertTaskRevision(ctx context.Context, q *gen.Queries, id string, number, criteria int64, definition domain.TaskDefinition, mutation domain.TaskMutation, now time.Time) (domain.TaskRevision, error) {
	_, hash, err := taskRevisionContent(definition, criteria)
	if err != nil {
		return domain.TaskRevision{}, err
	}
	content, err := json.Marshal(definition)
	if err != nil {
		return domain.TaskRevision{}, err
	}
	actor, err := json.Marshal(mutation.Actor)
	if err != nil {
		return domain.TaskRevision{}, err
	}
	err = q.InsertAdaptiveTaskRevision(ctx, gen.InsertAdaptiveTaskRevisionParams{TaskID: id, Number: number, CriteriaVersion: sql.NullInt64{Int64: criteria, Valid: criteria > 0}, Definition: string(content), ContentHash: hash, Actor: string(actor), Reason: mutation.Reason, CreatedAt: now})
	return domain.TaskRevision{TaskID: id, Number: number, CriteriaVersion: criteria, Definition: definition, ContentHash: hash, Actor: mutation.Actor, Reason: mutation.Reason, CreatedAt: now}, err
}

func insertTaskCriteria(ctx context.Context, q *gen.Queries, id string, number int64, criteria domain.AcceptanceCriteria, mutation domain.TaskMutation, now time.Time) error {
	content, hash, err := domain.TaskContent(criteria)
	if err != nil {
		return err
	}
	actor, err := json.Marshal(mutation.Actor)
	if err != nil {
		return err
	}
	return q.InsertAdaptiveTaskCriteria(ctx, gen.InsertAdaptiveTaskCriteriaParams{TaskID: id, Number: number, PreviousVersion: sql.NullInt64{Int64: number - 1, Valid: number > 1}, Definition: string(content), ContentHash: hash, Actor: string(actor), Reason: mutation.Reason, CreatedAt: now})
}

func insertTaskAudit(ctx context.Context, q *gen.Queries, id string, revision int64, action string, mutation domain.TaskMutation, now time.Time) error {
	actor, err := json.Marshal(mutation.Actor)
	if err != nil {
		return err
	}
	return q.InsertAdaptiveTaskAudit(ctx, gen.InsertAdaptiveTaskAuditParams{TaskID: id, Revision: revision, Action: action, Actor: string(actor), Reason: mutation.Reason, CreatedAt: now})
}

func replaceTaskDependencies(ctx context.Context, q *gen.Queries, projectID, id string, dependencies []string) error {
	if err := q.ClearAdaptiveTaskDependencies(ctx, id); err != nil {
		return err
	}
	for _, dependency := range dependencies {
		if err := q.InsertAdaptiveTaskDependency(ctx, gen.InsertAdaptiveTaskDependencyParams{ProjectID: projectID, TaskID: id, DependencyID: dependency}); err != nil {
			return err
		}
	}
	return nil
}

// CreateAdaptiveTask atomically creates identity, graph, criteria and first revision.
func (s *Store) CreateAdaptiveTask(ctx context.Context, id string, projectID domain.ProjectID, definition domain.TaskDefinition, criteria *domain.AcceptanceCriteria, mutation domain.TaskMutation) (domain.AdaptiveTask, error) {
	if err := validateTaskMutation(mutation); err != nil {
		return domain.AdaptiveTask{}, err
	}
	if mutation.ExpectedRevision != 0 {
		return domain.AdaptiveTask{}, ports.ErrTaskConflict
	}
	if strings.TrimSpace(id) == "" || len(id) > 200 || strings.ContainsRune(id, 0) || projectID == "" {
		return domain.AdaptiveTask{}, ports.ErrTaskInvalid
	}
	if err := definition.Validate(); err != nil {
		return domain.AdaptiveTask{}, fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
	}
	if criteria != nil {
		if err := criteria.Validate(); err != nil {
			return domain.AdaptiveTask{}, fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
		}
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.AdaptiveTask{}, err
	}
	defer s.writeMu.Unlock()
	now := time.Now().UTC()
	result := domain.AdaptiveTask{ID: id, ProjectID: projectID, Revision: 1, CreatedBy: mutation.Actor, CreatedAt: now, UpdatedAt: now}
	err := s.inTx(ctx, "create adaptive task", func(q *gen.Queries) error {
		if err := validateTaskActor(ctx, q, string(projectID), mutation.Actor); err != nil {
			return err
		}
		if err := validateAdaptiveTaskGraph(ctx, q, string(projectID), id, definition); err != nil {
			return err
		}
		actor, err := json.Marshal(mutation.Actor)
		if err != nil {
			return err
		}
		if err := q.InsertAdaptiveTask(ctx, gen.InsertAdaptiveTaskParams{ID: id, ProjectID: string(projectID), ParentID: sql.NullString{String: definition.ParentID, Valid: definition.ParentID != ""}, CreatedBy: string(actor), CreatedAt: now, UpdatedAt: now}); err != nil {
			if isSQLiteUnique(err) {
				return ports.ErrTaskConflict
			}
			return err
		}
		criteriaVersion := int64(0)
		if criteria != nil {
			criteriaVersion = 1
			if err := insertTaskCriteria(ctx, q, id, 1, *criteria, mutation, now); err != nil {
				return err
			}
		}
		if _, err := insertTaskRevision(ctx, q, id, 1, criteriaVersion, definition, mutation, now); err != nil {
			return err
		}
		if err := replaceTaskDependencies(ctx, q, string(projectID), id, definition.Dependencies); err != nil {
			return err
		}
		return insertTaskAudit(ctx, q, id, 1, "created", mutation, now)
	})
	return result, err
}

// ReviseAdaptiveTask appends and activates a planning revision without changing old criteria.
func (s *Store) ReviseAdaptiveTask(ctx context.Context, id string, definition domain.TaskDefinition, mutation domain.TaskMutation) (domain.TaskRevision, error) {
	return s.reviseAdaptiveTask(ctx, id, &definition, nil, mutation)
}

// ReviseAcceptanceCriteria appends criteria and a planning revision pointing to them.
func (s *Store) ReviseAcceptanceCriteria(ctx context.Context, id string, criteria domain.AcceptanceCriteria, mutation domain.TaskMutation) (domain.TaskRevision, error) {
	return s.reviseAdaptiveTask(ctx, id, nil, &criteria, mutation)
}

func (s *Store) reviseAdaptiveTask(ctx context.Context, id string, definition *domain.TaskDefinition, criteria *domain.AcceptanceCriteria, mutation domain.TaskMutation) (domain.TaskRevision, error) {
	if err := validateTaskMutation(mutation); err != nil {
		return domain.TaskRevision{}, err
	}
	if definition != nil {
		if err := definition.Validate(); err != nil {
			return domain.TaskRevision{}, fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
		}
	}
	if criteria != nil {
		if err := criteria.Validate(); err != nil {
			return domain.TaskRevision{}, fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
		}
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.TaskRevision{}, err
	}
	defer s.writeMu.Unlock()
	var result domain.TaskRevision
	err := s.inTx(ctx, "revise adaptive task", func(q *gen.Queries) error {
		row, err := q.GetAdaptiveTask(ctx, id)
		if err != nil {
			return taskReadError(err)
		}
		if row.Revision != mutation.ExpectedRevision {
			return ports.ErrTaskConflict
		}
		if err := validateTaskActor(ctx, q, row.ProjectID, mutation.Actor); err != nil {
			return err
		}
		current, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: id, Number: row.Revision})
		if err != nil {
			return err
		}
		previous, err := taskRevisionFromRow(current)
		if err != nil {
			return err
		}
		if definition == nil {
			definition = &previous.Definition
		}
		if err := validateAdaptiveTaskGraph(ctx, q, row.ProjectID, id, *definition); err != nil {
			return err
		}
		now := time.Now().UTC()
		criteriaVersion, action := previous.CriteriaVersion, "revised"
		if criteria != nil {
			criteriaVersion++
			action = "criteria_revised"
			if err := insertTaskCriteria(ctx, q, id, criteriaVersion, *criteria, mutation, now); err != nil {
				return err
			}
		}
		result, err = insertTaskRevision(ctx, q, id, row.Revision+1, criteriaVersion, *definition, mutation, now)
		if err != nil {
			return err
		}
		if err := replaceTaskDependencies(ctx, q, row.ProjectID, id, definition.Dependencies); err != nil {
			return err
		}
		changed, err := q.ActivateAdaptiveTaskRevision(ctx, gen.ActivateAdaptiveTaskRevisionParams{ID: id, Revision: result.Number, Revision_2: row.Revision, ParentID: sql.NullString{String: definition.ParentID, Valid: definition.ParentID != ""}, UpdatedAt: now})
		if err != nil {
			return err
		}
		if changed != 1 {
			return ports.ErrTaskConflict
		}
		return insertTaskAudit(ctx, q, id, result.Number, action, mutation, now)
	})
	return result, err
}

// GetAdaptiveTask returns a stable task identity and its active revision pointer.
func (s *Store) GetAdaptiveTask(ctx context.Context, id string) (domain.AdaptiveTask, error) {
	row, err := s.qr.GetAdaptiveTask(ctx, id)
	if err != nil {
		return domain.AdaptiveTask{}, taskReadError(err)
	}
	return taskFromRow(row)
}

// ListAdaptiveTasks pages project-scoped identities without loading unbounded content.
func (s *Store) ListAdaptiveTasks(ctx context.Context, projectID domain.ProjectID, cursor string, limit int) ([]domain.AdaptiveTask, error) {
	if limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	ids, err := s.qr.ListAdaptiveTaskIDs(ctx, gen.ListAdaptiveTaskIDsParams{ProjectID: string(projectID), ID: cursor, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	tasks := make([]domain.AdaptiveTask, 0, len(ids))
	for _, id := range ids {
		task, err := s.GetAdaptiveTask(ctx, id)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// GetTaskRevision returns immutable planning content and its exact criteria reference.
func (s *Store) GetTaskRevision(ctx context.Context, id string, number int64) (domain.TaskRevision, error) {
	row, err := s.qr.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: id, Number: number})
	if err != nil {
		return domain.TaskRevision{}, taskReadError(err)
	}
	return taskRevisionFromRow(row)
}

// ListTaskRevisions pages immutable planning history.
func (s *Store) ListTaskRevisions(ctx context.Context, id string, after int64, limit int) ([]domain.TaskRevision, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListAdaptiveTaskRevisions(ctx, gen.ListAdaptiveTaskRevisionsParams{TaskID: id, Number: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	revisions := make([]domain.TaskRevision, 0, len(rows))
	for _, row := range rows {
		revision, err := taskRevisionFromRow(row)
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}
	return revisions, nil
}

// GetAcceptanceCriteria reads and verifies a retained definition of success.
func (s *Store) GetAcceptanceCriteria(ctx context.Context, id string, number int64) (domain.AcceptanceCriteriaVersion, error) {
	row, err := s.qr.GetAdaptiveTaskCriteria(ctx, gen.GetAdaptiveTaskCriteriaParams{TaskID: id, Number: number})
	if err != nil {
		return domain.AcceptanceCriteriaVersion{}, taskReadError(err)
	}
	result := domain.AcceptanceCriteriaVersion{TaskID: id, Number: row.Number, PreviousVersion: row.PreviousVersion.Int64, ContentHash: row.ContentHash, Reason: row.Reason, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Definition), &result.Definition); err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(row.Actor), &result.Actor); err != nil {
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
		return result, fmt.Errorf("acceptance criteria content hash mismatch")
	}
	return result, nil
}

// ListTaskAudit retains bounded semantic history independently of change_log.
func (s *Store) ListTaskAudit(ctx context.Context, id string, after int64, limit int) ([]domain.TaskAudit, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrTaskInvalid
	}
	rows, err := s.qr.ListAdaptiveTaskAudit(ctx, gen.ListAdaptiveTaskAuditParams{TaskID: id, Seq: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.TaskAudit, 0, len(rows))
	for _, row := range rows {
		event := domain.TaskAudit{Sequence: row.Seq, TaskID: id, Revision: row.Revision, Action: row.Action, Reason: row.Reason, CreatedAt: row.CreatedAt}
		if err := json.Unmarshal([]byte(row.Actor), &event.Actor); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, nil
}
