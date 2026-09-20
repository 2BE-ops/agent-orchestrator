package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func controlActor() domain.AdaptiveActor {
	return domain.AdaptiveActor{Kind: "USER", ID: "local-user"}
}

func controlChange(t *testing.T, s *sqlite.Store, project string, state domain.ProjectControlState, reason string) domain.ProjectControlView {
	t.Helper()
	view, err := s.SetProjectControl(context.Background(), domain.ProjectID(project), state, controlActor(), reason, time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("set control %s: %v", state, err)
	}
	return view
}

func TestProjectControlLifecycleFencesAdmissions(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	seedProject(t, s, "other")
	// Absence is the running default.
	view, err := s.GetProjectControl(ctx, "project")
	if err != nil || view.Control.State != domain.ProjectRunning || view.EffectiveState != domain.ProjectRunning || view.ActiveAttempts != 0 {
		t.Fatalf("default control not running: %+v %v", view, err)
	}
	if _, err := s.CreateSession(ctx, sampleRecord("project")); err != nil {
		t.Fatalf("running project refused a worker: %v", err)
	}
	// Pause fences only the paused project; other projects and standalone
	// workers keep admitting.
	paused := controlChange(t, s, "project", domain.ProjectPaused, "Evening maintenance")
	if paused.Control.State != domain.ProjectPaused || paused.EffectiveState != domain.ProjectPaused {
		t.Fatalf("pause not recorded: %+v", paused)
	}
	rec := sampleRecord("project")
	rec.Kind = domain.KindWorker
	if _, err := s.CreateSession(ctx, rec); !errors.Is(err, ports.ErrProjectAdmissionsFenced) {
		t.Fatalf("paused project admitted a worker: %v", err)
	}
	if _, err := s.CreateSession(ctx, sampleRecord("other")); err != nil {
		t.Fatalf("unrelated project fenced: %v", err)
	}
	if _, err := s.CreateSession(ctx, sampleRecord("")); err != nil {
		t.Fatalf("standalone worker fenced: %v", err)
	}
	// Orchestrator and manager sessions are not worker admissions.
	controller := sampleRecord("project")
	controller.Kind = domain.KindOrchestrator
	if _, err := s.CreateSession(ctx, controller); err != nil {
		t.Fatalf("controller session fenced by pause: %v", err)
	}
	// Illegal transitions refuse; the legal resume admits again.
	if _, err := s.SetProjectControl(ctx, "project", domain.ProjectDraining, controlActor(), "Skip pause", time.Now().UTC()); !errors.Is(err, ports.ErrProjectControlConflict) {
		t.Fatalf("paused -> draining accepted: %v", err)
	}
	controlChange(t, s, "project", domain.ProjectRunning, "Resumed")
	if _, err := s.CreateSession(ctx, rec); err != nil {
		t.Fatalf("resumed project refused a worker: %v", err)
	}
	// Idempotent control calls return the current state without error.
	same := controlChange(t, s, "project", domain.ProjectRunning, "Resumed again")
	if same.Control.State != domain.ProjectRunning {
		t.Fatalf("idempotent resume changed state: %+v", same)
	}
	// Unknown projects and invalid actors refuse.
	if _, err := s.SetProjectControl(ctx, "ghost", domain.ProjectPaused, controlActor(), "No project", time.Now().UTC()); !errors.Is(err, ports.ErrProjectControlNotFound) {
		t.Fatalf("unknown project accepted: %v", err)
	}
	if _, err := s.SetProjectControl(ctx, "project", domain.ProjectPaused, domain.AdaptiveActor{Kind: "AGENT_MANAGER", ID: "mgr"}, "Manager self-pause", time.Now().UTC()); !errors.Is(err, ports.ErrProjectControlInvalid) {
		t.Fatalf("manager actor accepted: %v", err)
	}
}

func TestProjectControlPauseFencesNewReservationsButNotReplays(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	// Reserve before the pause, then fence and verify the idempotent replay
	// still returns the existing reservation: recovery never re-fences.
	if _, _, err := s.ReserveTask(ctx, taskReservation("attempt", "work")); err != nil {
		t.Fatal(err)
	}
	controlChange(t, s, "project", domain.ProjectPaused, "Hold new work")
	createTask(t, s, "queued", taskDefinition())
	if _, _, err := s.ReserveTask(ctx, taskReservation("queued-attempt", "queued")); !errors.Is(err, ports.ErrProjectAdmissionsFenced) {
		t.Fatalf("paused project admitted a new reservation: %v", err)
	}
	attempt, lease, err := s.ReserveTask(ctx, taskReservation("attempt", "work"))
	if err != nil || attempt.ID != "attempt" || lease.AttemptID != "attempt" {
		t.Fatalf("reservation replay fenced: %+v %+v %v", attempt, lease, err)
	}
	controlChange(t, s, "project", domain.ProjectRunning, "Resumed")
	if _, _, err := s.ReserveTask(ctx, taskReservation("queued-attempt", "queued")); err != nil {
		t.Fatalf("resumed project refused a new reservation: %v", err)
	}
}

func TestProjectControlPauseRacesConcurrentLaunches(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	controlChange(t, s, "project", domain.ProjectRunning, "Start")
	var wg sync.WaitGroup
	created := make(chan bool, 8)
	refused := make(chan error, 8)
	pause := make(chan struct{})
	for range 8 {
		wg.Go(func() {
			rec := sampleRecord("project")
			rec.Kind = domain.KindWorker
			<-pause
			if _, err := s.CreateSession(ctx, rec); err != nil {
				if !errors.Is(err, ports.ErrProjectAdmissionsFenced) {
					t.Errorf("unexpected refusal: %v", err)
				}
				refused <- err
				return
			}
			created <- true
		})
	}
	// The pause returns before any launcher proceeds: after it, no launch can
	// slip through, and before it every launch saw a running project.
	controlChange(t, s, "project", domain.ProjectPaused, "Freeze now")
	close(pause)
	wg.Wait()
	close(created)
	close(refused)
	if len(created) != 0 || len(refused) != 8 {
		t.Fatalf("pause leaked launches: %d created, %d refused", len(created), len(refused))
	}
}

func TestProjectControlDrainDerivesPausedWhenAttemptsFinish(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "work", taskDefinition())
	_, lease := reserveTask(t, s, "attempt", "work")
	draining := controlChange(t, s, "project", domain.ProjectDraining, "Finish current work")
	if draining.Control.State != domain.ProjectDraining || draining.EffectiveState != domain.ProjectDraining || draining.ActiveAttempts != 1 {
		t.Fatalf("drain not derived from the live attempt: %+v", draining)
	}
	rec := sampleRecord("project")
	rec.Kind = domain.KindWorker
	if _, err := s.CreateSession(ctx, rec); !errors.Is(err, ports.ErrProjectAdmissionsFenced) {
		t.Fatalf("draining project admitted a worker: %v", err)
	}
	// Normal lifecycle release of the current attempt completes the drain.
	if err := s.ReleaseTaskLease(ctx, domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, Reason: "worker completed", Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	drained, err := s.GetProjectControl(ctx, "project")
	if err != nil || drained.Control.State != domain.ProjectDraining || drained.EffectiveState != domain.ProjectPaused || drained.ActiveAttempts != 0 {
		t.Fatalf("drained project did not derive paused: %+v %v", drained, err)
	}
	// A stopped project must be resumed explicitly before admissions open.
	controlChange(t, s, "project", domain.ProjectStopped, "Stop after drain")
	if _, err := s.CreateSession(ctx, rec); !errors.Is(err, ports.ErrProjectAdmissionsFenced) {
		t.Fatalf("stopped project admitted a worker: %v", err)
	}
	controlChange(t, s, "project", domain.ProjectRunning, "Restart")
	if _, err := s.CreateSession(ctx, rec); err != nil {
		t.Fatalf("restarted project refused a worker: %v", err)
	}
}

func TestProjectControlRestartKeepsFence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	controlChange(t, s, "project", domain.ProjectPaused, "Survives restart")
	s2, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	view, err := s2.GetProjectControl(ctx, "project")
	if err != nil || view.Control.State != domain.ProjectPaused || view.Control.Reason != "Survives restart" {
		t.Fatalf("control lost across restart: %+v %v", view, err)
	}
	rec := sampleRecord("project")
	rec.Kind = domain.KindWorker
	if _, err := s2.CreateSession(ctx, rec); !errors.Is(err, ports.ErrProjectAdmissionsFenced) {
		t.Fatalf("restart unfenced the project: %v", err)
	}
}

func TestNeedsHumanFencesTaskAndDescendantsButNotBranches(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	createTask(t, s, "root", taskDefinition())
	child := taskDefinition()
	child.ParentID = "root"
	createTask(t, s, "child", child)
	createTask(t, s, "branch", taskDefinition())
	raised, err := s.RaiseTaskNeedsHuman(ctx, "root", domain.TaskNeedsHuman{
		ID: "nh-1", ReasonCode: "credential_missing", Detail: "The provider credential expired",
		Actor: domain.AdaptiveActor{Kind: "WORKER", ID: "worker-1"}, CreatedAt: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	})
	if err != nil || raised.TaskID != "root" || raised.ProjectID != "project" || raised.Resolution != nil {
		t.Fatalf("raise failed: %+v %v", raised, err)
	}
	// The affected task and its descendants refuse new attempts.
	if _, _, err := s.ReserveTask(ctx, taskReservation("root-attempt", "root")); !errors.Is(err, ports.ErrTaskNeedsHumanFenced) {
		t.Fatalf("needs human task admitted a reservation: %v", err)
	}
	if _, _, err := s.ReserveTask(ctx, taskReservation("child-attempt", "child")); !errors.Is(err, ports.ErrTaskNeedsHumanFenced) {
		t.Fatalf("needs human descendant admitted a reservation: %v", err)
	}
	// Unrelated branches remain schedulable.
	if _, _, err := s.ReserveTask(ctx, taskReservation("branch-attempt", "branch")); err != nil {
		t.Fatalf("unrelated branch fenced: %v", err)
	}
	// One pending request per task.
	if _, err := s.RaiseTaskNeedsHuman(ctx, "root", domain.TaskNeedsHuman{ID: "nh-2", ReasonCode: "approval_required", Detail: "Second request", Actor: controlActor(), CreatedAt: time.Now().UTC()}); !errors.Is(err, ports.ErrTaskNeedsHumanConflict) {
		t.Fatalf("duplicate pending accepted: %v", err)
	}
	if ancestor, err := s.NeedsHumanTaskAncestor(ctx, "child"); err != nil || ancestor != "root" {
		t.Fatalf("ancestor not identified: %q %v", ancestor, err)
	}
	if ancestor, err := s.NeedsHumanTaskAncestor(ctx, "branch"); err != nil || ancestor != "" {
		t.Fatalf("unrelated branch flagged: %q %v", ancestor, err)
	}
	// Resolution unblocks exactly the fenced work.
	resolved, err := s.ResolveTaskNeedsHuman(ctx, "root", domain.TaskNeedsHumanResolution{Resolution: "Rotated the credential", Actor: controlActor(), ResolvedAt: time.Date(2026, 9, 19, 13, 0, 0, 0, time.UTC)})
	if err != nil || resolved.Resolution == nil || resolved.Resolution.Resolution != "Rotated the credential" {
		t.Fatalf("resolve failed: %+v %v", resolved, err)
	}
	if _, err := s.ResolveTaskNeedsHuman(ctx, "root", domain.TaskNeedsHumanResolution{Resolution: "Again", Actor: controlActor(), ResolvedAt: time.Now().UTC()}); !errors.Is(err, ports.ErrTaskNeedsHumanNotFound) {
		t.Fatalf("double resolve accepted: %v", err)
	}
	if _, _, err := s.ReserveTask(ctx, taskReservation("root-attempt", "root")); err != nil {
		t.Fatalf("resolved task still fenced: %v", err)
	}
	// History: the resolved request stays readable and a new one may rise.
	items, err := s.ListProjectNeedsHuman(ctx, "project", "", 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("resolved request still listed pending: %+v %v", items, err)
	}
	if _, err := s.RaiseTaskNeedsHuman(ctx, "root", domain.TaskNeedsHuman{ID: "nh-3", ReasonCode: "approval_required", Detail: "Needs a release sign-off", Actor: controlActor(), CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("re-raise after resolve refused: %v", err)
	}
	items, err = s.ListProjectNeedsHuman(ctx, "project", "", 10)
	if err != nil || len(items) != 1 || items[0].ID != "nh-3" {
		t.Fatalf("pending re-raise not listed: %+v %v", items, err)
	}
	if items, err = s.ListProjectNeedsHuman(ctx, "project", items[0].ID, 10); err != nil || len(items) != 0 {
		t.Fatalf("keyset page not bounded: %+v %v", items, err)
	}
	if _, err := s.RaiseTaskNeedsHuman(ctx, "ghost", domain.TaskNeedsHuman{ID: "nh-4", ReasonCode: "approval_required", Detail: "No task", Actor: controlActor(), CreatedAt: time.Now().UTC()}); !errors.Is(err, ports.ErrTaskNotFound) {
		t.Fatalf("unknown task accepted: %v", err)
	}
}

func TestCancelProjectWorkScopesPendingVersusAll(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	seedProject(t, s, "other")
	createTask(t, s, "free", taskDefinition())
	createTask(t, s, "held", taskDefinition())
	createTask(t, s, "already", taskDefinition())
	if _, err := s.CreateAdaptiveTask(ctx, "foreign", "other", taskDefinition(), nil, taskMutation(0)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReserveTask(ctx, taskReservation("held-attempt", "held")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeTaskIntent(ctx, "already", domain.TaskIntentChange{Intent: "cancel", ExpectedVersion: 0, Mutation: taskMutation(1)}); err != nil {
		t.Fatal(err)
	}
	pending, err := s.CancelProjectWork(ctx, domain.ProjectWorkCancellation{
		ProjectID: "project", Scope: "pending", Actor: controlActor(), Reason: "Wrong direction", Now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending.Cancelled) != 1 || pending.Cancelled[0] != "free" || len(pending.Retained) != 1 || pending.Retained[0] != "held" {
		t.Fatalf("pending scope wrong: %+v", pending)
	}
	// The leased task keeps its admission under the pending scope: the same
	// launch intent replays idempotently against its retained reservation.
	if _, _, err := s.ReserveTask(ctx, taskReservation("held-attempt", "held")); err != nil {
		t.Fatalf("leased task lost its reservation path: %v", err)
	}
	// Foreign projects are untouched.
	if intent, err := s.GetTaskIntent(ctx, "foreign"); err != nil || intent.Intent != "run" {
		t.Fatalf("foreign task cancelled: %+v %v", intent, err)
	}
	// Cancel-all marks leased work cancelling too. The already-cancelled task
	// is not a new control event; only the leased survivor is newly marked.
	all, err := s.CancelProjectWork(ctx, domain.ProjectWorkCancellation{
		ProjectID: "project", Scope: "all", Actor: controlActor(), Reason: "Stop everything", Now: time.Date(2026, 9, 19, 12, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Cancelled) != 1 || all.Cancelled[0] != "held" || len(all.Retained) != 0 {
		t.Fatalf("all scope wrong: %+v", all)
	}
	for _, id := range []string{"held", "free"} {
		intent, err := s.GetTaskIntent(ctx, id)
		if err != nil || intent.Intent != "cancel" {
			t.Fatalf("cancel-all left %s running: %+v %v", id, intent, err)
		}
	}
	// The retained lease is never assumed stopped: cancellation only marks
	// intent; reconciliation releases ownership.
	if _, _, err := s.ReserveTask(ctx, taskReservation("held-third", "held")); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("cancelled lease admitted a new attempt: %v", err)
	}
	// Invalid scopes and unknown projects refuse.
	if _, err := s.CancelProjectWork(ctx, domain.ProjectWorkCancellation{ProjectID: "project", Scope: "some", Actor: controlActor(), Reason: "Bad scope", Now: time.Now().UTC()}); !errors.Is(err, ports.ErrProjectControlInvalid) {
		t.Fatalf("invalid scope accepted: %v", err)
	}
	if _, err := s.CancelProjectWork(ctx, domain.ProjectWorkCancellation{ProjectID: "ghost", Scope: "pending", Actor: controlActor(), Reason: "No project", Now: time.Now().UTC()}); !errors.Is(err, ports.ErrProjectControlNotFound) {
		t.Fatalf("unknown project accepted: %v", err)
	}
}
