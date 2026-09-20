package review

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// pinnedRun fixtures a still-running task-scoped review pass: the native
// reviewer was launched from sealed adaptive task context.
func pinnedRun(harness domain.ReviewerHarness) domain.ReviewRun {
	return domain.ReviewRun{ID: "pinned-run-1", ReviewID: "rev-1", SessionID: "mer-1", Harness: harness,
		PRURL: "https://github.com/o/r/pull/1", TargetSHA: "sha1", Status: domain.ReviewRunRunning,
		TaskScope: "task-scope-hash"}
}

func TestRestoreReviewerKeepsLivePinnedTaskReviewerAndItsSealedContext(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{pinnedRun(domain.ReviewerCodex)},
	}
	launcher := &fakeLauncher{alive: true, handle: "review-mer-1"}
	worker := liveWorker()
	worker.ReviewerHarness = domain.ReviewerCodex
	eng := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.RestoreReviewer(context.Background(), "mer-1")
	if err == nil || !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "still active") {
		t.Fatalf("live pinned reviewer restore: %v", err)
	}
	if res.Restored || launcher.restored || launcher.spawned {
		t.Fatalf("idle pane substituted for the sealed context: res=%+v launcher=%+v", res, launcher)
	}
	if store.runs[0].Status != domain.ReviewRunRunning {
		t.Fatalf("live pinned run settled: %+v", store.runs[0])
	}
}

func TestRestoreReviewerSettlesDeadPinnedTaskReviewerAsRetainedUncertainty(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{pinnedRun(domain.ReviewerCodex)},
	}
	launcher := &fakeLauncher{alive: false, handle: "review-mer-1"}
	worker := liveWorker()
	worker.ReviewerHarness = domain.ReviewerCodex
	eng := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.RestoreReviewer(context.Background(), "mer-1")
	if err != nil {
		t.Fatalf("dead pinned reviewer restore: %v", err)
	}
	if !res.Restored || !launcher.restored {
		t.Fatalf("expected idle restore after reconciliation: res=%+v launcher=%+v", res, launcher)
	}
	run := store.runs[0]
	if run.Status != domain.ReviewRunFailed || !strings.Contains(run.Body, "uncertain") {
		t.Fatalf("pinned run settled dishonestly: %+v", run)
	}
	if launcher.gotSpec.TaskContext != nil {
		t.Fatalf("idle restore received task context: %+v", launcher.gotSpec.TaskContext)
	}
}

func TestRestoreReviewerKeepsPinnedRunWhenLivenessIsUnknown(t *testing.T) {
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{pinnedRun(domain.ReviewerCodex)},
	}
	launcher := &fakeLauncher{alive: false, handle: "review-mer-1", aliveErr: errors.New("probe failed")}
	worker := liveWorker()
	worker.ReviewerHarness = domain.ReviewerCodex
	eng := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	if _, err := eng.RestoreReviewer(context.Background(), "mer-1"); err == nil || !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown liveness treated as death: %v", err)
	}
	if store.runs[0].Status != domain.ReviewRunRunning || launcher.restored || launcher.spawned {
		t.Fatalf("unknown liveness settled or substituted: run=%+v launcher=%+v", store.runs[0], launcher)
	}

	// An uncertain launch that never obtained a pane handle cannot be probed;
	// its run stays retained rather than assumed dead.
	store2 := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerClaudeCode},
		runs:   []domain.ReviewRun{pinnedRun(domain.ReviewerClaudeCode)},
	}
	launcher2 := &fakeLauncher{alive: false, handle: "review-mer-1"}
	worker2 := liveWorker()
	worker2.ReviewerHarness = domain.ReviewerClaudeCode
	eng2 := newEngineForTest(store2, fakeSessions{rec: worker2, ok: true}, prAt("sha1"), fakeProjects{}, launcher2)
	if _, err := eng2.RestoreReviewer(context.Background(), "mer-1"); err == nil || !errors.Is(err, ErrInvalid) {
		t.Fatalf("unprobeable uncertain launch restored: %v", err)
	}
	if store2.runs[0].Status != domain.ReviewRunRunning {
		t.Fatalf("uncertain launch settled without evidence: %+v", store2.runs[0])
	}
}

func TestRestoreReviewerAllowsIdlePaneAfterPinnedPassFinished(t *testing.T) {
	finished := pinnedRun(domain.ReviewerCodex)
	finished.Status = domain.ReviewRunComplete
	finished.Verdict = domain.VerdictApproved
	store := &fakeStore{
		review: &domain.Review{ID: "rev-1", SessionID: "mer-1", Harness: domain.ReviewerCodex, ReviewerHandleID: "review-mer-1"},
		runs:   []domain.ReviewRun{finished},
	}
	launcher := &fakeLauncher{alive: false, handle: "review-mer-1"}
	worker := liveWorker()
	worker.ReviewerHarness = domain.ReviewerCodex
	eng := newEngineForTest(store, fakeSessions{rec: worker, ok: true}, prAt("sha1"), fakeProjects{}, launcher)

	res, err := eng.RestoreReviewer(context.Background(), "mer-1")
	if err != nil {
		t.Fatalf("finished pinned pass blocked restore: %v", err)
	}
	if !res.Restored || !launcher.restored {
		t.Fatalf("expected idle restore: res=%+v launcher=%+v", res, launcher)
	}
	if launcher.gotSpec.TaskContext != nil {
		t.Fatalf("idle restore received task context: %+v", launcher.gotSpec.TaskContext)
	}
}

func TestLauncherRestoreTerminalRefusesRetainedTaskContextSubstitution(t *testing.T) {
	l := newTestLauncher(t, &fakeReviewer{}, &fakeRuntime{})
	spec := launchSpec()
	spec.TaskContext = &domain.TaskReviewContext{TaskID: "work", ResultHash: "hash", LaunchID: "launch-1"}

	if _, err := l.RestoreTerminal(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "reconciliation of its retained native launch") {
		t.Fatalf("idle restore substituted live configuration: %v", err)
	}
}
