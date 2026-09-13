package sessionmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestStatusReadinessWaitsForRecoveryAndAllowsRetry(t *testing.T) {
	m, st, rt, _ := newManager()
	rec := domain.SessionRecord{ID: "s1", ProjectID: "mer", Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: time.Unix(100, 0)},
		Metadata: domain.SessionMetadata{Branch: "ao/s1", WorkspacePath: "/wt/s1", RuntimeHandleID: "s1"}}
	st.sessions[rec.ID] = rec
	if got := m.SessionStatusReadiness(rec); got != "checking" {
		t.Fatalf("before recovery = %s", got)
	}
	rt.aliveErr = errors.New("runtime probe unavailable")
	if err := m.ReconcileBackground(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.SessionStatusReadiness(rec); got != "unavailable" {
		t.Fatalf("failed probe = %s", got)
	}
	if st.sessions[rec.ID].Activity != rec.Activity {
		t.Fatal("failed probe changed activity")
	}
	rt.aliveErr = nil
	rt.aliveByHandle = map[string]bool{"s1": true}
	if _, err := m.ResumeAgentWithMode(context.Background(), rec.ID); err != nil {
		t.Fatal(err)
	}
	if got := m.SessionStatusReadiness(st.sessions[rec.ID]); got != "ready" {
		t.Fatalf("retry = %s", got)
	}
	if rt.created != 0 {
		t.Fatal("retry spawned a duplicate of a surviving runtime")
	}
}

func TestStatusReadinessDeadlineDoesNotInventIdle(t *testing.T) {
	m, st, rt, _ := newManager()
	rec := domain.SessionRecord{ID: "s1", ProjectID: "mer", Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive},
		Metadata: domain.SessionMetadata{Branch: "ao/s1", WorkspacePath: "/wt/s1", RuntimeHandleID: "s1"}}
	st.sessions[rec.ID] = rec
	blocked := &blockingAliveRuntime{fakeRuntime: rt, entered: make(chan domain.SessionID, 1), release: make(chan struct{})}
	m.runtime = blocked
	finished := make(chan error, 1)
	go func() { finished <- m.ReconcileBackground(context.Background()) }()
	<-blocked.entered
	if got := m.SessionStatusReadiness(rec); got != "checking" {
		t.Fatalf("pending = %s", got)
	}
	// Age the presentation deadline while the worker remains held at the real
	// probe boundary; no wall-clock sleep or premature idle assignment.
	m.statusRecoveryMu.Lock()
	result := m.statusRecoveries[rec.ID]
	result.startedAt = time.Now().Add(-statusVerificationLimit)
	m.statusRecoveries[rec.ID] = result
	m.statusRecoveryMu.Unlock()
	if got := m.SessionStatusReadiness(rec); got != "unavailable" {
		t.Fatalf("deadline = %s", got)
	}
	close(blocked.release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if got := m.SessionStatusReadiness(rec); got != "ready" {
		t.Fatalf("late recovery = %s", got)
	}
	if st.sessions[rec.ID].Activity != rec.Activity {
		t.Fatal("recovery invented activity")
	}
}

func TestStatusReadinessDiscoveryFailureIsUnavailable(t *testing.T) {
	m, st, _, _ := newManager()
	st.listAllErr = errors.New("storage unavailable")
	if err := m.ReconcileBackground(context.Background()); err == nil {
		t.Fatal("expected discovery failure")
	}
	if got := m.SessionStatusReadiness(domain.SessionRecord{ID: "s1"}); got != "unavailable" {
		t.Fatalf("readiness = %s", got)
	}
}
