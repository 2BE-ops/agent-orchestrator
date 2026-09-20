package domain

import (
	"fmt"
	"strings"
	"time"
)

// TaskRevisionRef pins the exact dependency planning seen when work is reserved.
type TaskRevisionRef struct {
	TaskID      string `json:"taskId"`
	Revision    int64  `json:"revision"`
	ContentHash string `json:"contentHash"`
}

// TaskAttempt retains dispatch intent even if launch or its requesting daemon fails.
type TaskAttempt struct {
	ID              string            `json:"id"`
	TaskID          string            `json:"taskId"`
	TaskRevision    int64             `json:"taskRevision"`
	CriteriaVersion int64             `json:"criteriaVersion"`
	Number          int64             `json:"number"`
	LaunchIntentID  string            `json:"launchIntentId"`
	Dependencies    []TaskRevisionRef `json:"dependencies"`
	Actor           AdaptiveActor     `json:"actor"`
	Reason          string            `json:"reason"`
	CreatedAt       time.Time         `json:"createdAt"`
}

// TaskLeaseToken fences both attempt generation and scheduler incarnation.
// It is trusted service context and is not accepted from worker result bodies.
type TaskLeaseToken struct {
	AttemptID  string `json:"attemptId"`
	Generation int64  `json:"generation"`
	HolderID   string `json:"-"`
}

// TaskLease retains its exclusive reservation after timeout until reconciliation.
type TaskLease struct {
	TaskLeaseToken
	TaskID         string     `json:"taskId"`
	HeartbeatAt    time.Time  `json:"heartbeatAt"`
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
	ExpiresAt      time.Time  `json:"expiresAt"`
	ReleasedAt     *time.Time `json:"releasedAt,omitempty"`
	ReleaseReason  string     `json:"releaseReason,omitempty"`
}

// NeedsReconciliation distinguishes expired liveness from confirmed termination.
func (l TaskLease) NeedsReconciliation(now time.Time) bool {
	return l.ReleasedAt == nil && !now.Before(l.ExpiresAt)
}

// TaskReservation is scheduler-only input. Reserve does not launch a process.
type TaskReservation struct {
	ID             string
	TaskID         string
	LaunchIntentID string
	HolderID       string
	Mutation       TaskMutation
	Now            time.Time
	TTL            time.Duration
}

// Validate bounds reservation identity and the scheduler's heartbeat window.
func (r TaskReservation) Validate() error {
	for _, value := range []string{r.ID, r.TaskID, r.LaunchIntentID, r.HolderID} {
		if strings.TrimSpace(value) == "" || len(value) > 200 || strings.ContainsRune(value, 0) {
			return fmt.Errorf("invalid task reservation identity")
		}
	}
	if r.Now.IsZero() || r.TTL < time.Second || r.TTL > 5*time.Minute || r.Mutation.ExpectedRevision < 1 {
		return fmt.Errorf("invalid task reservation window or revision")
	}
	return r.Mutation.ValidatePlanning()
}

// TaskWorkerDispatch is immutable association with an existing AO session seed.
type TaskWorkerDispatch struct {
	AttemptID         string    `json:"attemptId"`
	SessionID         SessionID `json:"sessionId"`
	ConfigurationHash string    `json:"configurationHash"`
	CreatedAt         time.Time `json:"createdAt"`
}

// TaskLeaseRecovery carries trusted lifecycle evidence, never an expired probe.
// A service must confirm the native host is connected (adopt) or terminated
// (release) before supplying its observed owner. Storage rechecks that owner.
type TaskLeaseRecovery struct {
	Token         TaskLeaseToken
	SessionID     SessionID
	ObservedOwner *SessionControllerOwner
	NewHolderID   string
	Reason        string
	Now           time.Time
	TTL           time.Duration
}
