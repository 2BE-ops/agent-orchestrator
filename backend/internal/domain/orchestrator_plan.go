package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// OrchestratorPlanAction is one native planning decision. It reuses the task
// service's exact definitions; the orchestrator never bypasses graph or
// criteria validation.
type OrchestratorPlanAction struct {
	Action           string              `json:"action" enum:"create_task,revise_task,freeze_criteria"`
	TaskID           string              `json:"taskId,omitempty"`
	ExpectedRevision int64               `json:"expectedRevision"`
	Definition       *TaskDefinition     `json:"definition,omitempty"`
	Criteria         *AcceptanceCriteria `json:"criteria,omitempty"`
	Reason           string              `json:"reason"`
}

// Validate bounds one planning action before it can reach the task store.
func (a OrchestratorPlanAction) Validate() error {
	switch a.Action {
	case "create_task":
		if a.TaskID != "" {
			return fmt.Errorf("task identity is minted by AO, never claimed by native output")
		}
		if a.Definition == nil {
			return fmt.Errorf("create_task requires a task definition")
		}
		if err := a.Definition.Validate(); err != nil {
			return err
		}
	case "revise_task":
		if strings.TrimSpace(a.TaskID) == "" || len(a.TaskID) > 200 || strings.ContainsRune(a.TaskID, 0) {
			return fmt.Errorf("revise_task requires an existing task id")
		}
		if a.Definition == nil {
			return fmt.Errorf("revise_task requires a task definition")
		}
		if err := a.Definition.Validate(); err != nil {
			return err
		}
	case "freeze_criteria":
		if strings.TrimSpace(a.TaskID) == "" || len(a.TaskID) > 200 || strings.ContainsRune(a.TaskID, 0) {
			return fmt.Errorf("freeze_criteria requires an existing task id")
		}
		if a.Criteria == nil {
			return fmt.Errorf("freeze_criteria requires acceptance criteria")
		}
		if err := a.Criteria.Validate(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown orchestrator plan action")
	}
	if a.Criteria != nil && a.Action != "freeze_criteria" {
		return fmt.Errorf("criteria are frozen through freeze_criteria")
	}
	if strings.TrimSpace(a.Reason) == "" || len(a.Reason) > 2000 || strings.ContainsRune(a.Reason, 0) {
		return fmt.Errorf("a bounded non-empty planning reason is required")
	}
	if a.ExpectedRevision < 0 {
		return fmt.Errorf("expectedRevision must not be negative")
	}
	return nil
}

// OrchestratorPlanRequestHash seals the exact action a receipt applies to.
func OrchestratorPlanRequestHash(project ProjectID, sessionID SessionID, key string, action OrchestratorPlanAction) (string, error) {
	data, err := json.Marshal(struct {
		ProjectID ProjectID              `json:"projectId"`
		SessionID SessionID              `json:"sessionId"`
		Key       string                 `json:"idempotencyKey"`
		Action    OrchestratorPlanAction `json:"action"`
	}{project, sessionID, key, action})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// OrchestratorPlanOutcome seals one retained planning action: the exact
// request it answers and the durable effect it produced.
type OrchestratorPlanOutcome struct {
	ReceiptID       string    `json:"receiptId"`
	ProjectID       string    `json:"projectId"`
	SessionID       string    `json:"sessionId"`
	IdempotencyKey  string    `json:"idempotencyKey"`
	Action          string    `json:"action"`
	RequestHash     string    `json:"requestHash"`
	TaskID          string    `json:"taskId"`
	Revision        int64     `json:"revision"`
	CriteriaVersion int64     `json:"criteriaVersion"`
	CreatedAt       time.Time `json:"createdAt"`
}

// OrchestratorPlanReceipt retains one sealed native planning action.
type OrchestratorPlanReceipt struct {
	Outcome OrchestratorPlanOutcome `json:"outcome"`
}

// OrchestratorPlanSubmission is a native planning action with daemon-minted
// identity. Task identity for create_task is minted here, never claimed by
// native output.
type OrchestratorPlanSubmission struct {
	ReceiptID      string
	ProjectID      ProjectID
	SessionID      SessionID
	IdempotencyKey string
	Action         OrchestratorPlanAction
	NewTaskID      string
	Now            time.Time
}

// Validate bounds a submission before it reaches the task store transaction.
func (s OrchestratorPlanSubmission) Validate() error {
	if s.Now.IsZero() {
		return fmt.Errorf("planning action requires a timestamp")
	}
	if strings.TrimSpace(s.ReceiptID) == "" || len(s.ReceiptID) > 200 || strings.ContainsRune(s.ReceiptID, 0) || s.ProjectID == "" || s.SessionID == "" {
		return fmt.Errorf("invalid planning submission identity")
	}
	if strings.TrimSpace(s.IdempotencyKey) == "" || len(s.IdempotencyKey) > 200 || strings.ContainsRune(s.IdempotencyKey, 0) {
		return fmt.Errorf("a bounded non-empty idempotency key is required")
	}
	if err := s.Action.Validate(); err != nil {
		return err
	}
	if s.Action.Action == "create_task" && (strings.TrimSpace(s.NewTaskID) == "" || len(s.NewTaskID) > 200 || strings.ContainsRune(s.NewTaskID, 0)) {
		return fmt.Errorf("create_task requires a daemon-minted task identity")
	}
	if s.Action.Action != "create_task" && s.NewTaskID != "" {
		return fmt.Errorf("only create_task mints task identity")
	}
	if s.Action.Action == "create_task" && s.Action.ExpectedRevision != 0 {
		return fmt.Errorf("create_task cannot expect an existing revision")
	}
	return nil
}
