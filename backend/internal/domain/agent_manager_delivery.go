package domain

import (
	"fmt"
	"time"
)

// AgentManagerDelivery is an exclusive native send claim. Only a proven not_sent
// observation permits another attempt; handed_off is not a routing decision.
type AgentManagerDelivery struct {
	ID               string                 `json:"id"`
	RequestID        string                 `json:"requestId"`
	ContextID        string                 `json:"contextId"`
	ControllerID     string                 `json:"controllerId"`
	Number           int64                  `json:"number"`
	SessionID        SessionID              `json:"sessionId"`
	Owner            SessionControllerOwner `json:"-"`
	NativeGeneration string                 `json:"nativeGeneration"`
	DeliveryKey      string                 `json:"deliveryKey"`
	State            string                 `json:"state" enum:"dispatching,handed_off,not_sent,uncertain"`
	Reason           string                 `json:"reason"`
	CreatedAt        time.Time              `json:"createdAt"`
	UpdatedAt        time.Time              `json:"updatedAt"`
}

// AgentManagerDeliveryResolution carries a transport observation, not permission
// to clear an uncertain composer or resend potentially received instructions.
type AgentManagerDeliveryResolution struct {
	ID     string
	State  string
	Reason string
}

// Validate bounds observations and permits only terminal transport facts.
func (r AgentManagerDeliveryResolution) Validate() error {
	if !messageIdentity(r.ID) || !resultText(r.Reason, 1500, true) || (r.State != "handed_off" && r.State != "not_sent" && r.State != "uncertain") {
		return fmt.Errorf("invalid Manager delivery resolution")
	}
	return nil
}
