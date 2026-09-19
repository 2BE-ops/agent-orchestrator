package domain

import (
	"fmt"
	"math"
	"slices"
	"time"
)

// TaskPerformanceSummaryLimit bounds a complete, consistent aggregation. Larger
// cohorts require a narrower window; they must never yield silent partial sums.
const TaskPerformanceSummaryLimit = 1000

// TaskPerformanceSummaryQuery selects a complete admission cohort and dimension.
type TaskPerformanceSummaryQuery struct {
	ProjectID ProjectID
	From      time.Time
	To        time.Time
	GroupBy   string
}

// Validate checks the shared cohort bounds and supported observational grouping.
func (q TaskPerformanceSummaryQuery) Validate() error {
	if err := (TaskPerformanceQuery{ProjectID: q.ProjectID, From: q.From, To: q.To, Limit: 1}).Validate(); err != nil {
		return err
	}
	switch q.GroupBy {
	case "agent_type", "agent_type_version", "skill", "skill_version", "harness", "model", "category", "capability":
		return nil
	default:
		return fmt.Errorf("unsupported performance grouping")
	}
}

// TaskPerformanceUsageTotals retains sums and their known sample denominators.
// PricedCostNanos is only the priced portion; it is not an observed invoice.
type TaskPerformanceUsageTotals struct {
	Events               int64 `json:"events"`
	NativeReportedEvents int64 `json:"nativeReportedEvents"`
	EstimatedEvents      int64 `json:"estimatedEvents"`
	UnknownEvents        int64 `json:"unknownEvents"`
	IncompleteAttempts   int64 `json:"incompleteAttempts"`
	KnownInputSamples    int64 `json:"knownInputSamples"`
	InputTokens          int64 `json:"inputTokens"`
	KnownOutputSamples   int64 `json:"knownOutputSamples"`
	OutputTokens         int64 `json:"outputTokens"`
	PricedEvents         int64 `json:"pricedEvents"`
	PricedCostNanos      int64 `json:"pricedCostNanos"`
}

// TaskPerformanceMetrics preserves counts instead of concealing small samples
// in percentages. AssessedPassed is historical, not current task completion.
type TaskPerformanceMetrics struct {
	Attempts                   int64                      `json:"attempts"`
	AssessedPassed             int64                      `json:"assessedPassed"`
	AssessedFailed             int64                      `json:"assessedFailed"`
	Inconclusive               int64                      `json:"inconclusive"`
	Unassessed                 int64                      `json:"unassessed"`
	Superseded                 int64                      `json:"superseded"`
	FirstPassCompleted         int64                      `json:"firstPassCompleted"`
	ResultRevisions            int64                      `json:"resultRevisions"`
	RetryAttempts              int64                      `json:"retryAttempts"`
	CIFailureAttempts          int64                      `json:"ciFailureAttempts"`
	ReviewChangesRequested     int64                      `json:"reviewChangesRequested"`
	ClosedReservationSamples   int64                      `json:"closedReservationSamples"`
	ClosedReservationElapsedMS int64                      `json:"closedReservationElapsedMs"`
	OngoingReservations        int64                      `json:"ongoingReservations"`
	UnseededAttempts           int64                      `json:"unseededAttempts"`
	MixedConfigurationAttempts int64                      `json:"mixedConfigurationAttempts"`
	Usage                      TaskPerformanceUsageTotals `json:"usage"`
}

// TaskPerformanceGroup identifies one historical dimension, optionally versioned.
// Skill/capability memberships overlap; group totals must not be added together.
type TaskPerformanceGroup struct {
	Key     string                 `json:"key"`
	Version int64                  `json:"version,omitempty"`
	Name    string                 `json:"name"`
	Metrics TaskPerformanceMetrics `json:"metrics"`
}

// TaskPerformanceSummary includes every cohort attempt in Total. Configuration
// groups exclude mixed and unseeded attempts with separate exclusion counts.
type TaskPerformanceSummary struct {
	GroupBy                            string                 `json:"groupBy" enum:"agent_type,agent_type_version,skill,skill_version,harness,model,category,capability"`
	From                               time.Time              `json:"from"`
	To                                 time.Time              `json:"to"`
	ObservedAt                         time.Time              `json:"observedAt"`
	Total                              TaskPerformanceMetrics `json:"total"`
	Groups                             []TaskPerformanceGroup `json:"groups"`
	ExcludedMixedConfigurationAttempts int64                  `json:"excludedMixedConfigurationAttempts"`
	ExcludedUnseededAttempts           int64                  `json:"excludedUnseededAttempts"`
}

type performanceGroupKey struct {
	id      string
	version int64
}

// SummarizeTaskPerformance aggregates a complete bounded cohort. It refuses
// duplicate attempts and arithmetic overflow; it never invents missing counters.
func SummarizeTaskPerformance(query TaskPerformanceSummaryQuery, items []TaskPerformanceAttempt, observedAt time.Time) (TaskPerformanceSummary, error) {
	result := TaskPerformanceSummary{GroupBy: query.GroupBy, From: query.From, To: query.To, ObservedAt: observedAt, Groups: []TaskPerformanceGroup{}}
	if err := query.Validate(); err != nil {
		return result, err
	}
	if len(items) > TaskPerformanceSummaryLimit {
		return result, fmt.Errorf("performance cohort exceeds %d attempts", TaskPerformanceSummaryLimit)
	}
	seen := map[string]bool{}
	groups := map[performanceGroupKey]*TaskPerformanceGroup{}
	for _, item := range items {
		if item.AttemptID == "" || seen[item.AttemptID] {
			return TaskPerformanceSummary{}, fmt.Errorf("duplicate or missing performance attempt identity")
		}
		seen[item.AttemptID] = true
		if err := addTaskPerformanceMetrics(&result.Total, item); err != nil {
			return TaskPerformanceSummary{}, err
		}
		if query.GroupBy != "category" && query.GroupBy != "capability" {
			if item.Configuration == nil {
				result.ExcludedUnseededAttempts++
				continue
			}
			if item.MixedConfigurations {
				result.ExcludedMixedConfigurationAttempts++
				continue
			}
		}
		memberships := taskPerformanceGroups(query.GroupBy, item)
		for key, name := range memberships {
			group := groups[key]
			if group == nil {
				group = &TaskPerformanceGroup{Key: key.id, Version: key.version, Name: name}
				groups[key] = group
			}
			if err := addTaskPerformanceMetrics(&group.Metrics, item); err != nil {
				return TaskPerformanceSummary{}, err
			}
		}
	}
	for _, group := range groups {
		result.Groups = append(result.Groups, *group)
	}
	slices.SortFunc(result.Groups, func(a, b TaskPerformanceGroup) int {
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
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

func taskPerformanceGroups(dimension string, item TaskPerformanceAttempt) map[performanceGroupKey]string {
	groups := map[performanceGroupKey]string{}
	add := func(key, name string, version int64) { groups[performanceGroupKey{key, version}] = name }
	switch dimension {
	case "category":
		name := item.Category
		if name == "" {
			name = "Uncategorized"
		}
		add(item.Category, name, 0)
	case "capability":
		for _, capability := range item.RequiredCapabilities {
			add(capability, capability, 0)
		}
		if len(groups) == 0 {
			add("", "No required capability", 0)
		}
	case "agent_type", "agent_type_version":
		ref := item.Configuration.AgentType
		version := int64(0)
		if dimension == "agent_type_version" {
			version = ref.Version
		}
		add(ref.ID, ref.Name, version)
	case "skill", "skill_version":
		for _, ref := range item.Configuration.Skills {
			version := int64(0)
			if dimension == "skill_version" {
				version = ref.Version
			}
			add(ref.ID, ref.Name, version)
		}
		if len(groups) == 0 {
			add("", "No attached skill", 0)
		}
	case "harness":
		add(string(item.Configuration.Harness), string(item.Configuration.Harness), 0)
	case "model":
		name := item.Configuration.Model
		if name == "" {
			name = "Unspecified native model"
		}
		add(item.Configuration.Model, name, 0)
	}
	return groups
}

func addTaskPerformanceMetrics(metrics *TaskPerformanceMetrics, item TaskPerformanceAttempt) error {
	metrics.Attempts++
	switch item.AssessedOutcome {
	case "passed":
		metrics.AssessedPassed++
	case "failed":
		metrics.AssessedFailed++
	case "inconclusive":
		metrics.Inconclusive++
	case "superseded":
		metrics.Superseded++
	case "unassessed":
		metrics.Unassessed++
	default:
		return fmt.Errorf("unknown performance assessment outcome")
	}
	if item.FirstPassCompleted {
		metrics.FirstPassCompleted++
	}
	if item.AttemptNumber > 1 {
		metrics.RetryAttempts++
	}
	if item.CIFailureObserved {
		metrics.CIFailureAttempts++
	}
	if item.MixedConfigurations {
		metrics.MixedConfigurationAttempts++
	}
	if item.Configuration == nil {
		metrics.UnseededAttempts++
	}
	if item.ReservationOngoing {
		metrics.OngoingReservations++
	} else {
		metrics.ClosedReservationSamples++
		if err := addPerformanceCounter(&metrics.ClosedReservationElapsedMS, item.ReservationElapsedMS); err != nil {
			return err
		}
	}
	u := item.Usage
	if u.Incomplete {
		metrics.Usage.IncompleteAttempts++
	}
	if u.InputTokens != nil {
		metrics.Usage.KnownInputSamples++
		if err := addPerformanceCounter(&metrics.Usage.InputTokens, *u.InputTokens); err != nil {
			return err
		}
	}
	if u.OutputTokens != nil {
		metrics.Usage.KnownOutputSamples++
		if err := addPerformanceCounter(&metrics.Usage.OutputTokens, *u.OutputTokens); err != nil {
			return err
		}
	}
	for _, counter := range []struct {
		target *int64
		value  int64
	}{
		{&metrics.ResultRevisions, max(0, item.ResultVersions-1)}, {&metrics.ReviewChangesRequested, item.ReviewChangesRequested},
		{&metrics.Usage.Events, u.Events}, {&metrics.Usage.NativeReportedEvents, u.NativeReportedEvents}, {&metrics.Usage.EstimatedEvents, u.EstimatedEvents}, {&metrics.Usage.UnknownEvents, u.UnknownEvents},
		{&metrics.Usage.PricedEvents, u.PricedEvents}, {&metrics.Usage.PricedCostNanos, u.PricedCostNanos},
	} {
		if err := addPerformanceCounter(counter.target, counter.value); err != nil {
			return err
		}
	}
	return nil
}

func addPerformanceCounter(total *int64, value int64) error {
	if value < 0 || *total > math.MaxInt64-value {
		return fmt.Errorf("performance counter overflow or negative value")
	}
	*total += value
	return nil
}
