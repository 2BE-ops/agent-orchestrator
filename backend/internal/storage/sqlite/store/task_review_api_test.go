package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	reviewsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/review"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type taskReviewResolver struct {
	ports.WorkerConfigurationResolver
	snapshot domain.WorkerConfiguration
	calls    int
	actor    domain.RegistryActor
}

func (r *taskReviewResolver) ResolveWorker(_ context.Context, selection domain.WorkerSelection, _ domain.ProjectRecord, _ domain.SessionMode, actor domain.RegistryActor) (domain.WorkerConfiguration, error) {
	r.calls++
	r.actor = actor
	snapshot := r.snapshot
	snapshot.Selection, snapshot.Origin, snapshot.ActorID = selection, actor.Origin, actor.ID
	return snapshot, nil
}

type taskReviewNative struct {
	reviewcore.Launcher
	t          *testing.T
	store      *sqlite.Store
	spawns     int
	spawnError error
}

func (n *taskReviewNative) Preflight(context.Context, domain.ReviewerHarness, string) error {
	return nil
}
func (n *taskReviewNative) Alive(context.Context, string) (bool, error) { return true, nil }
func (n *taskReviewNative) Destroy(context.Context, string) error       { return nil }
func (n *taskReviewNative) Spawn(ctx context.Context, spec reviewcore.LaunchSpec) (reviewcore.LaunchResult, error) {
	n.t.Helper()
	n.spawns++
	retained, found, err := n.store.GetTaskReviewContext(ctx, spec.RunID)
	if err != nil || !found || spec.TaskContext == nil || retained.Context.ContentHash != spec.TaskContext.ContentHash || retained.StartedAt != nil || spec.AgentSessionID != "" {
		n.t.Fatalf("native launch bypassed persisted context: %+v %v %v", retained, found, err)
	}
	return reviewcore.LaunchResult{HandleID: "native-review", LaunchID: spec.LaunchID, AgentSessionID: "native-thread"}, n.spawnError
}

func TestTaskReviewAPIUsesFrozenPolicyAndInspectsNativeProvenance(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, _, frozen := taskReviewFixture(t, s)
	worker, _, err := s.GetSession(ctx, frozen.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Metadata.WorkspacePath = t.TempDir()
	if err := s.UpdateSession(ctx, worker); err != nil {
		t.Fatal(err)
	}
	resolver := &taskReviewResolver{snapshot: frozen.Reviewer}
	native := &taskReviewNative{t: t, store: s}
	engine := reviewcore.New(reviewcore.Deps{Store: s, Sessions: s, PRs: s, Projects: s, Launcher: native})
	reviews := reviewsvc.New(engine, s)
	tasks := tasksvc.New(s, tasksvc.WithNativeReviews(resolver, reviews))
	router := chi.NewRouter()
	(&controllers.AdaptiveTasksController{Svc: tasks}).Register(router)
	request := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("%s %s = %d, want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	base := "/tasks/" + frozen.TaskID + "/attempts/" + frozen.AttemptID
	input := `{"resultId":"` + frozen.ResultID + `"}`
	for _, body := range []string{`{`, `null`, `{}`, `{"resultId":"x","actor":{"kind":"SYSTEM"}}`, `{"resultId":"x","reviewer":{}}`, `{} {}`, strings.Repeat("x", (8<<10)+1)} {
		request(http.MethodPost, base+"/reviews", body, http.StatusBadRequest)
	}
	if _, err := tasks.RequestReview(ctx, domain.AdaptiveActor{Kind: "WORKER", ID: "worker", SessionID: frozen.SessionID}, frozen.TaskID, frozen.AttemptID, tasksvc.ReviewInput{ResultID: frozen.ResultID}); err == nil || resolver.calls != 0 || native.spawns != 0 {
		t.Fatalf("worker requested its own review: %v", err)
	}
	request(http.MethodPost, "/tasks/other/attempts/"+frozen.AttemptID+"/reviews", input, http.StatusNotFound)
	w := request(http.MethodPost, base+"/reviews", input, http.StatusOK)
	var receipt domain.TaskReviewReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || !receipt.Created || len(receipt.Runs) != 1 || native.spawns != 1 || resolver.actor.Origin != domain.RegistryUser {
		t.Fatalf("native review admission: %+v %v", receipt, err)
	}
	run := receipt.Runs[0]
	w = request(http.MethodGet, base+"/reviews/"+run.ID, "", http.StatusOK)
	var view tasksvc.ReviewView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Snapshot.StartedAt == nil || view.Snapshot.Context.Reviewer.AgentType != frozen.Reviewer.AgentType || view.Snapshot.Context.Reviewer.Selection.Version != 1 || view.Snapshot.Context.CriteriaHash != frozen.CriteriaHash || view.Snapshot.Context.ResultID != frozen.ResultID || view.Snapshot.Context.Actor.Kind != "USER" {
		t.Fatalf("lost frozen provenance: %+v %v", view, err)
	}
	retainedHash := view.Snapshot.Context.ContentHash
	for _, prefix := range []string{"/tasks/other/attempts/" + frozen.AttemptID, "/tasks/" + frozen.TaskID + "/attempts/other"} {
		request(http.MethodGet, prefix+"/reviews/"+run.ID, "", http.StatusNotFound)
		request(http.MethodGet, prefix+"/results/"+frozen.ResultID+"/reviews", "", http.StatusNotFound)
	}
	w = request(http.MethodGet, base+"/results/"+frozen.ResultID+"/reviews", "", http.StatusOK)
	var history controllers.TaskReviewsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil || len(history.Items) != 1 || history.Items[0].ID != run.ID {
		t.Fatalf("history: %+v %v", history, err)
	}
	w = request(http.MethodPost, base+"/reviews", input, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.Created || native.spawns != 1 || len(receipt.Runs) != 1 || receipt.Runs[0].ID != run.ID {
		t.Fatalf("running retry duplicated review: %+v %v", receipt, err)
	}
	if _, err := reviews.SubmitMany(ctx, frozen.SessionID, []reviewsvc.SubmittedReview{{RunID: run.ID, SourceGeneration: view.Snapshot.Context.LaunchID, Verdict: domain.VerdictApproved, Body: "No blocking findings"}}); err != nil {
		t.Fatal(err)
	}
	request(http.MethodPost, base+"/evaluations", `{"resultId":"`+frozen.ResultID+`","idempotencyKey":"acceptance","reason":"Assess exact result"}`, http.StatusOK)
	w = request(http.MethodGet, "/tasks/"+frozen.TaskID, "", http.StatusOK)
	var taskView controllers.AdaptiveTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &taskView); err != nil || !taskView.Completion.Verified || taskView.State.Phase != "completed" || taskView.Lease == nil {
		t.Fatalf("completion API lost evidence or ownership: %+v %v", taskView, err)
	}
	w = request(http.MethodPost, base+"/reviews", input, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.Created || native.spawns != 1 {
		t.Fatalf("approved retry duplicated review: %+v %v", receipt, err)
	}
	metadata := registryMetadata("Disabled after review")
	metadata.Enabled = false
	if _, err := s.UpdateRegistryMetadata(ctx, frozen.Reviewer.AgentType.ID, metadata, registryMutation(domain.RegistryUser, 1)); err != nil {
		t.Fatal(err)
	}
	w = request(http.MethodGet, base+"/reviews/"+run.ID, "", http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Snapshot.Context.ContentHash != retainedHash || view.Run.Verdict != domain.VerdictApproved {
		t.Fatalf("live Type changed retained history: %+v %v", view, err)
	}
}

func TestTaskReviewRequestFailureRetainsPass(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		failure    error
		status     domain.ReviewRunStatus
	}{
		{"uncertain", "TASK_REVIEW_RECONCILE_REQUIRED", reviewcore.ErrTaskReviewLaunchUncertain, domain.ReviewRunRunning},
		{"missing binary", "REVIEWER_BINARY_NOT_FOUND", ports.ErrAgentBinaryNotFound, domain.ReviewRunFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			_, _, frozen := taskReviewFixture(t, s)
			worker, _, err := s.GetSession(ctx, frozen.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			worker.Metadata.WorkspacePath = t.TempDir()
			if err := s.UpdateSession(ctx, worker); err != nil {
				t.Fatal(err)
			}
			native := &taskReviewNative{t: t, store: s, spawnError: tc.failure}
			reviews := reviewsvc.New(reviewcore.New(reviewcore.Deps{Store: s, Sessions: s, PRs: s, Projects: s, Launcher: native}), s)
			tasks := tasksvc.New(s, tasksvc.WithNativeReviews(&taskReviewResolver{snapshot: frozen.Reviewer}, reviews))
			input := tasksvc.ReviewInput{ResultID: frozen.ResultID}
			_, err = tasks.RequestReview(ctx, frozen.Actor, frozen.TaskID, frozen.AttemptID, input)
			var problem *apierr.Error
			if !errors.As(err, &problem) || problem.Code != tc.code {
				t.Fatalf("failure envelope: %v", err)
			}
			runs, err := tasks.Reviews(ctx, frozen.TaskID, frozen.AttemptID, frozen.ResultID)
			if err != nil || len(runs) != 1 || runs[0].Status != tc.status || native.spawns != 1 {
				t.Fatalf("lost attempted native pass: %+v %v", runs, err)
			}
			view, err := tasks.Review(ctx, frozen.TaskID, frozen.AttemptID, runs[0].ID)
			if err != nil || view.Snapshot.StartedAt != nil {
				t.Fatalf("failure invented launch witness: %+v %v", view, err)
			}
			if tc.status == domain.ReviewRunRunning {
				if receipt, err := tasks.RequestReview(ctx, frozen.Actor, frozen.TaskID, frozen.AttemptID, input); err != nil || receipt.Created || native.spawns != 1 {
					t.Fatalf("uncertain retry launched again: %+v %v", receipt, err)
				}
			}
		})
	}
}

type taskReviewAccountGate struct {
	ports.CodexOperationGate
	failure error
	calls   int
}

func (g *taskReviewAccountGate) AcquireSharedWait(context.Context) (func(), error) {
	g.calls++
	return nil, g.failure
}

func TestTaskReviewRequestRespectsCodexAccountGate(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, _, frozen := taskReviewFixture(t, s)
	frozen.Reviewer.Effective.Harness = domain.HarnessCodex
	frozen.Reviewer.ContentHash = frozen.Reviewer.Hash()
	frozen.Criteria.ReviewPolicy.DifferentHarness = false
	_, frozen.CriteriaHash, _ = domain.TaskContent(frozen.Criteria)
	frozen.ContentHash = frozen.Hash()
	gate := &taskReviewAccountGate{failure: errors.New("account switch owns admission")}
	svc := reviewsvc.New(reviewcore.New(reviewcore.Deps{}), s, reviewsvc.WithCodexAccountOperationGate(gate))
	if _, err := svc.RequestTaskReview(ctx, frozen); !errors.Is(err, gate.failure) || gate.calls != 1 {
		t.Fatalf("Codex gate bypassed: %v calls=%d", err, gate.calls)
	}
	runs, err := s.ListTaskReviewRuns(ctx, frozen.ResultID)
	if err != nil || len(runs) != 0 {
		t.Fatalf("gated request started native work: %+v %v", runs, err)
	}
}
