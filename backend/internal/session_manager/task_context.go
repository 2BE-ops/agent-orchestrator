package sessionmanager

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// A task's frozen input takes precedence over mutable/fallback session metadata.
// Native Chat adoption keeps its provider history; a fresh TUI launch receives
// this exact retained prompt. Neither path rebuilds context from current facts.
func (m *Manager) restoreTaskContext(ctx context.Context, rec domain.SessionRecord) (domain.SessionRecord, error) {
	leases, ok := m.store.(ports.TaskLeaseStore)
	if !ok {
		return rec, nil
	}
	dispatch, found, err := leases.GetTaskWorkerDispatchBySession(ctx, rec.ID)
	if err != nil || !found {
		return rec, err
	}
	store, ok := m.store.(ports.TaskContextStore)
	if !ok {
		return rec, taskExecutionUnavailable()
	}
	snapshot, found, err := store.GetTaskContext(ctx, dispatch.AttemptID)
	if err != nil {
		return rec, err
	}
	if !found || snapshot.SessionID != rec.ID || snapshot.ConfigurationHash != dispatch.ConfigurationHash {
		return rec, taskExecutionFenced(fmt.Errorf("task context is not sealed; reconcile the original dispatch"))
	}
	rec.Metadata.Prompt = snapshot.Prompt
	return rec, nil
}
