package review

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type taskEngineStore struct {
	fakeStore
	ports.TaskReviewStore
	contexts map[string]domain.TaskReviewContext
	started  []string
}

func (s *taskEngineStore) InsertTaskReviewRun(ctx context.Context, run domain.ReviewRun, frozen domain.TaskReviewContext) error {
	if err := frozen.Validate(); err != nil {
		return err
	}
	if err := s.InsertReviewRun(ctx, run); err != nil {
		return err
	}
	if s.contexts == nil {
		s.contexts = map[string]domain.TaskReviewContext{}
	}
	s.contexts[run.ID] = frozen
	return nil
}

func (s *taskEngineStore) MarkTaskReviewStarted(_ context.Context, id, launchID string) (bool, error) {
	if s.review == nil || s.review.ReviewerHandleID == "" || s.review.ReviewerLaunchID != launchID || s.contexts[id].LaunchID != launchID {
		return false, nil
	}
	s.started = append(s.started, id)
	return true, nil
}

func TestTriggerTaskUsesFrozenScopeAndNativeConfiguration(t *testing.T) {
	ctx := context.Background()
	spec := taskLaunchSpec(t)
	store := &taskEngineStore{}
	store.review = &domain.Review{ID: "old-review", SessionID: spec.WorkerID, Harness: spec.Harness, ReviewerHandleID: "review-mer-1", AgentSessionID: "previous-type-conversation"}
	store.runs = []domain.ReviewRun{{ID: "generic-approved", SessionID: spec.WorkerID, Harness: spec.Harness, PRURL: spec.PRURL, TargetSHA: spec.TargetSHA, Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved, CreatedAt: time.Unix(-1, 0)}}
	launcher := &fakeLauncher{handle: "review-mer-1", agentSessionID: "fresh-task-review", alive: true}
	launcher.onSpawn = func(launch LaunchSpec) {
		if store.contexts[launch.RunID].ContentHash != spec.TaskContext.ContentHash || len(store.started) != 0 {
			t.Fatal("spawn preceded durable context or fabricated launch witness")
		}
	}
	engine := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt(spec.TargetSHA), fakeProjects{}, launcher)
	result, err := engine.TriggerTask(ctx, *spec.TaskContext)
	if err != nil || !result.Created || result.Run.TaskScope != spec.TaskContext.ScopeHash() || len(store.started) != 1 {
		t.Fatalf("task review not created with provenance: %+v %v", result, err)
	}
	if launcher.gotSpec.TaskContext == nil || launcher.gotSpec.TaskContext.ContentHash != spec.TaskContext.ContentHash || launcher.gotSpec.AgentConfig.Model != "review-model" || launcher.gotSpec.AgentSessionID != "" || launcher.notified {
		t.Fatalf("native configuration inherited or lost: %+v", launcher.gotSpec)
	}
	if retried, err := engine.TriggerTask(ctx, *spec.TaskContext); err != nil || retried.Created || launcher.spawnCount != 1 || len(store.started) != 1 {
		t.Fatalf("exact retry duplicated a native pass: %+v %v", retried, err)
	}
	if _, err := engine.Trigger(ctx, spec.WorkerID, spec.Harness, domain.AgentConfig{}); err == nil || launcher.spawnCount != 1 {
		t.Fatal("ordinary trigger preempted the running task scope")
	}
	other := *spec.TaskContext
	other.ResultID = "replacement-result"
	other.ContentHash = other.Hash()
	if _, err := engine.TriggerTask(ctx, other); err == nil || launcher.spawnCount != 1 {
		t.Fatal("different task scope preempted native ownership")
	}
	if _, err := engine.RestoreReviewer(ctx, spec.WorkerID); err == nil || launcher.restored {
		t.Fatal("restored pinned native history with generic review configuration")
	}
	if _, err := store.UpdateReviewRunResult(ctx, result.Run.ID, domain.ReviewRunComplete, domain.VerdictApproved, "Reviewed fixed criteria", "", false); err != nil {
		t.Fatal(err)
	}
	if retried, err := engine.TriggerTask(ctx, *spec.TaskContext); err != nil || retried.Created || launcher.spawnCount != 1 {
		t.Fatalf("approval at exact scope was not reused: %+v %v", retried, err)
	}
}

func TestTriggerTaskLaunchFailureDoesNotRecordStarted(t *testing.T) {
	for _, preflight := range []bool{true, false} {
		t.Run(map[bool]string{true: "preflight", false: "spawn"}[preflight], func(t *testing.T) {
			spec := taskLaunchSpec(t)
			store := &taskEngineStore{}
			launcher := &fakeLauncher{handle: "review-mer-1"}
			if preflight {
				launcher.preflightErr = errors.New("provider unavailable")
			} else {
				launcher.spawnErr = errors.New("native spawn failed")
			}
			engine := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt(spec.TargetSHA), fakeProjects{}, launcher)
			if _, err := engine.TriggerTask(context.Background(), *spec.TaskContext); err == nil || len(store.started) != 0 || len(store.contexts) != 1 || len(store.runs) != 1 || store.runs[0].Status != domain.ReviewRunFailed {
				t.Fatalf("failed launch became successful provenance: %v %+v", err, store)
			}
		})
	}
}

func TestTriggerTaskUncertainLaunchNeverBlindlyRetries(t *testing.T) {
	ctx := context.Background()
	spec := taskLaunchSpec(t)
	store := &taskEngineStore{}
	launcher := &fakeLauncher{handle: "review-mer-1", spawnErr: ErrTaskReviewLaunchUncertain}
	engine := newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt(spec.TargetSHA), fakeProjects{}, launcher)
	if _, err := engine.TriggerTask(ctx, *spec.TaskContext); !errors.Is(err, ErrTaskReviewLaunchUncertain) || len(store.runs) != 1 || store.runs[0].Status != domain.ReviewRunRunning || len(store.started) != 0 {
		t.Fatalf("uncertain handoff treated as known failure: %v %+v", err, store)
	}
	// Recreate the engine without a known live reviewer handle, as on restart.
	engine = newEngineForTest(store, fakeSessions{rec: liveWorker(), ok: true}, prAt(spec.TargetSHA), fakeProjects{}, launcher)
	if result, err := engine.TriggerTask(ctx, *spec.TaskContext); err != nil || result.Created || launcher.spawnCount != 1 {
		t.Fatalf("restart repeated uncertain handoff: %+v %v", result, err)
	}
}
