package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

func messageAPIInput(target string) controllers.TaskMessageSubmitRequest {
	return controllers.TaskMessageSubmitRequest{SourceGeneration: "result-native", IdempotencyKey: "first-message", Definition: domain.TaskMessageDefinition{SchemaVersion: 1, Kind: "interface_contract", TargetTaskID: target, Subject: "Shared interface", Body: "Proposed interface awaiting review", CorrelationID: "thread", Interface: &domain.TaskInterfaceClaim{Name: "API", Contract: "Return a versioned identifier", Files: []string{"api/schema.json"}}}}
}

func TestTaskMessagesAPIStoresBeforeDeliveryAndScopesTheTimeline(t *testing.T) {
	router, s, sourceTask, worker := resultAPIFixture(t)
	target := createAdaptiveTaskHTTP(t, router)
	input := messageAPIInput(target.Task.ID)
	path := "/sessions/" + string(worker) + "/task-messages"
	w := registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	var first controllers.TaskMessageSubmitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || !first.Created || first.Message.SessionID != worker || first.Message.TaskID != sourceTask || first.Message.ContextHash == "" || first.Message.Definition.Interface == nil {
		t.Fatalf("message attribution: %+v %v", first, err)
	}
	w = registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	var replay controllers.TaskMessageSubmitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil || replay.Created || replay.Message.ID != first.Message.ID {
		t.Fatalf("retry: %+v %v", replay, err)
	}
	input.Definition.Body = "Conflicting content"
	registryRequest(t, router, http.MethodPost, path, input, http.StatusConflict)
	input.IdempotencyKey = "second-message"
	registryRequest(t, router, http.MethodPost, path, input, http.StatusOK)
	base := "/projects/project/task-messages"
	for _, taskID := range []string{sourceTask, target.Task.ID} {
		w = registryRequest(t, router, http.MethodGet, base+"?taskId="+taskID+"&limit=1", nil, http.StatusOK)
		var page controllers.TaskMessagesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.NextCursor != strconv.FormatInt(first.Message.Sequence, 10) {
			t.Fatalf("shared timeline: %+v %v", page, err)
		}
		w = registryRequest(t, router, http.MethodGet, base+"?taskId="+taskID+"&cursor="+page.NextCursor, nil, http.StatusOK)
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.Items[0].ID == first.Message.ID {
			t.Fatalf("timeline cursor: %+v %v", page, err)
		}
	}
	w = registryRequest(t, router, http.MethodGet, base+"/"+first.Message.ID, nil, http.StatusOK)
	var view controllers.TaskMessageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Message.ContentHash != first.Message.ContentHash || len(view.Deliveries) != 0 || !strings.Contains(w.Body.String(), `"deliveries":[]`) {
		t.Fatalf("stored message pretended delivery: %+v %v", view, err)
	}
	registryRequest(t, router, http.MethodGet, "/projects/other/task-messages/"+first.Message.ID, nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, "/projects/other/task-messages?taskId="+sourceTask, nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, "/projects/missing/task-messages", nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, base+"/missing", nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, base+"?taskId=missing", nil, http.StatusNotFound)
	registryRequest(t, router, http.MethodGet, base+"?limit=101", nil, http.StatusBadRequest)
	registryRequest(t, router, http.MethodGet, base+"?cursor=-1", nil, http.StatusBadRequest)
	pending, err := s.ListPendingTaskMessages(context.Background(), 0, 100)
	if err != nil || len(pending) != 2 {
		t.Fatalf("messages not retained for later delivery: %+v %v", pending, err)
	}
}

func TestTaskMessagesAPIRejectsMalformedAndForgedAuthority(t *testing.T) {
	router, s, taskID, worker := resultAPIFixture(t)
	target := createAdaptiveTaskHTTP(t, router)
	path := "/sessions/" + string(worker) + "/task-messages"
	for _, body := range []string{`{`, `null`, `{}`, `{"actor":{"kind":"USER"}}`, `{"sourceOwner":{}}`, `{"definition":{"kind":"finding","verified":true}}`, `{} {}`, strings.Repeat("x", (64<<10)+1)} {
		registryRequest(t, router, http.MethodPost, path, body, http.StatusBadRequest)
	}
	input := messageAPIInput(target.Task.ID)
	input.SourceGeneration = "stale"
	registryRequest(t, router, http.MethodPost, path, input, http.StatusConflict)
	input.SourceGeneration = "result-native"
	input.Definition.TargetTaskID = taskID
	registryRequest(t, router, http.MethodPost, path, input, http.StatusBadRequest)
	input.Definition.TargetTaskID = "missing"
	registryRequest(t, router, http.MethodPost, path, input, http.StatusNotFound)
	w := registryRequest(t, router, http.MethodPost, "/projects/other/tasks", adaptiveTaskInput(), http.StatusCreated)
	var foreign controllers.AdaptiveTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &foreign); err != nil {
		t.Fatal(err)
	}
	input.Definition.TargetTaskID = foreign.Task.ID
	registryRequest(t, router, http.MethodPost, path, input, http.StatusBadRequest)
	input = messageAPIInput(target.Task.ID)
	registryRequest(t, router, http.MethodPost, "/sessions/missing/task-messages", input, http.StatusNotFound)
	items, err := s.ListTaskMessages(context.Background(), "project", "", 0, 100)
	if err != nil || len(items) != 0 {
		t.Fatalf("invalid message persisted: %+v %v", items, err)
	}
}
