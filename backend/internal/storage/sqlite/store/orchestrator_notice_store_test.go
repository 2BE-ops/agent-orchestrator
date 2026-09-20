package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func noticeFixture(t *testing.T, s *sqlite.Store) {
	t.Helper()
	seedProject(t, s, "project")
	createTask(t, s, "task", taskDefinition())
}

func pendingNotice(id string) domain.OrchestratorNotice {
	return domain.OrchestratorNotice{ID: id, ProjectID: "project", TaskID: "task", Fact: "completed",
		Anchor: "result:result-1", Revision: 1, Detail: "completed \"Work\" revision 1: evidence passed",
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
}

func TestOrchestratorNoticeAnchoredOnceAndSettledOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	noticeFixture(t, s)

	created, ok, err := s.BeginOrchestratorNotice(ctx, pendingNotice("notice-1"))
	if err != nil || !ok || created.State != "pending" {
		t.Fatalf("begin: %+v created=%v err=%v", created, ok, err)
	}

	// The same durable anchor replays as the retained row, never a second send.
	replay, ok, err := s.BeginOrchestratorNotice(ctx, pendingNotice("notice-2"))
	if err != nil || ok || replay.ID != "notice-1" || replay.State != "pending" {
		t.Fatalf("anchor replay: %+v created=%v err=%v", replay, ok, err)
	}

	resolvedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.ResolveOrchestratorNotice(ctx, domain.OrchestratorNoticeResolution{ID: "notice-1",
		State: "handed_off", Reason: "Existing Chat controller accepted this delivery key", ResolvedAt: resolvedAt}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := s.ResolveOrchestratorNotice(ctx, domain.OrchestratorNoticeResolution{ID: "notice-1",
		State: "uncertain", Reason: "second settle", ResolvedAt: resolvedAt}); !errors.Is(err, ports.ErrGoalConflict) {
		t.Fatalf("second settle accepted: %v", err)
	}

	// A settled anchor replays as the retained outcome.
	after, ok, err := s.BeginOrchestratorNotice(ctx, pendingNotice("notice-3"))
	if err != nil || ok || after.ID != "notice-1" || after.State != "handed_off" || after.ResolvedAt == nil || !after.ResolvedAt.Equal(resolvedAt) {
		t.Fatalf("settled replay: %+v created=%v err=%v", after, ok, err)
	}

	// A different anchor for the same task journals separately.
	different := pendingNotice("notice-4")
	different.Fact, different.Anchor = "failed", "exhausted:2"
	different.Revision = 2
	if _, ok, err := s.BeginOrchestratorNotice(ctx, different); err != nil || !ok {
		t.Fatalf("different anchor: created=%v err=%v", ok, err)
	}
}

func TestOrchestratorNoticeStartupReconciliationAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	noticeFixture(t, s)

	first := pendingNotice("notice-1")
	if _, ok, err := s.BeginOrchestratorNotice(ctx, first); err != nil || !ok {
		t.Fatalf("begin: %v", err)
	}
	second := pendingNotice("notice-2")
	second.Anchor = "cancelled:1"
	second.Fact = "cancelled"
	if _, ok, err := s.BeginOrchestratorNotice(ctx, second); err != nil || !ok {
		t.Fatalf("begin second: %v", err)
	}

	// Simulate a daemon restart: reopen the same database and settle everything
	// a previous run left pending as retained uncertainty.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	pending, err := reopened.ListPendingOrchestratorNotices(ctx, 100)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending after restart: %+v err=%v", pending, err)
	}
	now := time.Now().UTC()
	for _, item := range pending {
		if err := reopened.ResolveOrchestratorNotice(ctx, domain.OrchestratorNoticeResolution{ID: item.ID,
			State: "uncertain", Reason: "Daemon restarted before the orchestrator notice outcome was recorded",
			ResolvedAt: now}); err != nil {
			t.Fatalf("settle %s: %v", item.ID, err)
		}
	}
	if pending, err := reopened.ListPendingOrchestratorNotices(ctx, 100); err != nil || len(pending) != 0 {
		t.Fatalf("pending after reconcile: %+v err=%v", pending, err)
	}
	// The journalled facts survive restart and never resend.
	if _, ok, err := reopened.BeginOrchestratorNotice(ctx, first); err != nil || ok {
		t.Fatalf("restart replay created a second send: created=%v err=%v", ok, err)
	}
}

func TestOrchestratorNoticeScopeAndSealGuards(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	noticeFixture(t, s)

	// A task from another project cannot be journalled against this project.
	foreign := pendingNotice("foreign")
	foreign.ProjectID = "other"
	seedProject(t, s, "other")
	if _, ok, err := s.BeginOrchestratorNotice(ctx, foreign); err == nil || ok {
		t.Fatalf("cross-project notice accepted: created=%v err=%v", ok, err)
	}

	if _, ok, err := s.BeginOrchestratorNotice(ctx, pendingNotice("notice-1")); err != nil || !ok {
		t.Fatalf("begin: %v", err)
	}

	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// SQL-level guards mirror the service boundary: a non-pending insert, an
	// identity rewrite and history deletion all refuse.
	if _, err := db.Exec(`INSERT INTO adaptive_orchestrator_notices(id,project_id,task_id,fact,anchor,revision,detail,state,reason,created_at) VALUES('direct','project','task','completed','x',1,'d','handed_off','',CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("non-pending insert accepted")
	}
	if _, err := db.Exec(`UPDATE adaptive_orchestrator_notices SET anchor='rewritten' WHERE id='notice-1'`); err == nil {
		t.Fatal("identity rewrite accepted")
	}
	if _, err := db.Exec(`DELETE FROM adaptive_orchestrator_notices WHERE id='notice-1'`); err == nil {
		t.Fatal("history delete accepted")
	}

	if _, err := s.ListPendingOrchestratorNotices(ctx, 0); err == nil {
		t.Fatal("zero limit accepted")
	}
	if _, err := s.ListOrchestratorNoticeProjects(ctx, "", 0); err == nil {
		t.Fatal("zero project limit accepted")
	}
}

func TestOrchestratorNoticeCandidateProjectsPageByLiveKind(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "alpha")
	seedProject(t, s, "beta")

	for _, project := range []string{"alpha", "beta"} {
		if _, err := s.CreateSession(ctx, domain.SessionRecord{ProjectID: domain.ProjectID(project),
			Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat,
			CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		// A worker session in the same project must not turn it into a candidate
		// twice, and projects without orchestrators stay invisible.
		if _, err := s.CreateSession(ctx, domain.SessionRecord{ProjectID: domain.ProjectID(project),
			Kind: domain.KindWorker, Harness: domain.HarnessCodex, Mode: domain.SessionModeTUI,
			CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}

	first, err := s.ListOrchestratorNoticeProjects(ctx, "", 10)
	if err != nil || len(first) != 2 || first[0] != "alpha" || first[1] != "beta" {
		t.Fatalf("candidates: %+v err=%v", first, err)
	}
	second, err := s.ListOrchestratorNoticeProjects(ctx, "alpha", 10)
	if err != nil || len(second) != 1 || second[0] != "beta" {
		t.Fatalf("paged candidates: %+v err=%v", second, err)
	}
}
