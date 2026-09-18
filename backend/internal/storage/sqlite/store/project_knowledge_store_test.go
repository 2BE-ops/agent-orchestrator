package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func knowledgeDefinition() domain.KnowledgeDefinition {
	return domain.KnowledgeDefinition{Title: "Database ownership", Kind: "architecture", Content: "The daemon owns durable state", Status: "candidate", Confidence: "medium", Sources: []domain.KnowledgeSource{{Kind: "user", Reference: "Architecture review"}}}
}

func knowledgeMutation(version int64) domain.KnowledgeMutation {
	return domain.KnowledgeMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Record reviewed project facts", ExpectedVersion: version}
}

func TestProjectKnowledgeImmutableReviewHistoryAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	d := knowledgeDefinition()
	if _, err := s.CreateProjectKnowledge(ctx, "fact", "project", d, knowledgeMutation(0)); err != nil {
		t.Fatal(err)
	}
	d.Status, d.Pinned, d.Content = "accepted", true, "Only daemon services may write durable state"
	accepted, err := s.ReviseProjectKnowledge(ctx, "fact", d, knowledgeMutation(1))
	if err != nil || accepted.Number != 2 {
		t.Fatalf("accept: %+v %v", accepted, err)
	}
	d.Status, d.Pinned = "invalidated", false
	if _, err := s.ReviseProjectKnowledge(ctx, "fact", d, knowledgeMutation(2)); err != nil {
		t.Fatal(err)
	}
	d.Status = "deleted"
	if _, err := s.ReviseProjectKnowledge(ctx, "fact", d, knowledgeMutation(3)); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	old, err := reopened.GetKnowledgeVersion(ctx, "fact", 2)
	if err != nil || old.Definition.Status != "accepted" || old.ContentHash != accepted.ContentHash || !old.Definition.Pinned {
		t.Fatalf("past accepted version changed: %+v %v", old, err)
	}
	items, err := reopened.ListProjectKnowledge(ctx, domain.KnowledgeFilter{ProjectID: "project", Limit: 20})
	if err != nil || len(items) != 0 {
		t.Fatalf("deleted fact selected: %+v %v", items, err)
	}
	items, err = reopened.ListProjectKnowledge(ctx, domain.KnowledgeFilter{ProjectID: "project", Limit: 20, Status: "deleted"})
	if err != nil || len(items) != 1 || items[0].Version != 4 {
		t.Fatalf("deleted history unavailable: %+v %v", items, err)
	}
	history, err := reopened.ListKnowledgeVersions(ctx, "fact", 1, 2)
	if err != nil || len(history) != 2 || history[0].Number != 2 || history[1].Number != 3 {
		t.Fatalf("history page: %+v %v", history, err)
	}
	events, err := reopened.EventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Type == cdc.EventProjectKnowledgeChanged {
			count++
			if event.ProjectID != "project" {
				t.Fatalf("wrong CDC project: %+v", event)
			}
		}
	}
	if count != 4 {
		t.Fatalf("expected one event per committed version: %d", count)
	}
}

func TestProjectKnowledgeWorkerProposalsRequireOwnActiveAttempt(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seed, lease := taskExecutionSeed(t, s)
	d := knowledgeDefinition()
	d.Sources = []domain.KnowledgeSource{{Kind: "worker", Reference: "Observed service ownership", TaskID: "work", AttemptID: lease.AttemptID, SessionID: seed.ID}}
	mutation := domain.KnowledgeMutation{Actor: domain.AdaptiveActor{Kind: "WORKER", ID: "worker", SessionID: seed.ID}, Reason: "Propose a discovered constraint"}
	if _, err := s.CreateProjectKnowledge(ctx, "candidate", "project", d, mutation); err != nil {
		t.Fatal(err)
	}
	d.Status = "accepted"
	if _, err := s.CreateProjectKnowledge(ctx, "self-accepted", "project", d, mutation); !errors.Is(err, ports.ErrKnowledgeForbidden) {
		t.Fatalf("worker canonized its claim: %v", err)
	}
	d.Status = "candidate"
	mutation.ExpectedVersion = 1
	if _, err := s.ReviseProjectKnowledge(ctx, "candidate", d, mutation); !errors.Is(err, ports.ErrKnowledgeForbidden) {
		t.Fatalf("worker rewrote review history: %v", err)
	}
	mutation.ExpectedVersion = 0
	d.Sources[0].SessionID = "unknown"
	if _, err := s.CreateProjectKnowledge(ctx, "forged", "project", d, mutation); err == nil {
		t.Fatal("worker forged another source")
	}
	d.Sources[0].SessionID = seed.ID
	seed.IsTerminated = true
	if err := s.UpdateSession(ctx, seed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProjectKnowledge(ctx, "stale", "project", d, mutation); !errors.Is(err, ports.ErrKnowledgeForbidden) {
		t.Fatalf("terminated worker submitted new claims: %v", err)
	}
}

func TestProjectKnowledgeScopeSearchAndSupersession(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	seedProject(t, s, "other")
	createTask(t, s, "local-task", taskDefinition())
	d := knowledgeDefinition()
	d.Status = "accepted"
	for _, item := range []struct{ id, project string }{{"old", "project"}, {"replacement", "project"}, {"foreign", "other"}} {
		if _, err := s.CreateProjectKnowledge(ctx, item.id, domain.ProjectID(item.project), d, knowledgeMutation(0)); err != nil {
			t.Fatal(err)
		}
	}
	d.TaskIDs = []string{"local-task"}
	if _, err := s.CreateProjectKnowledge(ctx, "leak", "other", d, knowledgeMutation(0)); !errors.Is(err, ports.ErrKnowledgeForbidden) {
		t.Fatalf("cross-project provenance accepted: %v", err)
	}
	d.TaskIDs = nil
	d.Status = "superseded"
	for _, id := range []string{"old", "foreign"} {
		d.SupersededBy = &domain.KnowledgeVersionRef{ID: id, Version: 1}
		if _, err := s.ReviseProjectKnowledge(ctx, "old", d, knowledgeMutation(1)); err == nil {
			t.Fatalf("invalid replacement %s accepted", id)
		}
	}
	d.SupersededBy = &domain.KnowledgeVersionRef{ID: "replacement", Version: 1}
	if _, err := s.ReviseProjectKnowledge(ctx, "old", d, knowledgeMutation(1)); err != nil {
		t.Fatal(err)
	}
	items, err := s.ListProjectKnowledge(ctx, domain.KnowledgeFilter{ProjectID: "project", Status: "accepted", Kind: "architecture", Search: "DAEMON", Limit: 20})
	if err != nil || len(items) != 1 || items[0].ID != "replacement" {
		t.Fatalf("current accepted search: %+v %v", items, err)
	}
	items, err = s.ListProjectKnowledge(ctx, domain.KnowledgeFilter{ProjectID: "project", Search: "%", Limit: 20})
	if err != nil || len(items) != 0 {
		t.Fatalf("search treated wildcard as SQL: %+v %v", items, err)
	}
}

func TestProjectKnowledgeConcurrentReviewHasOneWinner(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	d := knowledgeDefinition()
	if _, err := s.CreateProjectKnowledge(ctx, "fact", "project", d, knowledgeMutation(0)); err != nil {
		t.Fatal(err)
	}
	d.Status = "accepted"
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			_, err := s.ReviseProjectKnowledge(ctx, "fact", d, knowledgeMutation(1))
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	winners := 0
	for err := range errs {
		if err == nil {
			winners++
		} else if !errors.Is(err, ports.ErrKnowledgeConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("review writers: %d", winners)
	}
}

func TestProjectKnowledgeCDCFailureRollsBackAndHistoryCannotBeRewritten(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	d := knowledgeDefinition()
	if _, err := s.CreateProjectKnowledge(ctx, "fact", "project", d, knowledgeMutation(0)); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TRIGGER fail_knowledge_cdc BEFORE INSERT ON change_log WHEN NEW.event_type='project_knowledge_changed' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	d.Status = "accepted"
	if _, err := s.ReviseProjectKnowledge(ctx, "fact", d, knowledgeMutation(1)); err == nil {
		t.Fatal("CDC failure committed version")
	}
	if _, err := s.CreateProjectKnowledge(ctx, "orphan", "project", d, knowledgeMutation(0)); err == nil {
		t.Fatal("CDC failure committed identity")
	}
	if _, err := s.GetProjectKnowledge(ctx, "orphan"); !errors.Is(err, ports.ErrKnowledgeNotFound) {
		t.Fatalf("orphan identity: %v", err)
	}
	if _, err := s.GetKnowledgeVersion(ctx, "fact", 2); !errors.Is(err, ports.ErrKnowledgeNotFound) {
		t.Fatalf("orphan version: %v", err)
	}
	for _, statement := range []string{`UPDATE project_knowledge_versions SET reason='rewritten'`, `DELETE FROM project_knowledge_versions`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("history was mutable: %s", statement)
		}
	}
}
