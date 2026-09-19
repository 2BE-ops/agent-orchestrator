package task

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type workerIdentity struct {
	attemptID  string
	owner      domain.SessionControllerOwner
	activation int64
}

// workerSubmissionIdentity derives attribution from durable session facts. The
// writing transaction rechecks these observations before retaining any output.
func (m *Manager) workerSubmissionIdentity(ctx context.Context, sessionID domain.SessionID, generation string) (workerIdentity, error) {
	var identity workerIdentity
	rec, found, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return identity, err
	}
	if !found {
		return identity, ports.ErrTaskNotFound
	}
	identity.owner = rec.ControllerOwner()
	current := identity.owner.RuntimeLaunchID
	if identity.owner.Mode == domain.SessionModeChat {
		current = identity.owner.ControllerGeneration
	}
	if rec.Kind != domain.KindWorker || current != generation {
		return identity, ports.ErrTaskLeaseFenced
	}
	dispatch, found, err := m.store.GetTaskWorkerDispatchBySession(ctx, sessionID)
	if err != nil {
		return identity, err
	}
	if !found {
		return identity, ports.ErrTaskNotFound
	}
	_, identity.activation, found, err = m.store.GetEffectiveWorkerConfiguration(ctx, sessionID)
	if err != nil {
		return identity, err
	}
	if !found {
		return identity, ports.ErrTaskNotFound
	}
	identity.attemptID = dispatch.AttemptID
	return identity, nil
}
