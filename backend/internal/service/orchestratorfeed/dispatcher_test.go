package orchestratorfeed

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

type noticeJournal struct {
	rows       map[string]domain.OrchestratorNotice // keyed by anchor
	resolved   map[string]domain.OrchestratorNoticeResolution
	projects   []string
	commitErr  error
	beginErr   error
	pendingCap int
}

func newNoticeJournal(projects ...string) *noticeJournal {
	return &noticeJournal{rows: map[string]domain.OrchestratorNotice{}, resolved: map[string]domain.OrchestratorNoticeResolution{}, projects: projects}
}

func anchorKey(project, task, fact, anchor string) string {
	return project + "|" + task + "|" + fact + "|" + anchor
}

func (s *noticeJournal) BeginOrchestratorNotice(_ context.Context, notice domain.OrchestratorNotice) (domain.OrchestratorNotice, bool, error) {
	if s.beginErr != nil {
		return domain.OrchestratorNotice{}, false, s.beginErr
	}
	key := anchorKey(string(notice.ProjectID), notice.TaskID, notice.Fact, notice.Anchor)
	if existing, ok := s.rows[key]; ok {
		return existing, false, nil
	}
	notice.State, notice.Reason, notice.ResolvedAt = "pending", "", nil
	s.rows[key] = notice
	return notice, true, nil
}

func (s *noticeJournal) ResolveOrchestratorNotice(ctx context.Context, resolution domain.OrchestratorNoticeResolution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.commitErr != nil {
		return s.commitErr
	}
	for key, row := range s.rows {
		if row.ID == resolution.ID {
			if _, settled := s.resolved[key]; settled {
				return ports.ErrGoalConflict
			}
			s.resolved[key] = resolution
			return nil
		}
	}
	return ports.ErrGoalNotFound
}

func (s *noticeJournal) ListPendingOrchestratorNotices(_ context.Context, limit int) ([]domain.OrchestratorNotice, error) {
	items := make([]domain.OrchestratorNotice, 0)
	for key, row := range s.rows {
		if _, settled := s.resolved[key]; settled {
			continue
		}
		items = append(items, row)
		if s.pendingCap > 0 && len(items) == s.pendingCap {
			break
		}
		if len(items) == limit {
			break
		}
	}
	return items, nil
}

func (s *noticeJournal) ListOrchestratorNoticeProjects(_ context.Context, after string, limit int) ([]string, error) {
	items := make([]string, 0)
	for _, project := range s.projects {
		if project > after {
			items = append(items, project)
			if len(items) == limit {
				break
			}
		}
	}
	return items, nil
}

func (s *noticeJournal) settledStates() map[string]string {
	states := map[string]string{}
	for key, resolution := range s.resolved {
		states[key] = resolution.State
	}
	return states
}

type fakeOrchestrators struct {
	live        map[string]domain.SessionRecord
	pages       map[string][]domain.ProjectFeedbackItem
	feedbackErr map[string]error
	read        []string
}

func (f *fakeOrchestrators) LiveOrchestrator(_ context.Context, project domain.ProjectID) (domain.SessionRecord, bool, error) {
	rec, ok := f.live[string(project)]
	return rec, ok, nil
}

func (f *fakeOrchestrators) Feedback(_ context.Context, project domain.ProjectID, afterTaskID string, limit int) ([]domain.ProjectFeedbackItem, error) {
	f.read = append(f.read, string(project))
	if err := f.feedbackErr[string(project)]; err != nil {
		return nil, err
	}
	items := make([]domain.ProjectFeedbackItem, 0)
	for _, item := range f.pages[string(project)] {
		if item.TaskID > afterTaskID {
			items = append(items, item)
			if len(items) == limit {
				break
			}
		}
	}
	return items, nil
}

type recordingNoticeTransport struct {
	result   ports.OrchestratorNoticeTransportResult
	ready    map[domain.SessionID]bool
	probeErr error
	onSend   func(domain.OrchestratorNotice)
	sent     []domain.OrchestratorNotice
}

func (t *recordingNoticeTransport) OrchestratorTargetReady(_ context.Context, id domain.SessionID) (bool, error) {
	return t.ready[id], t.probeErr
}

func (t *recordingNoticeTransport) DeliverOrchestratorNotice(_ context.Context, _ domain.SessionID, notice domain.OrchestratorNotice) ports.OrchestratorNoticeTransportResult {
	t.sent = append(t.sent, notice)
	if t.onSend != nil {
		t.onSend(notice)
	}
	return t.result
}

func feedbackItem(task, state, resultID string, revision int64) domain.ProjectFeedbackItem {
	return domain.ProjectFeedbackItem{TaskID: task, Title: "Task " + task, State: state, Revision: revision,
		ResultID: resultID, Reason: "derived reason"}
}

func TestOrchestratorNoticeDispatchDeliversTerminalFactsOnce(t *testing.T) {
	journal := newNoticeJournal("project")
	orchestrators := &fakeOrchestrators{
		live: map[string]domain.SessionRecord{"project": {ID: "orch-1", ProjectID: "project", Kind: domain.KindOrchestrator}},
		pages: map[string][]domain.ProjectFeedbackItem{"project": {
			feedbackItem("a", "pending", "", 1),
			feedbackItem("b", "working", "", 1),
			feedbackItem("c", "completed", "result-9", 1),
			feedbackItem("d", "failed", "", 1),
			feedbackItem("e", "cancelled", "", 2),
			// A completion without a sealed result identity cannot be anchored.
			feedbackItem("f", "completed", "", 3),
			feedbackItem("g", "cancelling", "", 1),
		}},
	}
	transport := &recordingNoticeTransport{result: ports.OrchestratorNoticeTransportResult{State: "handed_off", Reason: "accepted"}}
	d := New(journal, orchestrators, transport, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := d.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 3 {
		t.Fatalf("terminal fact count: %+v", transport.sent)
	}
	states := journal.settledStates()
	for _, key := range []string{
		anchorKey("project", "c", "completed", "result:result-9"),
		anchorKey("project", "d", "failed", "exhausted:1"),
		anchorKey("project", "e", "cancelled", "cancelled:2"),
	} {
		if states[key] != "handed_off" {
			t.Fatalf("fact %s settled as %+v", key, states[key])
		}
	}

	// Later cycles derive the same facts without journalling or sending again.
	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 3 {
		t.Fatalf("repeat dispatch resent facts: %+v", transport.sent)
	}

	// A new terminal fact on a later cycle is pushed exactly once.
	orchestrators.pages["project"] = append(orchestrators.pages["project"], feedbackItem("h", "completed", "result-10", 1))
	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 4 || transport.sent[3].TaskID != "h" {
		t.Fatalf("new fact: %+v", transport.sent)
	}
}

func TestOrchestratorNoticeReconcileRetainsUncertaintyNeverResends(t *testing.T) {
	journal := newNoticeJournal("project")
	orchestrators := &fakeOrchestrators{
		live:  map[string]domain.SessionRecord{"project": {ID: "orch-1", ProjectID: "project"}},
		pages: map[string][]domain.ProjectFeedbackItem{"project": {feedbackItem("c", "completed", "result-9", 1)}},
	}
	transport := &recordingNoticeTransport{result: ports.OrchestratorNoticeTransportResult{State: "handed_off", Reason: "accepted"}}
	d := New(journal, orchestrators, transport, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// A previous daemon run created the row but never recorded its outcome.
	if _, created, err := journal.BeginOrchestratorNotice(context.Background(), domain.OrchestratorNotice{
		ID: "stuck", ProjectID: "project", TaskID: "c", Fact: "completed", Anchor: "result:result-9",
		Revision: 1, Detail: "completed", CreatedAt: time.Now().UTC()}); err != nil || !created {
		t.Fatalf("seed: %v", err)
	}
	if err := d.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state := journal.settledStates()[anchorKey("project", "c", "completed", "result:result-9")]; state != "uncertain" {
		t.Fatalf("restart outcome: %+v", state)
	}
	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("restart resent uncertain notice: %+v", transport.sent)
	}
}

func TestOrchestratorNoticeSkipsProjectsWithoutLiveOrchestrator(t *testing.T) {
	journal := newNoticeJournal("project")
	orchestrators := &fakeOrchestrators{
		pages: map[string][]domain.ProjectFeedbackItem{"project": {feedbackItem("c", "completed", "result-9", 1)}},
	}
	transport := &recordingNoticeTransport{result: ports.OrchestratorNoticeTransportResult{State: "handed_off", Reason: "accepted"}}
	d := New(journal, orchestrators, transport, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(orchestrators.read) != 0 || len(journal.rows) != 0 || len(transport.sent) != 0 {
		t.Fatalf("dead orchestrator derived facts: reads=%+v rows=%d sent=%d", orchestrators.read, len(journal.rows), len(transport.sent))
	}

	// A derivation failure on a live project defers without blocking peers.
	live := map[string]domain.SessionRecord{"project": {ID: "orch-1", ProjectID: "project"}, "second": {ID: "orch-2", ProjectID: "second"}}
	orchestrators.live, orchestrators.feedbackErr = live, map[string]error{"project": errors.New("injected derivation failure")}
	journal.projects = []string{"project", "second"}
	orchestrators.pages["second"] = []domain.ProjectFeedbackItem{feedbackItem("z", "completed", "result-2", 1)}
	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].TaskID != "z" {
		t.Fatalf("peer starved by derivation failure: %+v", transport.sent)
	}
}

func TestOrchestratorNoticeInvalidObservationSettlesUncertain(t *testing.T) {
	journal := newNoticeJournal("project")
	orchestrators := &fakeOrchestrators{
		live:  map[string]domain.SessionRecord{"project": {ID: "orch-1", ProjectID: "project"}},
		pages: map[string][]domain.ProjectFeedbackItem{"project": {feedbackItem("c", "completed", "result-9", 1)}},
	}
	transport := &recordingNoticeTransport{result: ports.OrchestratorNoticeTransportResult{State: "made_up", Reason: ""}}
	d := New(journal, orchestrators, transport, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := journal.settledStates()[anchorKey("project", "c", "completed", "result:result-9")]
	if state != "uncertain" {
		t.Fatalf("invalid observation settled as %q", state)
	}
}

func TestOrchestratorNoticeProjectCursorKeepsLateCandidatesFair(t *testing.T) {
	projects := make([]string, 0, 20)
	for i := range 20 {
		projects = append(projects, fmt.Sprintf("p%02d", i))
	}
	journal := newNoticeJournal(projects...)
	last := projects[len(projects)-1]
	orchestrators := &fakeOrchestrators{
		live:  map[string]domain.SessionRecord{last: {ID: "orch-1", ProjectID: domain.ProjectID(last)}},
		pages: map[string][]domain.ProjectFeedbackItem{last: {feedbackItem("c", "completed", "result-9", 1)}},
	}
	transport := &recordingNoticeTransport{result: ports.OrchestratorNoticeTransportResult{State: "handed_off", Reason: "accepted"}}
	d := New(journal, orchestrators, transport, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// The first cycle stops after sixteen candidates; the second continues from
	// the cursor instead of restarting at the alphabet and starving the tail.
	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("early cycle delivered tail project: %+v", transport.sent)
	}
	if d.projectCursor != projects[15] {
		t.Fatalf("cursor after full page: %q", d.projectCursor)
	}
	if err := d.dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].TaskID != "c" {
		t.Fatalf("tail candidate starved: %+v", transport.sent)
	}
}
