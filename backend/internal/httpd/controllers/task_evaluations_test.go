package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

func TestTaskEvaluationsAPICollectsEvidenceAndRetainsScopedHistory(t *testing.T) {
	router, s, taskID, worker := resultAPIFixture(t)
	w := registryRequest(t, router, http.MethodPost, "/sessions/"+string(worker)+"/task-results", resultAPIInput(), http.StatusOK)
	var result controllers.TaskResultSubmitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	base := "/tasks/" + taskID + "/attempts/result-attempt/evaluations"
	input := controllers.TaskEvaluateRequest{ResultID: result.Result.ID, IdempotencyKey: "evaluate-1", Reason: "Assess frozen acceptance criteria"}
	w = registryRequest(t, router, http.MethodPost, base, input, http.StatusOK)
	var first controllers.TaskEvaluateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || !first.Created || first.Evaluation.Definition.Outcome != "inconclusive" || first.Evaluation.ResultID != result.Result.ID || first.Evaluation.ContextHash != result.Result.ContextHash || first.Evaluation.Attribution.ConfigurationHash != result.Result.ConfigurationHash || first.Evaluation.Actor.Kind != "USER" {
		t.Fatalf("worker claim became verified evidence or lost attribution: %+v %v", first, err)
	}
	if first.Evaluation.Definition.Observations == nil || first.Evaluation.Definition.Observations.Worker.SessionID != worker || !first.Evaluation.Definition.Observations.Worker.ReservationOngoing {
		t.Fatalf("HTTP lost observed lifecycle attribution: %+v", first.Evaluation.Definition)
	}
	w = registryRequest(t, router, http.MethodPost, base, input, http.StatusOK)
	var retry controllers.TaskEvaluateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &retry); err != nil || retry.Created || retry.Evaluation.ContentHash != first.Evaluation.ContentHash {
		t.Fatalf("retry recollected evidence: %+v %v", retry, err)
	}
	input.Reason = "Different request"
	registryRequest(t, router, http.MethodPost, base, input, http.StatusConflict)
	input.IdempotencyKey = "evaluate-2"
	registryRequest(t, router, http.MethodPost, base, input, http.StatusConflict)
	input.ExpectedVersion = 1
	registryRequest(t, router, http.MethodPost, base, input, http.StatusOK)
	w = registryRequest(t, router, http.MethodGet, base+"?limit=1", nil, http.StatusOK)
	var page controllers.TaskEvaluationsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.NextCursor != "1" || page.Items[0].ID != first.Evaluation.ID {
		t.Fatalf("first page: %+v %v", page, err)
	}
	w = registryRequest(t, router, http.MethodGet, base+"?cursor=1", nil, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.Items[0].Number != 2 {
		t.Fatalf("next page: %+v %v", page, err)
	}
	w = registryRequest(t, router, http.MethodGet, base+"/"+first.Evaluation.ID, nil, http.StatusOK)
	if !strings.Contains(w.Body.String(), first.Evaluation.ContentHash) {
		t.Fatal("historical evidence changed")
	}
	for _, path := range []string{"/tasks/other/attempts/result-attempt/evaluations", "/tasks/" + taskID + "/attempts/other/evaluations"} {
		registryRequest(t, router, http.MethodGet, path, nil, http.StatusNotFound)
		registryRequest(t, router, http.MethodGet, path+"/"+first.Evaluation.ID, nil, http.StatusNotFound)
		registryRequest(t, router, http.MethodPost, path, input, http.StatusNotFound)
	}
	registryRequest(t, router, http.MethodGet, base+"/missing", nil, http.StatusNotFound)
	for _, query := range []string{"?limit=101", "?cursor=-1", "?cursor=invalid"} {
		registryRequest(t, router, http.MethodGet, base+query, nil, http.StatusBadRequest)
	}
	correction := resultAPIInput()
	correction.ExpectedVersion, correction.IdempotencyKey = 1, "correction"
	registryRequest(t, router, http.MethodPost, "/sessions/"+string(worker)+"/task-results", correction, http.StatusOK)
	input.ExpectedVersion, input.IdempotencyKey = 2, "obsolete-result"
	registryRequest(t, router, http.MethodPost, base, input, http.StatusConflict)
	w = registryRequest(t, router, http.MethodGet, "/tasks/"+taskID, nil, http.StatusOK)
	var task controllers.AdaptiveTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil || task.Lease == nil || task.State.Phase != "working" || task.Task.Revision != 1 {
		t.Fatalf("assessment changed ownership or planning: %+v %v", task, err)
	}
	audit, err := s.ListTaskAudit(context.Background(), taskID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range audit {
		if event.Action == "evaluation_recorded" {
			count++
			if event.Actor.Kind != "USER" {
				t.Fatalf("wrong authority: %+v", event)
			}
		}
	}
	if count != 2 {
		t.Fatalf("evaluation audit count: %d", count)
	}
}

func TestTaskEvaluationsAPIRejectsCallerVerdictsAndUnboundedInput(t *testing.T) {
	router, s, taskID, _ := resultAPIFixture(t)
	base := "/tasks/" + taskID + "/attempts/result-attempt/evaluations"
	for _, body := range []string{`{`, `null`, `{}`, `{"actor":{"kind":"SYSTEM"}}`, `{"definition":{"outcome":"passed"}}`, `{"outcome":"passed"}`, `{} {}`, strings.Repeat("x", (8<<10)+1)} {
		registryRequest(t, router, http.MethodPost, base, body, http.StatusBadRequest)
	}
	if _, found, err := s.LatestTaskEvaluation(context.Background(), "result-attempt"); err != nil || found {
		t.Fatalf("invalid input retained evaluation: %v %v", found, err)
	}
}
