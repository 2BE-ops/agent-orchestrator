package domain

import (
	"fmt"
	"strings"
	"time"
)

// WorkerNativeOption records only an advertised native control's chosen value.
// It never stores provider endpoints, environment variables or credentials.
type WorkerNativeOption struct {
	ID      string `json:"id"`
	Select  string `json:"select,omitempty"`
	Boolean *bool  `json:"boolean,omitempty"`
}

// Equal compares values rather than the allocation of a boolean option.
func (o WorkerNativeOption) Equal(other WorkerNativeOption) bool {
	return o.ID == other.ID && o.Select == other.Select && ((o.Boolean == nil && other.Boolean == nil) || (o.Boolean != nil && other.Boolean != nil && *o.Boolean == *other.Boolean))
}

// ValidateWorkerNativeOptions bounds the provider-owned configuration snapshot.
func ValidateWorkerNativeOptions(options []WorkerNativeOption) error {
	if len(options) > 64 {
		return fmt.Errorf("native configuration exceeds 64 controls")
	}
	seen := map[string]bool{}
	for _, option := range options {
		if strings.TrimSpace(option.ID) == "" || len(option.ID) > 200 || len(option.Select) > 2000 || seen[option.ID] || (option.Boolean != nil && option.Select != "") {
			return fmt.Errorf("invalid native configuration control")
		}
		seen[option.ID] = true
	}
	return nil
}

// WorkerNativeChange is durable intent written before a native control mutation.
// Absence of a resolution means its outcome needs reconciliation, never success.
type WorkerNativeChange struct {
	ID                 string                 `json:"id"`
	SessionID          SessionID              `json:"sessionId"`
	ConversationID     string                 `json:"conversationId"`
	Owner              SessionControllerOwner `json:"-"`
	PreviousActivation int64                  `json:"previousActivation"`
	Previous           []WorkerNativeOption   `json:"previous"`
	Requested          WorkerNativeOption     `json:"requested"`
	CreatedAt          time.Time              `json:"createdAt"`
}

// Validate requires an attributable, bounded recovery record.
func (c WorkerNativeChange) Validate() error {
	if c.ID == "" || len(c.ID) > 200 || c.SessionID == "" || c.ConversationID == "" || c.PreviousActivation < 0 || c.CreatedAt.IsZero() || c.Owner.Mode != SessionModeChat {
		return fmt.Errorf("invalid native configuration change identity")
	}
	if err := ValidateWorkerNativeOptions(c.Previous); err != nil {
		return err
	}
	if err := ValidateWorkerNativeOptions([]WorkerNativeOption{c.Requested}); err != nil {
		return err
	}
	for _, option := range c.Previous {
		if option.ID == c.Requested.ID {
			return nil
		}
	}
	return fmt.Errorf("native configuration change lacks the previous control value")
}
