package domain

import (
	"fmt"
	"strings"
	"time"
)

// WorkerExecution records a deliberately changed execution configuration. The
// original launch remains immutable. Preparing a segment does not activate it;
// activation belongs to the transaction that transfers controller ownership.
type WorkerExecution struct {
	ID                 string              `json:"id"`
	SessionID          SessionID           `json:"sessionId"`
	SourceKind         string              `json:"sourceKind" enum:"interface_transition,agent_switch,conversation_settings"`
	SourceID           string              `json:"sourceId"`
	PreviousActivation int64               `json:"previousActivation"`
	Configuration      WorkerConfiguration `json:"configuration"`
	Actor              RegistryActor       `json:"actor"`
	Reason             string              `json:"reason"`
	CreatedAt          time.Time           `json:"createdAt"`
}

// Validate requires a bounded source identity, trusted attribution and a sealed configuration.
func (e WorkerExecution) Validate() error {
	if strings.TrimSpace(e.ID) == "" || len(e.ID) > 200 || e.SessionID == "" || strings.TrimSpace(e.SourceID) == "" || len(e.SourceID) > 200 || e.PreviousActivation < 0 || e.CreatedAt.IsZero() {
		return fmt.Errorf("invalid worker execution identity")
	}
	switch e.SourceKind {
	case "interface_transition", "agent_switch", "conversation_settings":
	default:
		return fmt.Errorf("unknown worker execution source")
	}
	if err := (RegistryMutation{Actor: e.Actor, Reason: e.Reason}).Validate(); err != nil {
		return err
	}
	return e.Configuration.Validate()
}

// WorkerExecutionActivation is append-only controller/configuration provenance.
// An empty execution ID explicitly returns to the original launch snapshot.
type WorkerExecutionActivation struct {
	Sequence    int64     `json:"sequence"`
	SessionID   SessionID `json:"sessionId"`
	ExecutionID string    `json:"executionId,omitempty"`
	OperationID string    `json:"operationId"`
	Action      string    `json:"action" enum:"applied,rolled_back"`
	CreatedAt   time.Time `json:"createdAt"`
}
