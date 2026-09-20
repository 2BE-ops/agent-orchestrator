package domain

import (
	"fmt"
	"strings"
	"time"
)

// ProjectControlState is one project's adaptive control state. Running admits
// new worker launches; every other state fences them deterministically.
type ProjectControlState string

const (
	// ProjectRunning admits new admissions. The default when no control row
	// exists, so a project that never used controls behaves exactly as before.
	ProjectRunning ProjectControlState = "running"
	// ProjectPaused fences new admissions immediately; current tasks continue
	// and pending work is retained.
	ProjectPaused ProjectControlState = "paused"
	// ProjectDraining fences admissions and lets current attempts finish; the
	// effective state becomes paused once no active attempt remains.
	ProjectDraining ProjectControlState = "draining"
	// ProjectStopped fences admissions and follow-up dispatch, including the
	// adaptive controllers' launches.
	ProjectStopped ProjectControlState = "stopped"
)

// ValidControlState reports whether the state is one of the known states.
func ValidControlState(state ProjectControlState) bool {
	switch state {
	case ProjectRunning, ProjectPaused, ProjectDraining, ProjectStopped:
		return true
	default:
		return false
	}
}

// ValidProjectControlTransition is the control state machine. An identical
// transition is the idempotent no-op every control caller may retry.
func ValidProjectControlTransition(from, to ProjectControlState) bool {
	if !ValidControlState(from) || !ValidControlState(to) {
		return false
	}
	if from == to {
		return true
	}
	switch from {
	case ProjectRunning:
		return to == ProjectPaused || to == ProjectDraining || to == ProjectStopped
	case ProjectPaused:
		return to == ProjectRunning || to == ProjectStopped
	case ProjectDraining:
		// A drain may be resumed before it completes, or land in its two
		// terminal rests; only running also re-enters from stopped.
		return to == ProjectRunning || to == ProjectPaused || to == ProjectStopped
	case ProjectStopped:
		return to == ProjectRunning
	default:
		return false
	}
}

// ProjectControl is the durable adaptive-control record of one project. The
// row exists only after the first explicit control; absence means running.
type ProjectControl struct {
	ProjectID ProjectID           `json:"projectId"`
	State     ProjectControlState `json:"state" enum:"running,paused,draining,stopped"`
	Actor     AdaptiveActor       `json:"actor"`
	Reason    string              `json:"reason"`
	UpdatedAt time.Time           `json:"updatedAt"`
}

// Validate bounds the actor and reason of a control change. Only the user and
// deterministic system code may steer project controls; an orchestrator, agent
// manager or worker never pauses or stops its own project.
func (c ProjectControl) Validate() error {
	if c.ProjectID == "" || !ValidControlState(c.State) {
		return fmt.Errorf("invalid project control state")
	}
	if c.Actor.Kind != "USER" && c.Actor.Kind != "SYSTEM" {
		return fmt.Errorf("only the user or system can change project controls")
	}
	if strings.TrimSpace(c.Actor.ID) == "" || len(c.Actor.ID) > 200 || strings.ContainsRune(c.Actor.ID, 0) || len(c.Actor.SessionID) > 200 {
		return fmt.Errorf("invalid project control actor")
	}
	if strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 2000 || strings.ContainsRune(c.Reason, 0) {
		return fmt.Errorf("invalid project control reason")
	}
	if c.UpdatedAt.IsZero() {
		return fmt.Errorf("project control requires a timestamp")
	}
	return nil
}

// ProjectControlView couples the stored control with the derived effective
// state and the durable facts it derives from. Draining reports paused once no
// active attempt remains; nothing is materialized at read time.
type ProjectControlView struct {
	Control        ProjectControl      `json:"control"`
	EffectiveState ProjectControlState `json:"effectiveState" enum:"running,paused,draining,stopped"`
	ActiveAttempts int64               `json:"activeAttempts"`
}

// TaskNeedsHuman is a structured, resolvable request for human input. It
// blocks new attempts on its task and its descendants; every other eligible
// branch remains schedulable.
type TaskNeedsHuman struct {
	ID         string                    `json:"id"`
	TaskID     string                    `json:"taskId"`
	ProjectID  ProjectID                 `json:"projectId"`
	ReasonCode string                    `json:"reasonCode" enum:"credential_missing,approval_required,ambiguous_intent,provider_unavailable,recovery_inconclusive"`
	Detail     string                    `json:"detail"`
	Actor      AdaptiveActor             `json:"actor"`
	CreatedAt  time.Time                 `json:"createdAt"`
	Resolution *TaskNeedsHumanResolution `json:"resolution,omitempty"`
}

// TaskNeedsHumanResolution closes one request. Resolving records the human
// decision; it never rewrites why the request was raised.
type TaskNeedsHumanResolution struct {
	Resolution string        `json:"resolution"`
	Actor      AdaptiveActor `json:"actor"`
	ResolvedAt time.Time     `json:"resolvedAt"`
}

// Validate bounds a raised request before it can reach storage.
func (n TaskNeedsHuman) Validate() error {
	if strings.TrimSpace(n.ID) == "" || len(n.ID) > 200 || strings.ContainsRune(n.ID, 0) || strings.TrimSpace(n.TaskID) == "" || len(n.TaskID) > 200 || strings.ContainsRune(n.TaskID, 0) || n.ProjectID == "" {
		return fmt.Errorf("invalid needs human identity")
	}
	switch n.ReasonCode {
	case "credential_missing", "approval_required", "ambiguous_intent", "provider_unavailable", "recovery_inconclusive":
	default:
		return fmt.Errorf("unknown needs human reason code")
	}
	if strings.TrimSpace(n.Detail) == "" || len(n.Detail) > 4000 || strings.ContainsRune(n.Detail, 0) {
		return fmt.Errorf("invalid needs human detail")
	}
	if err := validateAdaptiveActor(n.Actor); err != nil {
		return err
	}
	if n.CreatedAt.IsZero() {
		return fmt.Errorf("needs human requires a timestamp")
	}
	if n.Resolution != nil {
		if err := n.Resolution.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate bounds a resolution.
func (r TaskNeedsHumanResolution) Validate() error {
	if strings.TrimSpace(r.Resolution) == "" || len(r.Resolution) > 4000 || strings.ContainsRune(r.Resolution, 0) {
		return fmt.Errorf("invalid needs human resolution")
	}
	if err := validateAdaptiveActor(r.Actor); err != nil {
		return err
	}
	if r.ResolvedAt.IsZero() {
		return fmt.Errorf("needs human resolution requires a timestamp")
	}
	return nil
}

func validateAdaptiveActor(actor AdaptiveActor) error {
	switch actor.Kind {
	case "USER", "ORCHESTRATOR", "AGENT_MANAGER", "WORKER", "SYSTEM":
	default:
		return fmt.Errorf("unknown adaptive actor kind")
	}
	if strings.TrimSpace(actor.ID) == "" || len(actor.ID) > 200 || strings.ContainsRune(actor.ID, 0) || len(actor.SessionID) > 200 || strings.ContainsRune(string(actor.SessionID), 0) {
		return fmt.Errorf("invalid adaptive actor")
	}
	if actor.Kind == "ORCHESTRATOR" && actor.SessionID == "" {
		return fmt.Errorf("orchestrator provenance requires its session identity")
	}
	return nil
}

// ProjectWorkCancellation is one deterministic bulk cancel instruction.
type ProjectWorkCancellation struct {
	ProjectID ProjectID
	Scope     string        `json:"scope" enum:"pending,all"`
	Actor     AdaptiveActor `json:"actor"`
	Reason    string        `json:"reason"`
	Now       time.Time
}

// Validate bounds the bulk cancel before it reaches storage. Pending cancels
// unleased work; all additionally marks leased work cancelling so lifecycle
// cleanup and explicit termination can proceed.
func (c ProjectWorkCancellation) Validate() error {
	if c.ProjectID == "" || (c.Scope != "pending" && c.Scope != "all") {
		return fmt.Errorf("invalid work cancellation scope")
	}
	if c.Actor.Kind != "USER" && c.Actor.Kind != "SYSTEM" {
		return fmt.Errorf("only the user or system can cancel project work")
	}
	if strings.TrimSpace(c.Actor.ID) == "" || len(c.Actor.ID) > 200 || strings.ContainsRune(c.Actor.ID, 0) || len(c.Actor.SessionID) > 200 {
		return fmt.Errorf("invalid work cancellation actor")
	}
	if strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 2000 || strings.ContainsRune(c.Reason, 0) {
		return fmt.Errorf("invalid work cancellation reason")
	}
	if c.Now.IsZero() {
		return fmt.Errorf("work cancellation requires a timestamp")
	}
	return nil
}

// ProjectWorkCancellationResult reports exactly what one bulk cancel did.
// Retained tasks name live work that keeps its admission; a cancel-all that
// could not mark everything reports the survivors instead of success.
type ProjectWorkCancellationResult struct {
	Scope     string   `json:"scope"`
	Cancelled []string `json:"cancelled"`
	Retained  []string `json:"retained"`
}
