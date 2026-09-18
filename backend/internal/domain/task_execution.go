package domain

import "time"

// TaskExecutionOperation reserves the native side-effect window. An unresolved
// operation survives daemon failure and prevents exclusive lease reassignment.
type TaskExecutionOperation struct {
	// ID is also the reserved native target generation. Launch integration must
	// pass it to the existing native controller/supervisor so recovery can find
	// a process that started before its final session metadata was committed.
	ID          string                 `json:"id"`
	SessionID   SessionID              `json:"sessionId"`
	Lease       TaskLeaseToken         `json:"-"`
	SourceOwner SessionControllerOwner `json:"-"`
	Kind        string                 `json:"kind" enum:"dispatch,restore"`
	CreatedAt   time.Time              `json:"createdAt"`
}

// TaskExecutionResolution is trusted lifecycle evidence. Storage verifies the
// exact observed owner; the caller must first confirm native connection or
// termination. Failed/unknown probes are never a resolution.
type TaskExecutionResolution struct {
	OperationID   string
	ObservedOwner SessionControllerOwner
	Outcome       string
	Reason        string
	CreatedAt     time.Time
}
