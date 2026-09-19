package domain

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func summaryQuery(group string) TaskPerformanceSummaryQuery {
	return TaskPerformanceSummaryQuery{ProjectID: "project", From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), GroupBy: group}
}

func summaryAttempts() []TaskPerformanceAttempt {
	zero, tokens := int64(0), int64(12)
	config := &TaskEvaluationAttribution{AgentType: WorkerDefinitionRef{ID: "coder", Version: 1, Name: "Coder"}, Skills: []WorkerDefinitionRef{{ID: "go", Version: 1, Name: "Go"}, {ID: "tests", Version: 2, Name: "Tests"}}, Harness: HarnessCodex, Model: "model-a"}
	newConfig := *config
	newConfig.AgentType.Version = 2
	return []TaskPerformanceAttempt{
		{AttemptID: "a", AttemptNumber: 1, AssessedOutcome: "passed", FirstPassCompleted: true, ResultVersions: 1, Configuration: config, Category: "backend", RequiredCapabilities: []string{"go", "tests"}, ReservationElapsedMS: 1000, Usage: TaskPerformanceUsage{Events: 1, NativeReportedEvents: 1, InputTokens: &tokens, OutputTokens: &zero, PricedEvents: 1, PricedCostNanos: 7}},
		{AttemptID: "b", AttemptNumber: 2, AssessedOutcome: "failed", ResultVersions: 3, Configuration: &newConfig, Category: "backend", RequiredCapabilities: []string{"go"}, CIFailureObserved: true, ReviewChangesRequested: 2, ReservationElapsedMS: 3000, Usage: TaskPerformanceUsage{Events: 1, EstimatedEvents: 1, InputTokens: &zero, Incomplete: true}},
		{AttemptID: "c", AttemptNumber: 3, AssessedOutcome: "inconclusive", ResultVersions: 1, Configuration: config, MixedConfigurations: true, Category: "backend", ReservationOngoing: true, ReservationElapsedMS: 99999, Usage: TaskPerformanceUsage{Incomplete: true}},
		{AttemptID: "d", AttemptNumber: 1, AssessedOutcome: "unassessed", ReservationOngoing: true, Usage: TaskPerformanceUsage{Incomplete: true}},
	}
}

func TestTaskPerformanceSummaryDenominatorsAndExclusions(t *testing.T) {
	query := summaryQuery("agent_type_version")
	observed := time.Now().UTC()
	summary, err := SummarizeTaskPerformance(query, summaryAttempts(), observed)
	if err != nil {
		t.Fatal(err)
	}
	m := summary.Total
	if m.Attempts != 4 || m.AssessedPassed != 1 || m.AssessedFailed != 1 || m.Inconclusive != 1 || m.Unassessed != 1 || m.FirstPassCompleted != 1 || m.RetryAttempts != 2 || m.ResultRevisions != 2 || m.CIFailureAttempts != 1 || m.ReviewChangesRequested != 2 || m.ClosedReservationSamples != 2 || m.ClosedReservationElapsedMS != 4000 || m.OngoingReservations != 2 || m.UnseededAttempts != 1 || m.MixedConfigurationAttempts != 1 {
		t.Fatalf("incorrect attempt denominators: %+v", m)
	}
	if m.Usage.KnownInputSamples != 2 || m.Usage.InputTokens != 12 || m.Usage.KnownOutputSamples != 1 || m.Usage.OutputTokens != 0 || m.Usage.Events != 2 || m.Usage.PricedEvents != 1 || m.Usage.PricedCostNanos != 7 || m.Usage.IncompleteAttempts != 3 {
		t.Fatalf("unknown counters/pricing lost: %+v", m.Usage)
	}
	if summary.ExcludedMixedConfigurationAttempts != 1 || summary.ExcludedUnseededAttempts != 1 || len(summary.Groups) != 2 || summary.Groups[0].Version != 1 || summary.Groups[1].Version != 2 || summary.Groups[0].Metrics.Attempts != 1 || summary.Groups[1].Metrics.Attempts != 1 || !summary.ObservedAt.Equal(observed) || !summary.From.Equal(query.From) || !summary.To.Equal(query.To) {
		t.Fatalf("incorrect provenance grouping: %+v", summary)
	}
}

func TestTaskPerformanceSummarySupportsEveryDimensionAndOverlappingMembership(t *testing.T) {
	for dimension, want := range map[string]int{"agent_type": 1, "agent_type_version": 2, "skill": 2, "skill_version": 2, "harness": 1, "model": 1, "category": 2, "capability": 3} {
		t.Run(dimension, func(t *testing.T) {
			items := summaryAttempts()
			// One attempt contributes once per group even when the same skill or
			// capability is listed twice; distinct memberships still overlap.
			items[0].Configuration.Skills = append(items[0].Configuration.Skills, items[0].Configuration.Skills[0])
			items[0].RequiredCapabilities = append(items[0].RequiredCapabilities, "go")
			s, err := SummarizeTaskPerformance(summaryQuery(dimension), items, time.Now().UTC())
			if err != nil || len(s.Groups) != want {
				t.Fatalf("groups: %+v %v", s, err)
			}
			if dimension == "category" || dimension == "capability" {
				if s.ExcludedUnseededAttempts != 0 || s.ExcludedMixedConfigurationAttempts != 0 {
					t.Fatalf("task dimensions dropped work: %+v", s)
				}
			}
			if dimension == "skill" || dimension == "skill_version" {
				for _, group := range s.Groups {
					if group.Metrics.Attempts != 2 {
						t.Fatalf("overlapping membership inflated: %+v", group)
					}
				}
			}
		})
	}
	item := summaryAttempts()[0]
	item.Configuration.Model = ""
	item.Configuration.Skills = nil
	for _, dimension := range []string{"model", "skill"} {
		s, err := SummarizeTaskPerformance(summaryQuery(dimension), []TaskPerformanceAttempt{item}, time.Now().UTC())
		if err != nil || len(s.Groups) != 1 || s.Groups[0].Key != "" || s.Groups[0].Metrics.Attempts != 1 {
			t.Fatalf("missing dimension disappeared: %+v %v", s, err)
		}
	}
}

func TestTaskPerformanceSummaryRejectsPartialDuplicateAndOverflowData(t *testing.T) {
	query := summaryQuery("category")
	items := summaryAttempts()
	for name, input := range map[string][]TaskPerformanceAttempt{"duplicate": {items[0], items[0]}, "missing ID": {{AssessedOutcome: "unassessed"}}, "too many": make([]TaskPerformanceAttempt, TaskPerformanceSummaryLimit+1)} {
		t.Run(name, func(t *testing.T) {
			if _, err := SummarizeTaskPerformance(query, input, time.Now().UTC()); err == nil {
				t.Fatal("invalid cohort accepted")
			}
		})
	}
	for _, kind := range []string{"tokens", "cost", "duration", "negative"} {
		t.Run(kind, func(t *testing.T) {
			input := summaryAttempts()[:2]
			switch kind {
			case "tokens":
				value := int64(math.MaxInt64)
				input[1].Usage.InputTokens = &value
			case "cost":
				input[1].Usage.PricedCostNanos = math.MaxInt64
			case "duration":
				input[1].ReservationElapsedMS = math.MaxInt64
			case "negative":
				input[1].Usage.PricedCostNanos = -1
			}
			s, err := SummarizeTaskPerformance(query, input, time.Now().UTC())
			if err == nil || !strings.Contains(err.Error(), "counter") || s.Total.Attempts != 0 || len(s.Groups) != 0 {
				t.Fatalf("partial aggregate escaped: %+v %v", s, err)
			}
		})
	}
	bounded := make([]TaskPerformanceAttempt, TaskPerformanceSummaryLimit)
	for i := range bounded {
		bounded[i] = TaskPerformanceAttempt{AttemptID: fmt.Sprint(i), AssessedOutcome: "unassessed"}
	}
	if s, err := SummarizeTaskPerformance(query, bounded, time.Now().UTC()); err != nil || s.Total.Attempts != TaskPerformanceSummaryLimit {
		t.Fatalf("exact cohort bound: %+v %v", s, err)
	}
	query.GroupBy = "intelligence"
	if _, err := SummarizeTaskPerformance(query, nil, time.Now().UTC()); err == nil {
		t.Fatal("unsupported grouping accepted")
	}
}
