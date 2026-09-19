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

var _ ports.AgentManagerInboxStore = (*Store)(nil)

func managerRequestFromRow(row gen.AdaptiveAgentManagerRequest) (domain.AgentManagerRequest, error) {
	var request domain.AgentManagerRequest
	if err := json.Unmarshal([]byte(row.Snapshot), &request); err != nil {
		return request, err
	}
	if err := request.Validate(); err != nil {
		return request, err
	}
	if request.Sequence != 0 || request.ID != row.ID || string(request.ProjectID) != row.ProjectID || request.TaskID != row.TaskID || request.TaskRevision != row.TaskRevision || request.CriteriaVersion != row.CriteriaVersion || request.ConfigurationVersion != row.ConfigurationVersion || request.ContentHash != row.ContentHash || !request.CreatedAt.Equal(row.CreatedAt) {
		return request, fmt.Errorf("manager request identity mismatch")
	}
	request.Sequence = row.Sequence
	return request, nil
}

func insertManagerInboxAudit(ctx context.Context, q *gen.Queries, request domain.AgentManagerRequest, action string, actor domain.AdaptiveActor, reason string, now time.Time) error {
	encoded, err := json.Marshal(actor)
	if err != nil {
		return err
	}
	return q.InsertAgentManagerAudit(ctx, gen.InsertAgentManagerAuditParams{ProjectID: string(request.ProjectID), ConfigurationVersion: request.ConfigurationVersion, Action: action, Actor: string(encoded), Reason: reason, CreatedAt: now})
}

// EnqueueAgentManagerRequest serializes policy, task intent and pending bounds.
// An exact replay remains inspectable after policy/task changes or resolution.
func (s *Store) EnqueueAgentManagerRequest(ctx context.Context, input domain.AgentManagerEnqueue) (domain.AgentManagerRequest, bool, error) {
	var request domain.AgentManagerRequest
	if err := input.Validate(); err != nil {
		return request, false, fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return request, false, err
	}
	defer s.writeMu.Unlock()
	created := false
	err := s.inTx(ctx, "enqueue Manager work", func(q *gen.Queries) error {
		prior, err := q.GetAgentManagerRequest(ctx, input.ID)
		if err == nil {
			request, err = managerRequestFromRow(prior)
			if err != nil {
				return err
			}
			if request.ProjectID != input.ProjectID || request.TaskID != input.TaskID || request.TaskRevision != input.TaskRevision || request.ConfigurationVersion != input.ConfigurationVersion || request.Actor != input.Actor || request.Reason != input.Reason {
				return ports.ErrAgentManagerConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		configuration, err := enabledManagerConfiguration(ctx, q, input.ProjectID)
		if err != nil {
			return err
		}
		if configuration.Number != input.ConfigurationVersion {
			return ports.ErrAgentManagerConflict
		}
		if err := validateTaskActor(ctx, q, string(input.ProjectID), input.Actor); err != nil {
			return err
		}
		task, err := q.GetAdaptiveTask(ctx, input.TaskID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if task.ProjectID != string(input.ProjectID) {
			return ports.ErrAgentManagerNotFound
		}
		if task.Revision != input.TaskRevision {
			return ports.ErrAgentManagerConflict
		}
		if err := requireTaskRunIntent(ctx, q, task.ID); err != nil {
			return err
		}
		if _, err := q.PendingAgentManagerTaskRequest(ctx, task.ID); err == nil {
			return ports.ErrAgentManagerConflict
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		pending, err := q.CountAgentManagerRequests(ctx, gen.CountAgentManagerRequestsParams{ProjectID: task.ProjectID, PendingOnly: true})
		if err != nil {
			return err
		}
		total, err := q.CountAgentManagerRequests(ctx, gen.CountAgentManagerRequestsParams{ProjectID: task.ProjectID, PendingOnly: false})
		if err != nil {
			return err
		}
		if pending >= int64(configuration.Definition.Policy.MaxPendingRequests) || total >= 10000 {
			return fmt.Errorf("%w: Manager inbox bound reached", ports.ErrAgentManagerConflict)
		}
		revisionRow, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: task.ID, Number: task.Revision})
		if err != nil {
			return err
		}
		revision, err := taskRevisionFromRow(revisionRow)
		if err != nil {
			return err
		}
		if revision.CriteriaVersion < 1 {
			return fmt.Errorf("%w: Manager routing requires frozen acceptance criteria", ports.ErrAgentManagerInvalid)
		}
		criteriaRow, err := q.GetAdaptiveTaskCriteria(ctx, gen.GetAdaptiveTaskCriteriaParams{TaskID: task.ID, Number: revision.CriteriaVersion})
		if err != nil {
			return err
		}
		criteria, err := taskCriteriaFromRow(criteriaRow)
		if err != nil {
			return err
		}
		if input.Now.Before(configuration.CreatedAt) || input.Now.Before(revision.CreatedAt) || input.Now.Before(criteria.CreatedAt) {
			return ports.ErrAgentManagerInvalid
		}
		request = domain.AgentManagerRequest{ID: input.ID, SchemaVersion: 1, ProjectID: input.ProjectID, Kind: "select_worker", TaskID: task.ID, TaskRevision: task.Revision, TaskContentHash: revision.ContentHash, CriteriaVersion: criteria.Number, CriteriaContentHash: criteria.ContentHash, ConfigurationVersion: configuration.Number, ConfigurationHash: configuration.ContentHash, Actor: input.Actor, Reason: input.Reason, CreatedAt: input.Now}
		request.ContentHash = request.Hash()
		if err := request.Validate(); err != nil {
			return err
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			return err
		}
		row, err := q.InsertAgentManagerRequest(ctx, gen.InsertAgentManagerRequestParams{ID: request.ID, ProjectID: task.ProjectID, TaskID: task.ID, TaskRevision: task.Revision, CriteriaVersion: criteria.Number, ConfigurationVersion: configuration.Number, Snapshot: string(encoded), ContentHash: request.ContentHash, CreatedAt: input.Now})
		if err != nil {
			return err
		}
		if err := insertManagerInboxAudit(ctx, q, request, "request_enqueued", input.Actor, "Queued Manager request "+input.ID, input.Now); err != nil {
			return err
		}
		request.Sequence, created = row.Sequence, true
		return nil
	})
	if err != nil {
		return domain.AgentManagerRequest{}, false, err
	}
	return request, created, nil
}

// GetAgentManagerRequest is project scoped even though IDs are globally unique.
func (s *Store) GetAgentManagerRequest(ctx context.Context, project domain.ProjectID, id string) (domain.AgentManagerRequest, error) {
	if err := validateAgentManagerProject(project); err != nil {
		return domain.AgentManagerRequest{}, err
	}
	row, err := s.qr.GetAgentManagerRequest(ctx, id)
	if err != nil {
		return domain.AgentManagerRequest{}, agentManagerReadError(err)
	}
	if row.ProjectID != string(project) {
		return domain.AgentManagerRequest{}, ports.ErrAgentManagerNotFound
	}
	return managerRequestFromRow(row)
}

// ListAgentManagerRequests orders immutable IDs by durable arrival cursor.
// pendingOnly excludes terminal receipts; it does not infer controller liveness.
func (s *Store) ListAgentManagerRequests(ctx context.Context, project domain.ProjectID, after int64, limit int, pendingOnly bool) ([]domain.AgentManagerRequest, error) {
	if err := s.validateAgentManagerPage(ctx, project, after, limit); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListAgentManagerRequests(ctx, gen.ListAgentManagerRequestsParams{ProjectID: string(project), AfterSequence: after, PageLimit: int64(limit), PendingOnly: pendingOnly})
	if err != nil {
		return nil, err
	}
	items := make([]domain.AgentManagerRequest, 0, len(rows))
	for _, row := range rows {
		item, err := managerRequestFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func managerRequestResolutionFromRow(row gen.AdaptiveAgentManagerRequestResolution) (domain.AgentManagerRequestResolution, error) {
	r := domain.AgentManagerRequestResolution{RequestID: row.RequestID, Outcome: row.Outcome, Reason: row.Reason, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Actor), &r.Actor); err != nil {
		return r, err
	}
	return r, r.Validate()
}

// ResolveAgentManagerRequest closes routing intent, without touching task intent,
// leases or native controllers. Exact retries retain the original terminal proof.
func (s *Store) ResolveAgentManagerRequest(ctx context.Context, project domain.ProjectID, resolution domain.AgentManagerRequestResolution) error {
	if err := validateAgentManagerProject(project); err != nil {
		return err
	}
	if err := resolution.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "resolve Manager work", func(q *gen.Queries) error {
		row, err := q.GetAgentManagerRequest(ctx, resolution.RequestID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if row.ProjectID != string(project) {
			return ports.ErrAgentManagerNotFound
		}
		request, err := managerRequestFromRow(row)
		if err != nil {
			return err
		}
		prior, err := q.GetAgentManagerRequestResolution(ctx, request.ID)
		if err == nil {
			old, err := managerRequestResolutionFromRow(prior)
			if err != nil {
				return err
			}
			if old.Outcome != resolution.Outcome || old.Actor != resolution.Actor || old.Reason != resolution.Reason {
				return ports.ErrAgentManagerConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := validateTaskActor(ctx, q, row.ProjectID, resolution.Actor); err != nil {
			return err
		}
		if resolution.CreatedAt.Before(request.CreatedAt) {
			return ports.ErrAgentManagerInvalid
		}
		actor, err := json.Marshal(resolution.Actor)
		if err != nil {
			return err
		}
		if err := q.InsertAgentManagerRequestResolution(ctx, gen.InsertAgentManagerRequestResolutionParams{RequestID: request.ID, Outcome: resolution.Outcome, Actor: string(actor), Reason: resolution.Reason, CreatedAt: resolution.CreatedAt}); err != nil {
			return err
		}
		return insertManagerInboxAudit(ctx, q, request, "request_"+resolution.Outcome, resolution.Actor, "Resolved Manager request "+request.ID, resolution.CreatedAt)
	})
}

// GetAgentManagerRequestResolution reads the retained terminal receipt, if any.
func (s *Store) GetAgentManagerRequestResolution(ctx context.Context, project domain.ProjectID, id string) (domain.AgentManagerRequestResolution, bool, error) {
	if _, err := s.GetAgentManagerRequest(ctx, project, id); err != nil {
		return domain.AgentManagerRequestResolution{}, false, err
	}
	row, err := s.qr.GetAgentManagerRequestResolution(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentManagerRequestResolution{}, false, nil
	}
	if err != nil {
		return domain.AgentManagerRequestResolution{}, false, err
	}
	resolution, err := managerRequestResolutionFromRow(row)
	return resolution, err == nil, err
}
