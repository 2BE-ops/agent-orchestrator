package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// AgentManagerControllerToken identifies one durable native admission. It is
// trusted service context, never a public spawn option or an expiring liveness lease.
type AgentManagerControllerToken struct {
	ID                   string    `json:"id"`
	ProjectID            ProjectID `json:"projectId"`
	ConfigurationVersion int64     `json:"configurationVersion"`
}

// AgentManagerController retains admission and confirmed release facts. Its
// associated session/configuration is stored atomically before process effects.
type AgentManagerController struct {
	AgentManagerControllerToken
	Actor         AdaptiveActor `json:"actor"`
	Reason        string        `json:"reason"`
	CreatedAt     time.Time     `json:"createdAt"`
	ReleasedAt    *time.Time    `json:"releasedAt,omitempty"`
	ReleaseReason string        `json:"releaseReason,omitempty"`
}

// AgentManagerControllerReservation is validated service admission, not LLM output.
type AgentManagerControllerReservation struct {
	AgentManagerControllerToken
	Actor  AdaptiveActor
	Reason string
	Now    time.Time
}

// Validate permits user/daemon admission only, under existing user governance.
func (r AgentManagerControllerReservation) Validate() error {
	for _, value := range []string{r.ID, string(r.ProjectID)} {
		if strings.TrimSpace(value) == "" || len(value) > 200 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return fmt.Errorf("invalid Manager controller identity")
		}
	}
	if r.ConfigurationVersion < 1 || r.ConfigurationVersion > 1000 || r.Now.IsZero() || (r.Actor.Kind != "USER" && r.Actor.Kind != "SYSTEM") || r.Actor.SessionID != "" {
		return fmt.Errorf("invalid Manager admission authority or version")
	}
	return (TaskMutation{Actor: r.Actor, Reason: r.Reason}).ValidatePlanning()
}

// AgentManagerControllerDispatch is immutable association with a native session.
type AgentManagerControllerDispatch struct {
	ControllerID      string    `json:"controllerId"`
	SessionID         SessionID `json:"sessionId"`
	ConfigurationHash string    `json:"configurationHash"`
	CreatedAt         time.Time `json:"createdAt"`
}

// AgentManagerExecutionOperation reserves a target native generation before I/O.
// An unresolved operation prohibits replacement even if the session looks dead.
type AgentManagerExecutionOperation struct {
	ID           string                 `json:"id"`
	ControllerID string                 `json:"controllerId"`
	SessionID    SessionID              `json:"sessionId"`
	SourceOwner  SessionControllerOwner `json:"-"`
	Kind         string                 `json:"kind" enum:"dispatch,restore"`
	CreatedAt    time.Time              `json:"createdAt"`
}

// AgentManagerExecutionResolution has the same observed-owner evidence contract
// as task execution: the lifecycle service must confirm connection/termination.
// Unknown or failed native probes do not supply a resolution.
type AgentManagerExecutionResolution TaskExecutionResolution

// AgentManagerControllerRelease carries trusted, lifecycle-confirmed termination.
// An unseeded reservation needs no owner because native effects require a seed.
type AgentManagerControllerRelease struct {
	Token         AgentManagerControllerToken
	ObservedOwner *SessionControllerOwner
	Reason        string
	Now           time.Time
}
