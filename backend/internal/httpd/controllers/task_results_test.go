package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/taskcontext"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func resultAPIFixture(t *testing.T) (http.Handler, *sqlite.Store, string, domain.SessionID) {
	t.Helper()
	ctx, now := context.Background(), time.Now().UTC()
	router, s, _ := adaptiveTaskRouter(t)
	task := createAdaptiveTaskHTTP(t, router)
	definition := domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeTUI, Config: domain.AgentConfig{Permissions: domain.PermissionModeAuto}, MaxParallelWorkers: 1}}
	entry, err := s.CreateRegistryEntry(ctx, "result-type", domain.RegistryAgentType, domain.RegistryMetadata{Name: "Result worker", Enabled: true}, definition, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Configure test worker"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.GetRegistryVersion(ctx, entry.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	configuration := domain.WorkerConfiguration{SchemaVersion: 1, AgentType: domain.WorkerDefinitionRef{ID: entry.ID, Version: 1, Name: entry.Metadata.Name, ContentHash: version.ContentHash}, Selection: domain.WorkerSelection{AgentTypeID: entry.ID}, Effective: *definition.AgentType, Origin: domain.RegistryUser, ActorID: "human", SystemPrompt: "Perform the pinned task", CreatedAt: now}
	configuration.ContentHash = configuration.Hash()
	_, lease, err := s.ReserveTask(ctx, domain.TaskReservation{ID: "result-attempt", TaskID: task.Task.ID, LaunchIntentID: "result-launch", HolderID: "scheduler", Mutation: domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Prepare result test", ExpectedRevision: 1}, Now: now, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, domain.SessionRecord{ProjectID: "project", Kind: domain.KindWorker, Harness: domain.HarnessCodex, Mode: domain.SessionModeTUI, Metadata: domain.SessionMetadata{Permissions: domain.PermissionModeAuto}, CreatedAt: now, UpdatedAt: now}, configuration, now)
	if err != nil {
		t.Fatal(err)
	}
	op := domain.TaskExecutionOperation{ID: "result-native", SessionID: rec.ID, Lease: lease.TaskLeaseToken, SourceOwner: rec.ControllerOwner(), Kind: "dispatch", CreatedAt: now}
	if _, err := s.BeginTaskExecution(ctx, op); err != nil {
		t.Fatal(err)
	}
	if _, err := taskcontext.New(s).Build(ctx, ports.TaskContextRequest{Lease: lease.TaskLeaseToken, SessionID: rec.ID, ExecutionOperationID: op.ID, WorkspacePath: t.TempDir(), Prompt: "Work", SystemPrompt: configuration.SystemPrompt}); err != nil {
		t.Fatal(err)
	}
	rec.Metadata.RuntimeLaunchID = op.ID
	if err := s.UpdateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveTaskExecution(ctx, domain.TaskExecutionResolution{OperationID: op.ID, ObservedOwner: rec.ControllerOwner(), Outcome: "connected", Reason: "Test controller ready", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return router, s, task.Task.ID, rec.ID
}

func resultAPIInput() controllers.TaskResultSubmitRequest {
	return controllers.TaskResultSubmitRequest{SourceGeneration: "result-native", IdempotencyKey: "first-result", Definition: domain.TaskResultDefinition{SchemaVersion: 1, ClaimedOutcome: "completed", Summary: "Worker claims implementation complete", Tests: []domain.TaskTestClaim{{Command: []string{"go", "test", "./..."}, Outcome: "passed", Details: "Self-reported"}}}}
}

func TestTaskResultsAPIRetainsIdempotentClaimsAndScopedHistory(t *testing.T) {
	router, s, taskID, worker := resultAPIFixture(t)
	path := "/sessions/" + string(worker) + "/task-results"
	input := resultAPIInput()
	w := registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	var first controllers.TaskResultSubmitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || !first.Created || first.Result.TaskID != taskID || first.Result.AttemptID != "result-attempt" || first.Result.SessionID != worker || first.Result.ContextHash == "" {
		t.Fatalf("result attribution: %+v %v", first, err)
	}
	w = registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	var replay controllers.TaskResultSubmitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil || replay.Created || replay.Result.ID != first.Result.ID {
		t.Fatalf("retry duplicated result: %+v %v", replay, err)
	}
	input.Definition.Summary = "Correction"
	registryRequest(t, router, http.MethodPost, path, input, http.StatusConflict)
	input.ExpectedVersion, input.IdempotencyKey = 1, "corrected-result"
	registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	base := "/tasks/" + taskID + "/attempts/result-attempt/results"
	w = registryRequest(t, router, http.MethodGet, base+"?limit=1", nil, http.StatusOK)
	var page controllers.TaskResultsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.NextCursor != "1" {
		t.Fatalf("result history page: %+v %v", page, err)
	}
	registryRequest(t, router, http.MethodGet, base+"?cursor=1", nil, http.StatusOK)
	w = registryRequest(t, router, http.MethodGet, base+"/"+first.Result.ID, nil, http.StatusOK)
	if !strings.Contains(w.Body.String(), "Worker claims implementation complete") {
		t.Fatal("historical claim overwritten")
	}
	registryRequest(t, router, http.MethodGet, "/tasks/other/attempts/result-attempt/results/"+first.Result.ID, nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, "/tasks/other/attempts/result-attempt/results", nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, base+"/missing", nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, base+"?limit=101", nil, http.StatusBadRequest)
	w = registryRequest(t, router, http.MethodGet, "/tasks/"+taskID, nil, http.StatusOK)
	var task controllers.AdaptiveTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil || task.State.Phase != "working" || task.Lease == nil {
		t.Fatalf("worker claim declared completion: %+v %v", task, err)
	}
	audit, err := s.ListTaskAudit(context.Background(), taskID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range audit {
		if event.Action == "result_submitted" && (event.Actor.Kind != "WORKER" || event.Actor.SessionID != worker) {
			t.Fatalf("forged result authority: %+v", event)
		}
	}
}

func TestTaskResultsAPIRejectsMalformedForgedAndStaleSubmissions(t *testing.T) {
	router, s, _, worker := resultAPIFixture(t)
	path := "/sessions/" + string(worker) + "/task-results"
	for _, body := range []string{`{`, `null`, `{}`, `{"actor":{"kind":"USER"}}`, `{"definition":{"schemaVersion":1,"verified":true}}`, `{} {}`, strings.Repeat("x", (512<<10)+1)} {
		registryRequest(t, router, http.MethodPost, path, body, http.StatusBadRequest)
	}
	input := resultAPIInput()
	input.SourceGeneration = "stale"
	registryRequest(t, router, http.MethodPost, path, input, http.StatusConflict)
	input.SourceGeneration = "result-native"
	input.Definition.ClaimedCommit = "main"
	registryRequest(t, router, http.MethodPost, path, input, http.StatusBadRequest)
	input = resultAPIInput()
	input.ExpectedVersion = 16
	registryRequest(t, router, http.MethodPost, path, input, http.StatusBadRequest)
	input = resultAPIInput()
	registryRequest(t, router, http.MethodPost, "/sessions/missing/task-results", input, http.StatusNotFound)
	if _, found, err := s.LatestTaskResult(context.Background(), "result-attempt"); err != nil || found {
		t.Fatalf("invalid submission left a result: %v %v", found, err)
	}
}
