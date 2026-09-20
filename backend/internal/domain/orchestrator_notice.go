package domain

import (
	"fmt"
	"time"
)

// OrchestratorNotice journals one derived terminal task fact pushed to the
// project's live orchestrator session. It is delivery bookkeeping over durable
// facts, never a second source of task status: the fact and its anchor are
// re-derived at read time by the feedback projection.
type OrchestratorNotice struct {
	ID         string     `json:"id"`
	ProjectID  ProjectID  `json:"projectId"`
	TaskID     string     `json:"taskId"`
	Fact       string     `json:"fact" enum:"completed,failed,cancelled"`
	Anchor     string     `json:"anchor"`
	Revision   int64      `json:"revision"`
	Detail     string     `json:"detail"`
	State      string     `json:"state" enum:"pending,handed_off,not_sent,uncertain"`
	Reason     string     `json:"reason"`
	CreatedAt  time.Time  `json:"createdAt"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
}

// OrchestratorNoticeResolution settles one pending notice exactly once. Only a
// proven no-write outcome is "not_sent"; transport ambiguity stays "uncertain"
// and is never retried by a timeout or probe.
type OrchestratorNoticeResolution struct {
	ID         string    `json:"id"`
	State      string    `json:"state" enum:"handed_off,not_sent,uncertain"`
	Reason     string    `json:"reason"`
	ResolvedAt time.Time `json:"resolvedAt"`
}

// ValidateOrchestratorNoticeFact bounds which feedback states are push-worthy.
func ValidateOrchestratorNoticeFact(fact string) error {
	switch fact {
	case "completed", "failed", "cancelled":
		return nil
	default:
		return fmt.Errorf("invalid orchestrator notice fact %q", fact)
	}
}

// Validate checks a new pending notice. Terminal states arrive only through a
// resolution, so a notice with a state or resolved timestamp set is refused.
func (n OrchestratorNotice) Validate() error {
	if err := ValidateOrchestratorNoticeFact(n.Fact); err != nil {
		return err
	}
	for _, id := range []string{n.ID, string(n.ProjectID), n.TaskID} {
		if !messageIdentity(id) {
			return fmt.Errorf("invalid orchestrator notice identity")
		}
	}
	if n.State != "pending" || n.ResolvedAt != nil {
		return fmt.Errorf("orchestrator notices start pending and are settled by resolution")
	}
	if n.Revision < 1 || len(n.Anchor) < 1 || len(n.Anchor) > 200 {
		return fmt.Errorf("invalid orchestrator notice anchor")
	}
	if n.CreatedAt.IsZero() {
		return fmt.Errorf("invalid orchestrator notice timestamp")
	}
	if size := len(n.Detail); size < 1 || size > 4096 {
		return fmt.Errorf("invalid orchestrator notice detail")
	}
	if size := len(n.Reason); size > 1024 {
		return fmt.Errorf("invalid orchestrator notice reason")
	}
	return nil
}

// Validate checks a settlement against the same boundary.
func (r OrchestratorNoticeResolution) Validate() error {
	if !messageIdentity(r.ID) {
		return fmt.Errorf("invalid orchestrator notice identity")
	}
	switch r.State {
	case "handed_off", "not_sent", "uncertain":
	default:
		return fmt.Errorf("invalid orchestrator notice resolution state")
	}
	if r.ResolvedAt.IsZero() {
		return fmt.Errorf("invalid orchestrator notice resolution timestamp")
	}
	if size := len(r.Reason); size < 1 || size > 1024 {
		return fmt.Errorf("invalid orchestrator notice resolution reason")
	}
	return nil
}
