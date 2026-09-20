package domain

import (
	"fmt"
	"time"
)

// DryRunActionLimit bounds one simulated plan. A dry-run reuses the exact
// sealed planning actions the orchestrator protocol submits, so a native plan
// can be rehearsed before it is executed.
const DryRunActionLimit = 64

// DryRunRequest simulates an autonomous plan. Validation, selection and
// admission run against current durable state with every write and process
// launch disabled: no worktree is created, no harness is installed, the
// repository is untouched, and registry/task state never changes.
type DryRunRequest struct {
	Actions []OrchestratorPlanAction `json:"actions"`
}

// Validate bounds the simulated plan and each of its actions. Actions apply
// sequentially, each against its own revision fence, exactly as execution
// would; the verdicts report where the sequence would stop applying cleanly.
func (r DryRunRequest) Validate() error {
	if len(r.Actions) < 1 || len(r.Actions) > DryRunActionLimit {
		return fmt.Errorf("a dry run requires 1 to %d actions", DryRunActionLimit)
	}
	for _, action := range r.Actions {
		if err := action.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// DryRunActionVerdict reports what one simulated action would do now.
// Findings name every reason the action would not apply cleanly; an empty
// finding list with WouldApply true is the honest all-clear.
type DryRunActionVerdict struct {
	Index      int      `json:"index"`
	Action     string   `json:"action"`
	TaskID     string   `json:"taskId,omitempty"`
	WouldApply bool     `json:"wouldApply"`
	Findings   []string `json:"findings"`
	Notes      []string `json:"notes,omitempty"`
}

// DryRunSelectionVerdict reports whether one simulated action's requested
// worker resolves against the registry, and at which exact version.
type DryRunSelectionVerdict struct {
	ActionIndex      int      `json:"actionIndex"`
	AgentTypeID      string   `json:"agentTypeId"`
	RequestedVersion int64    `json:"requestedVersion"`
	ActiveVersion    int64    `json:"activeVersion"`
	PinsActive       bool     `json:"pinsActive"`
	Resolves         bool     `json:"resolves"`
	Findings         []string `json:"findings"`
	Notes            []string `json:"notes,omitempty"`
}

// DryRunAdmission reports the deterministic admission facts a dry run would
// face right now. Headroom is derived from durable session rows and the
// daemon-wide cap; it never fabricates a cost or duration estimate.
type DryRunAdmission struct {
	ControlState         string `json:"controlState" enum:"running,paused,draining,stopped"`
	ActiveWorkers        int64  `json:"activeWorkers"`
	MaxConcurrentWorkers int    `json:"maxConcurrentWorkers"`
	Headroom             int64  `json:"headroom"`
	WouldAdmitDispatch   bool   `json:"wouldAdmitDispatch"`
}

// DryRunVerdict is the complete result of one simulated plan.
type DryRunVerdict struct {
	ProjectID    ProjectID                `json:"projectId"`
	GraphValid   bool                     `json:"graphValid"`
	Actions      []DryRunActionVerdict    `json:"actions"`
	Selections   []DryRunSelectionVerdict `json:"selections"`
	Admission    DryRunAdmission          `json:"admission"`
	CostEstimate string                   `json:"costEstimate"`
	SimulatedAt  time.Time                `json:"simulatedAt"`
}

// DryRunCostUnknown is the only cost estimate a dry run may report: it
// launches no worker and observes no model call, so it reports the unknown
// instead of inventing precision.
const DryRunCostUnknown = "unknown"
