package domain

import (
	"testing"
	"time"
)

func TestTaskPerformanceQueryBounds(t *testing.T) {
	valid := TaskPerformanceQuery{ProjectID: "p", From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), Limit: 100}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*TaskPerformanceQuery){
		"missing project": func(q *TaskPerformanceQuery) { q.ProjectID = "" },
		"missing start":   func(q *TaskPerformanceQuery) { q.From = time.Time{} },
		"missing end":     func(q *TaskPerformanceQuery) { q.To = time.Time{} },
		"reversed":        func(q *TaskPerformanceQuery) { q.To = q.From.Add(-time.Second) },
		"empty":           func(q *TaskPerformanceQuery) { q.To = q.From },
		"too long":        func(q *TaskPerformanceQuery) { q.To = q.From.Add(367 * 24 * time.Hour) },
		"invalid cursor":  func(q *TaskPerformanceQuery) { q.After = "bad\ncursor" },
		"zero limit":      func(q *TaskPerformanceQuery) { q.Limit = 0 },
		"large limit":     func(q *TaskPerformanceQuery) { q.Limit = 101 },
	} {
		t.Run(name, func(t *testing.T) {
			query := valid
			change(&query)
			if err := query.Validate(); err == nil {
				t.Fatal("invalid performance query accepted")
			}
		})
	}
}
