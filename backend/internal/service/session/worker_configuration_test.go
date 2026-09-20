package session

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type workerHistoryStore struct {
	*fakeStore
	ports.WorkerExecutionStore
	original  domain.WorkerConfiguration
	execution domain.WorkerExecution
	events    []domain.WorkerExecutionActivation
}

func (s *workerHistoryStore) GetWorkerConfiguration(_ context.Context, id domain.SessionID) (domain.WorkerConfiguration, bool, error) {
	return s.original, id == "worker", nil
}
func (s *workerHistoryStore) GetEffectiveWorkerConfiguration(context.Context, domain.SessionID) (domain.WorkerConfiguration, int64, bool, error) {
	return s.original, 2, true, nil
}
func (s *workerHistoryStore) ListWorkerExecutions(_ context.Context, _ domain.SessionID, after int64, limit int) ([]domain.WorkerExecutionActivation, error) {
	var events []domain.WorkerExecutionActivation
	for _, event := range s.events {
		if event.Sequence > after && len(events) < limit {
			events = append(events, event)
		}
	}
	return events, nil
}
func (s *workerHistoryStore) GetWorkerExecution(_ context.Context, sessionID domain.SessionID, id string) (domain.WorkerExecution, error) {
	if sessionID != "worker" || id != s.execution.ID {
		return domain.WorkerExecution{}, sql.ErrNoRows
	}
	return s.execution, nil
}

func TestWorkerExecutionHistoryUsesRollbackDestinationAndBounds(t *testing.T) {
	ctx := context.Background()
	store := &workerHistoryStore{fakeStore: newFakeStore(), original: domain.WorkerConfiguration{ContentHash: "original", Effective: domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeTUI}}}
	store.sessions["worker"] = domain.SessionRecord{ID: "worker"}
	store.sessions["legacy"] = domain.SessionRecord{ID: "legacy"}
	store.execution = domain.WorkerExecution{ID: "change", SessionID: "worker", Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Use Chat", SourceKind: "interface_transition", Configuration: domain.WorkerConfiguration{ContentHash: "changed", Effective: domain.AgentTypeDefinition{Harness: domain.HarnessCodex, SessionMode: domain.SessionModeChat}}}
	store.events = []domain.WorkerExecutionActivation{{Sequence: 1, SessionID: "worker", ExecutionID: "change", OperationID: "change", Action: "applied"}, {Sequence: 2, SessionID: "worker", OperationID: "change", Action: "rolled_back"}, {Sequence: 3, SessionID: "worker", ExecutionID: "change", OperationID: "change", Action: "applied"}}
	svc := &Service{store: store}
	page, err := svc.WorkerExecutions(ctx, "worker", 0, 1)
	if err != nil || page.NextCursor != 1 || len(page.Events) != 1 || page.Events[0].SessionMode != domain.SessionModeChat || page.Current.ContentHash != "original" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	page, err = svc.WorkerExecutions(ctx, "worker", page.NextCursor, 100)
	if err != nil || page.NextCursor != 0 || len(page.Events) != 1 || page.Events[0].SessionMode != domain.SessionModeTUI || page.Events[0].ContentHash != "original" || page.Events[0].Actor.ID != "human" {
		t.Fatalf("rollback or concurrent read drift: %+v %v", page, err)
	}
	for _, bounds := range [][2]int64{{-1, 10}, {0, 0}, {0, 101}} {
		if _, err := svc.WorkerExecutions(ctx, "worker", bounds[0], int(bounds[1])); err == nil {
			t.Fatalf("invalid bounds: %v", bounds)
		}
	}
	page, err = svc.WorkerExecutions(ctx, "legacy", 0, 20)
	if err != nil || page.Current != nil || len(page.Events) != 0 {
		t.Fatalf("invented legacy history: %+v %v", page, err)
	}
	if _, err := svc.WorkerExecutions(ctx, "missing", 0, 20); err == nil {
		t.Fatal("missing session accepted")
	}
	if _, err := svc.WorkerExecution(ctx, "worker", "missing"); err == nil {
		t.Fatal("missing execution accepted")
	} else {
		var api *apierr.Error
		if !errors.As(err, &api) || api.Code != "WORKER_EXECUTION_NOT_FOUND" {
			t.Fatalf("wrong missing error: %v", err)
		}
	}
	if _, err := svc.WorkerExecution(ctx, "legacy", "change"); err == nil {
		t.Fatal("cross-session execution returned")
	}
	if got, err := svc.WorkerExecution(ctx, "worker", "change"); err != nil || got.Actor.ID != "human" {
		t.Fatalf("detail: %+v %v", got, err)
	}
}
