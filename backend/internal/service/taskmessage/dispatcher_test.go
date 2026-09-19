package taskmessage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type messageJournal struct {
	ports.TaskMessageStore
	items        []domain.TaskMessage
	claims       map[string]domain.TaskMessageDelivery
	cursor       int64
	commitErr    error
	leaseErrTask string
}

func (s *messageJournal) TaskMessageDispatchCursor(context.Context) (int64, error) {
	return s.cursor, nil
}
func (s *messageJournal) SetTaskMessageDispatchCursor(ctx context.Context, after int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.cursor = after
	return nil
}
func (s *messageJournal) GetActiveTaskLease(_ context.Context, task string) (domain.TaskLease, bool, error) {
	if task == s.leaseErrTask {
		return domain.TaskLease{}, false, errors.New("injected target read failure")
	}
	return domain.TaskLease{TaskLeaseToken: domain.TaskLeaseToken{AttemptID: task}, ExpiresAt: time.Now().Add(time.Minute)}, true, nil
}
func (s *messageJournal) GetTaskWorkerDispatch(_ context.Context, attempt string) (domain.TaskWorkerDispatch, bool, error) {
	return domain.TaskWorkerDispatch{AttemptID: attempt, SessionID: domain.SessionID(attempt)}, true, nil
}
func (s *messageJournal) ListPendingTaskMessages(_ context.Context, after int64, limit int) ([]domain.TaskMessage, error) {
	var items []domain.TaskMessage
	for _, item := range s.items {
		if _, claimed := s.claims[item.ID]; item.Sequence > after && !claimed {
			items = append(items, item)
			if len(items) == limit {
				break
			}
		}
	}
	return items, nil
}
func (s *messageJournal) BeginTaskMessageDelivery(_ context.Context, messageID, id string) (domain.TaskMessageDelivery, bool, error) {
	if existing, ok := s.claims[messageID]; ok {
		return existing, false, nil
	}
	d := domain.TaskMessageDelivery{ID: id, MessageID: messageID, State: "dispatching", DeliveryKey: "adaptive-message:" + messageID}
	s.claims[messageID] = d
	return d, true, nil
}
func (s *messageJournal) ResolveTaskMessageDelivery(ctx context.Context, r domain.TaskMessageDeliveryResolution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.commitErr != nil {
		return s.commitErr
	}
	for key, d := range s.claims {
		if d.ID == r.ID {
			d.State, d.Reason = r.State, r.Reason
			s.claims[key] = d
			return nil
		}
	}
	return ports.ErrTaskNotFound
}
func (s *messageJournal) ListUnresolvedTaskMessageDeliveries(_ context.Context, _ string, _ int) ([]domain.TaskMessageDelivery, error) {
	var items []domain.TaskMessageDelivery
	for _, d := range s.claims {
		if d.State == "dispatching" {
			items = append(items, d)
		}
	}
	return items, nil
}

type recordingMessageTransport struct {
	ready    map[domain.SessionID]bool
	probeErr error
	onSend   func(domain.TaskMessageDelivery)
	result   ports.TaskMessageTransportResult
	sent     []string
}

func (t *recordingMessageTransport) TaskMessageTargetReady(_ context.Context, id domain.SessionID) (bool, error) {
	return t.ready[id], t.probeErr
}
func (t *recordingMessageTransport) DeliverTaskMessage(_ context.Context, d domain.TaskMessageDelivery, _ domain.TaskMessage) ports.TaskMessageTransportResult {
	t.sent = append(t.sent, d.MessageID)
	if t.onSend != nil {
		t.onSend(d)
	}
	return t.result
}

func dispatcherFixture(count int) (*messageJournal, *recordingMessageTransport, *Dispatcher) {
	s := &messageJournal{claims: map[string]domain.TaskMessageDelivery{}}
	transport := &recordingMessageTransport{ready: map[domain.SessionID]bool{}, result: ports.TaskMessageTransportResult{State: "handed_off", Reason: "Transport accepted"}}
	for i := range count {
		id := fmt.Sprint(i + 1)
		s.items = append(s.items, domain.TaskMessage{ID: id, Sequence: int64(i + 1), Definition: domain.TaskMessageDefinition{TargetTaskID: id}})
	}
	return s, transport, New(s, transport, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestTaskMessageDispatchCursorPreservesFairnessAcrossRestart(t *testing.T) {
	s, transport, first := dispatcherFixture(17)
	transport.ready["17"] = true
	if err := first.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.cursor != 16 || len(s.claims) != 0 {
		t.Fatalf("blocked recipients consumed attempts: %d %+v", s.cursor, s.claims)
	}
	restarted := New(s, transport, first.log)
	transport.onSend = func(d domain.TaskMessageDelivery) {
		if stored, ok := s.claims[d.MessageID]; !ok || stored.ID != d.ID || stored.State != "dispatching" {
			t.Fatal("transport preceded durable reservation")
		}
	}
	if err := restarted.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0] != "17" || s.claims["17"].State != "handed_off" || s.cursor != 0 {
		t.Fatalf("unrelated work starved: %+v %d", transport.sent, s.cursor)
	}
}

func TestTaskMessageDispatchCommitFailureNeverRepeatsNativeEffect(t *testing.T) {
	s, transport, d := dispatcherFixture(1)
	transport.ready["1"] = true
	s.commitErr = errors.New("lost write acknowledgement")
	if err := d.deliver(context.Background(), s.items[0]); !errors.Is(err, s.commitErr) {
		t.Fatalf("commit failure: %v", err)
	}
	if len(transport.sent) != 1 || s.claims["1"].State != "dispatching" {
		t.Fatalf("missing reservation: %+v", s.claims)
	}
	if err := d.deliver(context.Background(), s.items[0]); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 {
		t.Fatal("claim replay repeated native send")
	}
	s.commitErr = nil
	restarted := New(s, transport, d.log)
	if err := restarted.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := restarted.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || s.claims["1"].State != "uncertain" {
		t.Fatalf("restart repeated or hid uncertain send: %+v", s.claims)
	}
}

func TestTaskMessageDispatchUnknownReadinessAndBranchReadFailureDoNotBlockPeers(t *testing.T) {
	s, transport, d := dispatcherFixture(2)
	transport.ready["1"], transport.ready["2"] = true, true
	transport.probeErr = errors.New("unknown runtime probe")
	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.claims) != 0 {
		t.Fatal("unknown readiness consumed delivery budget")
	}
	transport.probeErr = nil
	s.leaseErrTask = "1"
	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0] != "2" {
		t.Fatalf("branch read failure blocked unrelated message: %+v", transport.sent)
	}
}

func TestTaskMessageDispatchRecordsCancellationAndInvalidOutcomesConservatively(t *testing.T) {
	s, transport, d := dispatcherFixture(1)
	transport.ready["1"] = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport.onSend = func(domain.TaskMessageDelivery) { cancel() }
	transport.result = ports.TaskMessageTransportResult{State: "made_up", Reason: ""}
	d.Run(ctx)
	if len(transport.sent) != 1 || s.claims["1"].State != "uncertain" {
		t.Fatalf("shutdown lost native observation: %+v", s.claims)
	}
}
