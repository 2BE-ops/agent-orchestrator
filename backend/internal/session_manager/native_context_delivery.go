package sessionmanager

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/sessionguard"
)

// nativeContextRecipientReady shares the existing mode-aware readiness checks
// while retaining an explicit role boundary for each caller.
func (m *Manager) nativeContextRecipientReady(ctx context.Context, rec domain.SessionRecord, kind domain.SessionKind) (bool, error) {
	if rec.IsTerminated || rec.Kind != kind {
		return false, nil
	}
	if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat {
		return m.chat != nil && m.chat.HasLiveChatController(rec.ID) && rec.Metadata.ControllerGeneration != "" && (rec.Activity.State == domain.ActivityIdle || rec.Activity.State == domain.ActivityActive), nil
	}
	if rec.Activity.State != domain.ActivityIdle || (rec.FirstSignalAt.IsZero() && m.harnessStartupSignalGatesInput(rec.Harness)) {
		return false, nil
	}
	return m.sourceGenerationCanReceiveCoordination(ctx, rec)
}

type nativeContextDelivery struct {
	SessionID   domain.SessionID
	ProjectID   domain.ProjectID
	Owner       domain.SessionControllerOwner
	Kind        domain.SessionKind
	Operation   agentOperationKind
	Prompt      string
	DeliveryKey string
	Check       func(context.Context) error
}

// deliverNativeContext is the one Chat/TUI coordination path for both workers
// and Managers. The caller supplies already sealed input; this path owns native
// operation exclusion, last-moment generation guards and transport observations.
func (m *Manager) deliverNativeContext(ctx context.Context, delivery nativeContextDelivery) (string, string) {
	notSent := func(reason string) (string, string) { return "not_sent", reason }
	if err := m.beginAgentOperation(ctx, delivery.SessionID, delivery.Operation); err != nil {
		return notSent("Native session operation prevented delivery before transport")
	}
	defer m.endAgentOperation(delivery.SessionID, delivery.Operation)
	rec, found, err := m.store.GetSession(ctx, delivery.SessionID)
	if err != nil || !found {
		return notSent("Recipient could not be read before transport")
	}
	if rec.Kind != delivery.Kind || rec.ControllerOwner() != delivery.Owner || rec.ProjectID != delivery.ProjectID {
		return notSent("Recipient native ownership changed before transport")
	}
	ready, err := m.nativeContextRecipientReady(ctx, rec, delivery.Kind)
	if err != nil || !ready {
		return notSent("Recipient readiness was not confirmed before transport")
	}
	if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat {
		if delivery.Check != nil {
			if err := delivery.Check(ctx); err != nil {
				return notSent("Durable input authority changed before the Chat relay")
			}
		}
		handled, err := m.sendChat(ctx, rec.ID, delivery.Prompt, delivery.DeliveryKey)
		if err != nil {
			return "uncertain", "Chat relay returned an error after admission; inspect the retained conversation before any retry"
		}
		if !handled {
			return notSent("Chat controller changed before relay")
		}
		return "handed_off", "Existing Chat controller accepted or had already retained this delivery key"
	}
	preWrite := func(writeCtx context.Context, current domain.SessionRecord) error {
		if current.Kind != delivery.Kind || current.ControllerOwner() != delivery.Owner || current.ProjectID != delivery.ProjectID || (current.FirstSignalAt.IsZero() && m.harnessStartupSignalGatesInput(current.Harness)) {
			return ports.ErrTaskLeaseFenced
		}
		if delivery.Check != nil {
			if err := delivery.Check(writeCtx); err != nil {
				return err
			}
		}
		return m.exactGenerationPreWrite(rec.ID, rec.Harness, ports.RuntimeHandle{ID: rec.Metadata.RuntimeHandleID}, domain.AgentGenerationID(rec.Metadata.RuntimeLaunchID), ports.ErrTaskLeaseFenced)(writeCtx, current)
	}
	outcome, sendErr := m.messenger.CoordinationUnderMutationChecked(ctx, rec.ID, delivery.Prompt, nil, nil, preWrite)
	if outcome != sessionguard.Sent {
		return notSent("Guard refused coordination before the runtime write")
	}
	if sendErr != nil {
		return "uncertain", "Runtime write returned an error; recipient acceptance is unknown"
	}
	return "handed_off", "Runtime paste and submit completed; recipient acknowledgement is unavailable"
}
