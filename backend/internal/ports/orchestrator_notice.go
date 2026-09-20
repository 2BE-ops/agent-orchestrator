package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// OrchestratorNoticeStore owns the push half of the orchestrator loop: an
// immutable journal of terminal task facts delivered to a live orchestrator
// session. Facts are derived at read time; only delivery bookkeeping persists.
type OrchestratorNoticeStore interface {
	BeginOrchestratorNotice(context.Context, domain.OrchestratorNotice) (domain.OrchestratorNotice, bool, error)
	ResolveOrchestratorNotice(context.Context, domain.OrchestratorNoticeResolution) error
	ListPendingOrchestratorNotices(context.Context, int) ([]domain.OrchestratorNotice, error)
	ListOrchestratorNoticeProjects(context.Context, string, int) ([]string, error)
}

// OrchestratorNoticeTransportResult distinguishes a proven no-write from an
// ambiguous transport outcome, mirroring the other native delivery surfaces.
type OrchestratorNoticeTransportResult struct {
	State  string
	Reason string
}

// OrchestratorNoticeTransport delivers through the existing native session
// boundary. Readiness is advisory and consumes nothing; delivery rechecks the
// reserved owner against the supplied live orchestrator session.
type OrchestratorNoticeTransport interface {
	OrchestratorTargetReady(context.Context, domain.SessionID) (bool, error)
	DeliverOrchestratorNotice(context.Context, domain.SessionID, domain.OrchestratorNotice) OrchestratorNoticeTransportResult
}
