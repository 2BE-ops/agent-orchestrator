package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.OrchestratorNoticeStore = (*Store)(nil)

func orchestratorNoticeFromRow(row gen.AdaptiveOrchestratorNotice) (domain.OrchestratorNotice, error) {
	notice := domain.OrchestratorNotice{ID: row.ID, ProjectID: domain.ProjectID(row.ProjectID), TaskID: row.TaskID,
		Fact: row.Fact, Anchor: row.Anchor, Revision: row.Revision, Detail: row.Detail,
		State: row.State, Reason: row.Reason, CreatedAt: row.CreatedAt}
	if row.ResolvedAt.Valid {
		resolved := row.ResolvedAt.Time
		notice.ResolvedAt = &resolved
	}
	if notice.State != "pending" {
		return notice, orchestratorNoticeRetainedValid(notice)
	}
	return notice, notice.Validate()
}

// orchestratorNoticeRetainedValid re-checks a settled notice without requiring
// the pending shape Validate enforces for new rows.
func orchestratorNoticeRetainedValid(n domain.OrchestratorNotice) error {
	if err := domain.ValidateOrchestratorNoticeFact(n.Fact); err != nil {
		return err
	}
	switch n.State {
	case "handed_off", "not_sent", "uncertain":
	default:
		return fmt.Errorf("invalid orchestrator notice state %q", n.State)
	}
	if n.ResolvedAt == nil || n.ResolvedAt.IsZero() {
		return fmt.Errorf("settled orchestrator notice lost its resolution timestamp")
	}
	return nil
}

// BeginOrchestratorNotice inserts one pending notice keyed by its durable
// anchor. The journal mints the pending state itself; an existing anchor
// returns the retained row so a fact is journalled and delivered at most once.
func (s *Store) BeginOrchestratorNotice(ctx context.Context, input domain.OrchestratorNotice) (domain.OrchestratorNotice, bool, error) {
	notice := input
	notice.State, notice.Reason, notice.ResolvedAt = "pending", "", nil
	if err := notice.Validate(); err != nil {
		return domain.OrchestratorNotice{}, false, fmt.Errorf("%w: %w", ports.ErrGoalInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.OrchestratorNotice{}, false, err
	}
	defer s.writeMu.Unlock()
	created := false
	err := s.inTx(ctx, "begin orchestrator notice", func(q *gen.Queries) error {
		changed, err := q.InsertOrchestratorNotice(ctx, gen.InsertOrchestratorNoticeParams{ID: notice.ID,
			ProjectID: string(notice.ProjectID), TaskID: notice.TaskID, Fact: notice.Fact, Anchor: notice.Anchor,
			Revision: notice.Revision, Detail: notice.Detail, CreatedAt: notice.CreatedAt})
		if err != nil {
			// The unique anchor is the dedup boundary: a replayed fact keeps its
			// original row and delivery outcome instead of journalling twice.
			if isSQLiteUnique(err) {
				return nil
			}
			return err
		}
		created = changed == 1
		return nil
	})
	if err != nil || created {
		return notice, created, err
	}
	row, err := s.qr.GetOrchestratorNoticeByAnchor(ctx, gen.GetOrchestratorNoticeByAnchorParams{
		ProjectID: string(notice.ProjectID), TaskID: notice.TaskID, Fact: notice.Fact, Anchor: notice.Anchor})
	if err != nil {
		return domain.OrchestratorNotice{}, false, taskReadError(err)
	}
	retained, err := orchestratorNoticeFromRow(row)
	return retained, false, err
}

// ResolveOrchestratorNotice settles a pending notice exactly once. The store
// keeps the change count so callers can surface a lost race instead of
// assuming delivery bookkeeping succeeded.
func (s *Store) ResolveOrchestratorNotice(ctx context.Context, resolution domain.OrchestratorNoticeResolution) error {
	if err := resolution.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrGoalInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "resolve orchestrator notice", func(q *gen.Queries) error {
		changed, err := q.ResolveOrchestratorNotice(ctx, gen.ResolveOrchestratorNoticeParams{
			State: resolution.State, Reason: resolution.Reason,
			ResolvedAt: sql.NullTime{Time: resolution.ResolvedAt, Valid: true}, ID: resolution.ID})
		if err != nil {
			return err
		}
		if changed != 1 {
			return ports.ErrGoalConflict
		}
		return nil
	})
}

// ListPendingOrchestratorNotices bounds startup reconciliation over every
// notice a previous daemon run never settled.
func (s *Store) ListPendingOrchestratorNotices(ctx context.Context, limit int) ([]domain.OrchestratorNotice, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("%w: orchestrator notice limit must be between 1 and 1000", ports.ErrGoalInvalid)
	}
	rows, err := s.qr.ListPendingOrchestratorNotices(ctx, int64(limit))
	if err != nil {
		return nil, err
	}
	items := make([]domain.OrchestratorNotice, 0, len(rows))
	for _, row := range rows {
		notice, err := orchestratorNoticeFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, notice)
	}
	return items, nil
}

// ListOrchestratorNoticeProjects pages candidate projects by their retained
// orchestrator sessions, so dispatch only derives feedback where a live
// orchestrator could receive it.
func (s *Store) ListOrchestratorNoticeProjects(ctx context.Context, afterProjectID string, limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: orchestrator notice project limit must be between 1 and 100", ports.ErrGoalInvalid)
	}
	cursor := domain.ProjectID(afterProjectID)
	rows, err := s.qr.ListOrchestratorNoticeProjects(ctx, gen.ListOrchestratorNoticeProjectsParams{ProjectID: &cursor, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	projects := make([]string, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		projects = append(projects, string(*row))
	}
	return projects, nil
}
