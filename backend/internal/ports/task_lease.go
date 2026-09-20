package ports

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ErrTaskLeaseFenced rejects stale owners and overlapping exclusive workers.
var ErrTaskLeaseFenced = errors.New("task lease is owned, expired, released or fenced")

// TaskLeaseStore records exclusive ownership and atomic worker association.
// ReserveTask is an internal persistence boundary, not a scheduler admission API.
// Services must perform full admission through the shared scheduler.
type TaskLeaseStore interface {
	ReserveTask(context.Context, domain.TaskReservation) (domain.TaskAttempt, domain.TaskLease, error)
	GetTaskAttempt(context.Context, string) (domain.TaskAttempt, error)
	ListTaskAttempts(context.Context, string, int64, int) ([]domain.TaskAttempt, error)
	GetTaskLease(context.Context, string) (domain.TaskLease, error)
	GetActiveTaskLease(context.Context, string) (domain.TaskLease, bool, error)
	ListExpiredTaskLeases(context.Context, time.Time, string, int) ([]domain.TaskLease, error)
	RenewTaskLease(context.Context, domain.TaskLeaseToken, time.Time, time.Duration, *time.Time) error
	RecoverTaskLease(context.Context, domain.TaskLeaseRecovery) error
	ReleaseTaskLease(context.Context, domain.TaskLeaseRecovery) error
	CreateTaskWorkerSession(context.Context, domain.TaskLeaseToken, domain.SessionRecord, domain.WorkerConfiguration, time.Time) (domain.SessionRecord, bool, error)
	GetTaskWorkerDispatch(context.Context, string) (domain.TaskWorkerDispatch, bool, error)
	GetTaskWorkerDispatchBySession(context.Context, domain.SessionID) (domain.TaskWorkerDispatch, bool, error)
}
