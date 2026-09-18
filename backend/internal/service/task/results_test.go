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

type resultServiceStore struct {
	tasksvc.Store
	rec               domain.SessionRecord
	captured          *domain.TaskResultSubmission
	readErr, writeErr error
}

func (s *resultServiceStore) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return s.rec, true, s.readErr
}
func (s *resultServiceStore) GetTaskWorkerDispatchBySession(context.Context, domain.SessionID) (domain.TaskWorkerDispatch, bool, error) {
	return domain.TaskWorkerDispatch{AttemptID: "assigned-attempt", SessionID: s.rec.ID}, true, nil
}
func (s *resultServiceStore) GetEffectiveWorkerConfiguration(context.Context, domain.SessionID) (domain.WorkerConfiguration, int64, bool, error) {
	return domain.WorkerConfiguration{}, 7, true, nil
}
func (s *resultServiceStore) SubmitTaskResult(_ context.Context, input domain.TaskResultSubmission) (domain.TaskResult, bool, error) {
	s.captured = &input
	return domain.TaskResult{ID: input.ID}, s.writeErr == nil, s.writeErr
}

func TestTaskResultServiceDerivesControllerAttributionAndPreservesFailure(t *testing.T) {
	ctx := context.Background()
	input := tasksvc.ResultInput{SourceGeneration: "generation", IdempotencyKey: "submission", Definition: domain.TaskResultDefinition{SchemaVersion: 1, ClaimedOutcome: "partial", Summary: "Partial work retained"}}
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			store := &resultServiceStore{rec: domain.SessionRecord{ID: "worker", Kind: domain.KindWorker, Mode: mode, Harness: domain.HarnessCodex, Metadata: domain.SessionMetadata{RuntimeLaunchID: "generation", ControllerGeneration: "generation"}}}
			svc := tasksvc.New(store)
			receipt, err := svc.SubmitResult(ctx, "worker", input)
			if err != nil || !receipt.Created || store.captured == nil || store.captured.ID == "" || store.captured.AttemptID != "assigned-attempt" || store.captured.SourceOwner != store.rec.ControllerOwner() || store.captured.ExpectedActivation != 7 {
				t.Fatalf("attribution was not server derived: %+v %v", store.captured, err)
			}
			store.captured = nil
			bad := input
			bad.SourceGeneration = "stale"
			if _, err := svc.SubmitResult(ctx, "worker", bad); err == nil || store.captured != nil {
				t.Fatalf("stale claim reached storage: %v", err)
			}
			bad = input
			bad.IdempotencyKey = ""
			if _, err := svc.SubmitResult(ctx, "worker", bad); err == nil || store.captured != nil {
				t.Fatalf("unkeyed claim reached storage: %v", err)
			}
			failure := errors.New("database unavailable")
			store.readErr = failure
			if _, err := svc.SubmitResult(ctx, "worker", input); !errors.Is(err, failure) {
				t.Fatalf("read failure disguised: %v", err)
			}
			store.readErr, store.writeErr = nil, ports.ErrTaskConflict
			_, err = svc.SubmitResult(ctx, "worker", input)
			var problem *apierr.Error
			if !errors.As(err, &problem) || problem.Code != "TASK_RESULT_CONFLICT" {
				t.Fatalf("write conflict envelope: %v", err)
			}
		})
	}
}
