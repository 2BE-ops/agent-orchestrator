package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// TaskPerformanceQuery selects an admission cohort. Outcomes and native usage
// are observed at read time; the time window is not an event-billing filter.
type TaskPerformanceQuery struct {
	ProjectID ProjectID
	From      time.Time
	To        time.Time
	After     string
	Limit     int
}

// Validate bounds the admission window, identity cursor and page size.
func (q TaskPerformanceQuery) Validate() error {
	if strings.IndexFunc(string(q.ProjectID), unicode.IsControl) >= 0 || strings.IndexFunc(q.After, unicode.IsControl) >= 0 {
		return fmt.Errorf("performance project and cursor cannot contain control characters")
	}
	if !resultText(string(q.ProjectID), 200, true) || q.From.IsZero() || q.To.IsZero() || !q.From.Before(q.To) || q.To.Sub(q.From) > 366*24*time.Hour || !resultText(q.After, 200, false) || q.Limit < 1 || q.Limit > 100 {
		return fmt.Errorf("performance requires a project, admission window up to 366 days, bounded cursor and limit 1 to 100")
	}
	return nil
}

// TaskPerformanceUsage retains session-wide native accounting. Nil counters
// mean unknown, not zero; priced cost is a lower bound when not all events are
// priced. It must not be assigned to one execution epoch in a mixed attempt.
type TaskPerformanceUsage struct {
	Scope                string `json:"scope"`
	Events               int64  `json:"events"`
	NativeReportedEvents int64  `json:"nativeReportedEvents"`
	EstimatedEvents      int64  `json:"estimatedEvents"`
	UnknownEvents        int64  `json:"unknownEvents"`
	InputTokens          *int64 `json:"inputTokens"`
	OutputTokens         *int64 `json:"outputTokens"`
	PricedEvents         int64  `json:"pricedEvents"`
	PricedCostNanos      int64  `json:"pricedCostNanos"`
	Incomplete           bool   `json:"incomplete"`
}

// TaskPerformanceAttempt exposes one denominator unit and inspectable evidence
// IDs. Configuration pins the latest result, or original launch if no result
// exists. MixedConfigurations prevents treating mid-attempt changes as a clean
// comparison. An unseeded reservation has no attributed configuration.
type TaskPerformanceAttempt struct {
	AttemptID              string                     `json:"attemptId"`
	TaskID                 string                     `json:"taskId"`
	SessionID              SessionID                  `json:"sessionId,omitempty"`
	TaskRevision           int64                      `json:"taskRevision"`
	CriteriaVersion        int64                      `json:"criteriaVersion"`
	AttemptNumber          int64                      `json:"attemptNumber"`
	CreatedAt              time.Time                  `json:"createdAt"`
	Category               string                     `json:"category"`
	RequiredCapabilities   []string                   `json:"requiredCapabilities"`
	Configuration          *TaskEvaluationAttribution `json:"configuration,omitempty"`
	MixedConfigurations    bool                       `json:"mixedConfigurations"`
	ResultID               string                     `json:"resultId,omitempty"`
	ResultVersions         int64                      `json:"resultVersions"`
	EvaluationID           string                     `json:"evaluationId,omitempty"`
	Evaluations            int64                      `json:"evaluations"`
	AssessedOutcome        string                     `json:"assessedOutcome" enum:"unassessed,passed,failed,inconclusive,superseded"`
	FirstPassCompleted     bool                       `json:"firstPassCompleted"`
	CIFailureObserved      bool                       `json:"ciFailureObserved"`
	ReviewChangesRequested int64                      `json:"reviewChangesRequested"`
	ReservationElapsedMS   int64                      `json:"reservationElapsedMs"`
	ReservationOngoing     bool                       `json:"reservationOngoing"`
	Usage                  TaskPerformanceUsage       `json:"usage"`
}

// TaskPerformancePage keeps admission window, observation time and cursor with
// the evidence. Each attempt appears once, regardless of evaluation retries.
type TaskPerformancePage struct {
	Items      []TaskPerformanceAttempt `json:"items"`
	NextCursor string                   `json:"nextCursor,omitempty"`
	From       time.Time                `json:"from"`
	To         time.Time                `json:"to"`
	ObservedAt time.Time                `json:"observedAt"`
}
