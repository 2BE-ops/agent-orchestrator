package domain

import (
	"fmt"
	"slices"
	"time"
)

// OutcomeAttributionLimit bounds one complete attribution cohort exactly like a
// performance summary. Wider windows must be narrowed; summaries never drop
// members silently.
const OutcomeAttributionLimit = 1000

// OutcomeAttributionQuery selects one complete decision/receipt cohort.
type OutcomeAttributionQuery struct {
	ProjectID ProjectID
	From      time.Time
	To        time.Time
}

// Validate bounds the attribution window with the performance cohort rules.
func (q OutcomeAttributionQuery) Validate() error {
	if !resultText(string(q.ProjectID), 200, true) || q.From.IsZero() || q.To.IsZero() || !q.From.Before(q.To) || q.To.Sub(q.From) > 366*24*time.Hour {
		return fmt.Errorf("attribution requires a project and a decision window up to 366 days")
	}
	return nil
}

// TaskOutcomeTotals counts the read-time fate of attributed tasks by the same
// states the loop feedback reports. Cancelling counts separately from cancelled
// so a mid-cancellation snapshot cannot read as finished work.
type TaskOutcomeTotals struct {
	Completed  int64 `json:"completed"`
	Failed     int64 `json:"failed"`
	Cancelled  int64 `json:"cancelled"`
	Cancelling int64 `json:"cancelling"`
	Working    int64 `json:"working"`
	Pending    int64 `json:"pending"`
}

func (t *TaskOutcomeTotals) add(state string) error {
	switch state {
	case "completed":
		t.Completed++
	case "failed":
		t.Failed++
	case "cancelled":
		t.Cancelled++
	case "cancelling":
		t.Cancelling++
	case "working":
		t.Working++
	case "pending":
		t.Pending++
	default:
		return fmt.Errorf("unknown attributed task state")
	}
	return nil
}

// ManagerRoutingOutcome couples one routing request's chosen decision — the
// accepted selection when present, otherwise its latest rejection — with the
// read-time fate of the task it routed. State, attempts and evidence identity
// are derived from durable facts on every read; nothing is stored.
type ManagerRoutingOutcome struct {
	Sequence     int64                `json:"sequence"`
	DecisionID   string               `json:"decisionId"`
	RequestID    string               `json:"requestId"`
	TaskID       string               `json:"taskId"`
	TaskTitle    string               `json:"taskTitle"`
	Revision     int64                `json:"revision"`
	Outcome      string               `json:"outcome" enum:"accepted,rejected"`
	Optimization string               `json:"optimization" enum:"quality,balanced,speed,usage"`
	AgentType    *WorkerDefinitionRef `json:"agentType,omitempty"`
	State        string               `json:"state" enum:"pending,working,completed,failed,cancelling,cancelled"`
	Reason       string               `json:"reason"`
	Attempts     int64                `json:"attempts"`
	ResultID     string               `json:"resultId,omitempty"`
	EvaluationID string               `json:"evaluationId,omitempty"`
	DecidedAt    time.Time            `json:"decidedAt"`
}

// ManagerRoutingTotals keeps the decision-maker's counts. Task-state and
// attempt counters cover only accepted (routed) work, so rejections never
// inflate the work the Manager actually sent.
type ManagerRoutingTotals struct {
	Decisions          int64             `json:"decisions"`
	Accepted           int64             `json:"accepted"`
	Rejected           int64             `json:"rejected"`
	RoutedTaskStates   TaskOutcomeTotals `json:"routedTaskStates"`
	RoutedTaskAttempts int64             `json:"routedTaskAttempts"`
}

// ManagerRoutingTypeGroup attributes accepted routing per exact Agent Type
// version. Open folds working and pending; cancelled folds cancelling.
type ManagerRoutingTypeGroup struct {
	AgentTypeID string `json:"agentTypeId"`
	Name        string `json:"name"`
	Version     int64  `json:"version"`
	Routed      int64  `json:"routed"`
	Completed   int64  `json:"completed"`
	Failed      int64  `json:"failed"`
	Cancelled   int64  `json:"cancelled"`
	Open        int64  `json:"open"`
}

// ManagerRoutingSummary aggregates one complete routing cohort. Counts are
// retained instead of percentages so small samples stay visible.
type ManagerRoutingSummary struct {
	From       time.Time                 `json:"from"`
	To         time.Time                 `json:"to"`
	ObservedAt time.Time                 `json:"observedAt"`
	Totals     ManagerRoutingTotals      `json:"totals"`
	Types      []ManagerRoutingTypeGroup `json:"types"`
}

// SummarizeManagerRouting aggregates a complete bounded routing cohort. It
// refuses duplicate request attribution, accepted decisions without their
// selected type and arithmetic overflow; it never invents counters.
func SummarizeManagerRouting(query OutcomeAttributionQuery, outcomes []ManagerRoutingOutcome, observedAt time.Time) (ManagerRoutingSummary, error) {
	result := ManagerRoutingSummary{From: query.From, To: query.To, ObservedAt: observedAt, Types: []ManagerRoutingTypeGroup{}}
	if err := query.Validate(); err != nil {
		return ManagerRoutingSummary{}, err
	}
	if len(outcomes) > OutcomeAttributionLimit {
		return ManagerRoutingSummary{}, fmt.Errorf("routing cohort exceeds %d decisions", OutcomeAttributionLimit)
	}
	seen := map[string]bool{}
	groups := map[SkillVersionRef]*ManagerRoutingTypeGroup{}
	for _, item := range outcomes {
		if item.DecisionID == "" || item.RequestID == "" || item.TaskID == "" || seen[item.RequestID] {
			return ManagerRoutingSummary{}, fmt.Errorf("duplicate or incomplete routing attribution")
		}
		seen[item.RequestID] = true
		result.Totals.Decisions++
		switch item.Outcome {
		case "accepted":
			result.Totals.Accepted++
			if item.AgentType == nil {
				return ManagerRoutingSummary{}, fmt.Errorf("accepted routing attribution lacks its selected Agent Type")
			}
			if err := result.Totals.RoutedTaskStates.add(item.State); err != nil {
				return ManagerRoutingSummary{}, err
			}
			if err := addPerformanceCounter(&result.Totals.RoutedTaskAttempts, item.Attempts); err != nil {
				return ManagerRoutingSummary{}, err
			}
			key := SkillVersionRef{ID: item.AgentType.ID, Version: item.AgentType.Version}
			group := groups[key]
			if group == nil {
				group = &ManagerRoutingTypeGroup{AgentTypeID: item.AgentType.ID, Name: item.AgentType.Name, Version: item.AgentType.Version}
				groups[key] = group
			}
			group.Routed++
			switch item.State {
			case "completed":
				group.Completed++
			case "failed":
				group.Failed++
			case "cancelled", "cancelling":
				group.Cancelled++
			case "working", "pending":
				group.Open++
			}
		case "rejected":
			result.Totals.Rejected++
		default:
			return ManagerRoutingSummary{}, fmt.Errorf("unknown routing decision outcome")
		}
	}
	for _, group := range groups {
		result.Types = append(result.Types, *group)
	}
	slices.SortFunc(result.Types, func(a, b ManagerRoutingTypeGroup) int {
		if a.AgentTypeID < b.AgentTypeID {
			return -1
		}
		if a.AgentTypeID > b.AgentTypeID {
			return 1
		}
		if a.Version < b.Version {
			return -1
		}
		if a.Version > b.Version {
			return 1
		}
		return 0
	})
	return result, nil
}

// OrchestratorPlanningOutcome couples one sealed planning receipt with the
// read-time fate of the task it created (create_task) or revised/refroze
// (revise_task, freeze_criteria). Derived on every read; nothing is stored.
type OrchestratorPlanningOutcome struct {
	ReceiptID    string    `json:"receiptId"`
	Action       string    `json:"action" enum:"create_task,revise_task,freeze_criteria"`
	TaskID       string    `json:"taskId"`
	TaskTitle    string    `json:"taskTitle"`
	Revision     int64     `json:"revision"`
	State        string    `json:"state" enum:"pending,working,completed,failed,cancelling,cancelled"`
	Reason       string    `json:"reason"`
	Attempts     int64     `json:"attempts"`
	ResultID     string    `json:"resultId,omitempty"`
	EvaluationID string    `json:"evaluationId,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// OrchestratorPlanningTotals counts planning actions and the fate of tasks the
// orchestrator minted. Task-state and attempt counters cover one entry per
// unique created task; revision-only receipts never inflate them.
type OrchestratorPlanningTotals struct {
	Receipts            int64             `json:"receipts"`
	CreateTask          int64             `json:"createTask"`
	ReviseTask          int64             `json:"reviseTask"`
	FreezeCriteria      int64             `json:"freezeCriteria"`
	PlannedTaskStates   TaskOutcomeTotals `json:"plannedTaskStates"`
	PlannedTaskAttempts int64             `json:"plannedTaskAttempts"`
}

// OrchestratorPlanningSummary aggregates one complete planning cohort.
type OrchestratorPlanningSummary struct {
	From       time.Time                  `json:"from"`
	To         time.Time                  `json:"to"`
	ObservedAt time.Time                  `json:"observedAt"`
	Totals     OrchestratorPlanningTotals `json:"totals"`
}

// SummarizeOrchestratorPlanning aggregates a complete bounded planning cohort.
// Each created task counts exactly once; duplicate creation attribution and
// arithmetic overflow are refused.
func SummarizeOrchestratorPlanning(query OutcomeAttributionQuery, outcomes []OrchestratorPlanningOutcome, observedAt time.Time) (OrchestratorPlanningSummary, error) {
	result := OrchestratorPlanningSummary{From: query.From, To: query.To, ObservedAt: observedAt}
	if err := query.Validate(); err != nil {
		return OrchestratorPlanningSummary{}, err
	}
	if len(outcomes) > OutcomeAttributionLimit {
		return OrchestratorPlanningSummary{}, fmt.Errorf("planning cohort exceeds %d receipts", OutcomeAttributionLimit)
	}
	created := map[string]bool{}
	for _, item := range outcomes {
		if item.ReceiptID == "" || item.TaskID == "" {
			return OrchestratorPlanningSummary{}, fmt.Errorf("incomplete planning attribution")
		}
		result.Totals.Receipts++
		switch item.Action {
		case "create_task":
			result.Totals.CreateTask++
			if created[item.TaskID] {
				return OrchestratorPlanningSummary{}, fmt.Errorf("duplicate created-task planning attribution")
			}
			created[item.TaskID] = true
			if err := result.Totals.PlannedTaskStates.add(item.State); err != nil {
				return OrchestratorPlanningSummary{}, err
			}
			if err := addPerformanceCounter(&result.Totals.PlannedTaskAttempts, item.Attempts); err != nil {
				return OrchestratorPlanningSummary{}, err
			}
		case "revise_task":
			result.Totals.ReviseTask++
		case "freeze_criteria":
			result.Totals.FreezeCriteria++
		default:
			return OrchestratorPlanningSummary{}, fmt.Errorf("unknown planning action")
		}
	}
	return result, nil
}
