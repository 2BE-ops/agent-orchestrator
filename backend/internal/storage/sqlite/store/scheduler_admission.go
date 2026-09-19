package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// DefaultMaxConcurrentWorkers matches the migration default so a settings read
// failure fails open to the durable default rather than to an unbounded daemon.
const DefaultMaxConcurrentWorkers = 100

// schedulerWorkerCap reads the durable daemon-wide worker cap inside the
// caller's write transaction.
func schedulerWorkerCap(ctx context.Context, q *gen.Queries) (int, error) {
	settings, err := q.GetAppSettings(ctx)
	if err != nil {
		return 0, err
	}
	limit := int(settings.MaxConcurrentWorkers)
	if limit < 1 {
		limit = DefaultMaxConcurrentWorkers
	}
	return limit, nil
}

// admitSessionByKind enforces the daemon-wide concurrent worker cap for every
// session-row creation. Orchestrator and agent_manager sessions never consume
// worker capacity. Running under the store's single-writer lock, the count and
// the insert cannot interleave with another spawn, and the count derives from
// durable session state, so restarts and worker termination keep it exact.
// A project whose control state fences admissions refuses worker creation in
// the same transaction, so no launch caller can slip past a pause or stop.
func admitSessionByKind(ctx context.Context, q *gen.Queries, rec domain.SessionRecord) error {
	if rec.Kind != domain.KindWorker {
		return nil
	}
	if rec.ProjectID != "" {
		control, err := q.GetProjectControl(ctx, string(rec.ProjectID))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// An absent control row is the durable running default.
		if err == nil && domain.ProjectControlState(control.State) != domain.ProjectRunning {
			return fmt.Errorf("%w: project is %s", ports.ErrProjectAdmissionsFenced, control.State)
		}
	}
	limit, err := schedulerWorkerCap(ctx, q)
	if err != nil {
		return err
	}
	active, err := q.CountActiveWorkerSessions(ctx)
	if err != nil {
		return err
	}
	if int(active) >= limit {
		return ports.ErrSchedulerWorkerLimit
	}
	return nil
}

// admitAgentTypeWorkers enforces one Agent Type's pinned maxParallelWorkers
// for configured worker launches, inside the same transaction that creates
// the session and persists its immutable launch snapshot.
func admitAgentTypeWorkers(ctx context.Context, q *gen.Queries, agentTypeID string, typeLimit int) error {
	if agentTypeID == "" || typeLimit < 1 {
		return nil
	}
	active, err := q.CountActiveWorkerSessionsByType(ctx, agentTypeID)
	if err != nil {
		return err
	}
	if int(active) >= typeLimit {
		return ports.ErrSchedulerAgentTypeLimit
	}
	return nil
}
