package task_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	tasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/task"
)

type messageServiceStore struct {
	*resultServiceStore
	message *domain.TaskMessageSubmission
}

func (s *messageServiceStore) SubmitTaskMessage(_ context.Context, input domain.TaskMessageSubmission) (domain.TaskMessage, bool, error) {
	s.message = &input
	return domain.TaskMessage{ID: input.ID}, s.writeErr == nil, s.writeErr
}

func TestTaskMessageServiceDerivesNativeIdentityAndPreservesFailures(t *testing.T) {
	ctx := context.Background()
	input := tasksvc.MessageInput{SourceGeneration: "current", IdempotencyKey: "message", Definition: domain.TaskMessageDefinition{SchemaVersion: 1, Kind: "finding", TargetTaskID: "target", Subject: "Discovery", Body: "An observation", CorrelationID: "thread"}}
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			store := &messageServiceStore{resultServiceStore: &resultServiceStore{rec: domain.SessionRecord{ID: "worker", Kind: domain.KindWorker, Harness: domain.HarnessCodex, Mode: mode, Metadata: domain.SessionMetadata{RuntimeLaunchID: "current", ControllerGeneration: "current"}}}}
			svc := tasksvc.New(store)
			if _, createdErr := svc.SubmitMessage(ctx, "worker", input); createdErr != nil || store.message == nil || store.message.AttemptID != "assigned-attempt" || store.message.SourceOwner != store.rec.ControllerOwner() || store.message.ExpectedActivation != 7 {
				t.Fatalf("untrusted attribution: %+v %v", store.message, createdErr)
			}
			store.message = nil
			bad := input
			bad.SourceGeneration = "old"
			if _, err := svc.SubmitMessage(ctx, "worker", bad); err == nil || store.message != nil {
				t.Fatalf("stale native message: %v", err)
			}
			bad = input
			bad.IdempotencyKey = ""
			if _, err := svc.SubmitMessage(ctx, "worker", bad); err == nil || store.message != nil {
				t.Fatalf("unkeyed message: %v", err)
			}
			failure := errors.New("database unavailable")
			store.readErr = failure
			if _, err := svc.SubmitMessage(ctx, "worker", input); !errors.Is(err, failure) {
				t.Fatalf("hidden database failure: %v", err)
			}
			store.readErr, store.writeErr = nil, ports.ErrTaskConflict
			_, err := svc.SubmitMessage(ctx, "worker", input)
			var problem *apierr.Error
			if !errors.As(err, &problem) || problem.Code != "TASK_MESSAGE_CONFLICT" {
				t.Fatalf("lost conflict envelope: %v", err)
			}
		})
	}
}
