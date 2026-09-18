package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// TaskMessageDefinition is coordination data, never a planning mutation or an
// instruction to execute commands. Replies retain a project-scoped thread ID.
type TaskMessageDefinition struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Kind          string              `json:"kind" enum:"finding,question,answer,blocker,handoff,interface_contract,review_request,dependency_update"`
	TargetTaskID  string              `json:"targetTaskId"`
	Subject       string              `json:"subject"`
	Body          string              `json:"body"`
	CorrelationID string              `json:"correlationId"`
	ReplyToID     string              `json:"replyToId,omitempty"`
	ResultID      string              `json:"resultId,omitempty"`
	Interface     *TaskInterfaceClaim `json:"interface,omitempty"`
}

func messageIdentity(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 200 && strings.IndexFunc(value, unicode.IsControl) < 0
}

// Validate bounds inert coordination content and its typed references.
func (d TaskMessageDefinition) Validate() error {
	if d.SchemaVersion != 1 || !messageIdentity(d.TargetTaskID) || !messageIdentity(d.CorrelationID) || !resultText(d.Subject, 300, true) || !resultText(d.Body, 16000, true) {
		return fmt.Errorf("invalid schema v1 task message")
	}
	switch d.Kind {
	case "finding", "question", "answer", "blocker", "handoff", "interface_contract", "review_request", "dependency_update":
	default:
		return fmt.Errorf("unknown task message kind")
	}
	for _, optional := range []string{d.ReplyToID, d.ResultID} {
		if optional != "" && !messageIdentity(optional) {
			return fmt.Errorf("invalid message reference")
		}
	}
	if d.Kind == "answer" && d.ReplyToID == "" {
		return fmt.Errorf("answer requires a question reference")
	}
	if (d.Kind == "interface_contract") != (d.Interface != nil) {
		return fmt.Errorf("interface_contract requires exactly one structured interface")
	}
	if d.Interface != nil {
		if err := d.Interface.Validate(); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(encoded) > 32<<10 {
		return fmt.Errorf("task message exceeds 32 KiB")
	}
	return nil
}

// TaskMessage is immutable evidence in the shared project timeline. Sequence is
// a database cursor; IDs and hashes survive native session termination.
type TaskMessage struct {
	ID                    string                `json:"id"`
	Sequence              int64                 `json:"sequence"`
	ProjectID             ProjectID             `json:"projectId"`
	TaskID                string                `json:"taskId"`
	AttemptID             string                `json:"attemptId"`
	SessionID             SessionID             `json:"sessionId"`
	NativeGeneration      string                `json:"nativeGeneration"`
	TaskRevision          int64                 `json:"taskRevision"`
	CriteriaVersion       int64                 `json:"criteriaVersion"`
	ConfigurationHash     string                `json:"configurationHash"`
	ConfigurationSequence int64                 `json:"configurationSequence"`
	ContextHash           string                `json:"contextHash"`
	Definition            TaskMessageDefinition `json:"definition"`
	ContentHash           string                `json:"contentHash"`
	CreatedAt             time.Time             `json:"createdAt"`
}

// TaskMessageSubmission contains daemon-derived attribution and a native retry
// key. The API never accepts SourceOwner or a claimed actor from worker JSON.
type TaskMessageSubmission struct {
	ID                 string
	AttemptID          string
	SessionID          SessionID
	SourceOwner        SessionControllerOwner
	ExpectedActivation int64
	IdempotencyKey     string
	Definition         TaskMessageDefinition
}

// Validate checks identities before storage derives authority and provenance.
func (s TaskMessageSubmission) Validate() error {
	for _, value := range []string{s.ID, s.AttemptID, string(s.SessionID), s.IdempotencyKey} {
		if !messageIdentity(value) {
			return fmt.Errorf("invalid message submission identity")
		}
	}
	if s.ExpectedActivation < 0 {
		return fmt.Errorf("invalid configuration activation")
	}
	return s.Definition.Validate()
}

// TaskMessageDelivery records an exclusive native send attempt. handed_off is a
// transport acknowledgement, not proof that an agent read or acted on the data.
// Only proven not_sent attempts permit a new claim; uncertain work is retained.
type TaskMessageDelivery struct {
	ID              string                 `json:"id"`
	MessageID       string                 `json:"messageId"`
	Number          int64                  `json:"number"`
	TargetAttemptID string                 `json:"targetAttemptId"`
	SessionID       SessionID              `json:"sessionId"`
	Owner           SessionControllerOwner `json:"owner"`
	DeliveryKey     string                 `json:"deliveryKey"`
	State           string                 `json:"state" enum:"dispatching,handed_off,not_sent,uncertain"`
	Reason          string                 `json:"reason"`
	CreatedAt       time.Time              `json:"createdAt"`
	UpdatedAt       time.Time              `json:"updatedAt"`
}

// TaskMessageDeliveryResolution records one observed transport outcome.
type TaskMessageDeliveryResolution struct {
	ID     string
	State  string
	Reason string
}

// Validate keeps audit reasons bounded including the message/delivery IDs.
func (r TaskMessageDeliveryResolution) Validate() error {
	if !messageIdentity(r.ID) || !resultText(r.Reason, 1500, true) || (r.State != "handed_off" && r.State != "not_sent" && r.State != "uncertain") {
		return fmt.Errorf("invalid message delivery resolution")
	}
	return nil
}
