package domain

import "time"

// TaskIntent is a durable admission instruction, not a session display status.
// Version zero is the implicit run intent of a newly authored task.
type TaskIntent struct {
	TaskID       string        `json:"taskId"`
	Version      int64         `json:"version"`
	TaskRevision int64         `json:"taskRevision"`
	Intent       string        `json:"intent" enum:"run,cancel"`
	Actor        AdaptiveActor `json:"actor"`
	Reason       string        `json:"reason"`
	CreatedAt    time.Time     `json:"createdAt"`
}

// TaskIntentChange fences both planning and control history. Cancellation only
// requests lifecycle cleanup; it never proves that a worker has stopped.
type TaskIntentChange struct {
	Intent          string
	ExpectedVersion int64
	Mutation        TaskMutation
}
