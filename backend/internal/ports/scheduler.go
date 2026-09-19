package ports

import "errors"

// Scheduler admission is deterministic storage-level enforcement: an LLM never
// decides whether a worker may launch, and no caller can spend capacity the
// durable session state does not show. Both limits are checked inside the same
// write transaction that creates the session.
var (
	// ErrSchedulerWorkerLimit reports the daemon-wide concurrent worker cap.
	ErrSchedulerWorkerLimit = errors.New("global concurrent worker limit reached")
	// ErrSchedulerAgentTypeLimit reports one Agent Type's maxParallelWorkers.
	ErrSchedulerAgentTypeLimit = errors.New("agent type concurrent worker limit reached")
)
