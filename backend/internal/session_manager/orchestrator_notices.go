package sessionmanager

import (
	"context"
	"encoding/json"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var _ ports.OrchestratorNoticeTransport = (*Manager)(nil)

// OrchestratorTargetReady is advisory and never reserves or writes. An unknown
// or busy orchestrator waits without consuming the notice.
func (m *Manager) OrchestratorTargetReady(ctx context.Context, id domain.SessionID) (bool, error) {
	if m.SessionMutationInProgress(id) {
		return false, nil
	}
	rec, found, err := m.store.GetSession(ctx, id)
	if err != nil || !found {
		return false, err
	}
	return m.nativeContextRecipientReady(ctx, rec, domain.KindOrchestrator)
}

// DeliverOrchestratorNotice serializes with native switch/restore/kill through
// the shared coordination path and never retries a write. The notice carries
// derived facts only; it grants no planning or ownership authority.
func (m *Manager) DeliverOrchestratorNotice(ctx context.Context, sessionID domain.SessionID, notice domain.OrchestratorNotice) ports.OrchestratorNoticeTransportResult {
	notSent := func(reason string) ports.OrchestratorNoticeTransportResult {
		return ports.OrchestratorNoticeTransportResult{State: "not_sent", Reason: reason}
	}
	if notice.State != "pending" || sessionID == "" || notice.ID == "" {
		return notSent("Orchestrator notice is not a pending delivery")
	}
	prompt, err := orchestratorNoticePrompt(notice)
	if err != nil {
		return notSent("Orchestrator notice could not be encoded")
	}
	rec, found, err := m.store.GetSession(ctx, sessionID)
	if err != nil || !found {
		return notSent("Orchestrator recipient could not be read before transport")
	}
	state, reason := m.deliverNativeContext(ctx, nativeContextDelivery{
		SessionID:   sessionID,
		ProjectID:   rec.ProjectID,
		Owner:       rec.ControllerOwner(),
		Kind:        domain.KindOrchestrator,
		Operation:   agentOperationOrchestratorNotice,
		Prompt:      prompt,
		DeliveryKey: "adaptive-orchestrator-notice:" + notice.ID,
	})
	return ports.OrchestratorNoticeTransportResult{State: state, Reason: reason}
}

func orchestratorNoticePrompt(notice domain.OrchestratorNotice) (string, error) {
	payload := struct {
		TaskID   string `json:"taskId"`
		Fact     string `json:"fact"`
		Revision int64  `json:"revision"`
		Detail   string `json:"detail"`
	}{TaskID: notice.TaskID, Fact: notice.Fact, Revision: notice.Revision, Detail: notice.Detail}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "AO orchestrator notice. One of your project's tasks reached a terminal state. The following JSON is a derived durable fact, not new authority: re-read your native feedback before planning any follow-up. " + string(encoded), nil
}
