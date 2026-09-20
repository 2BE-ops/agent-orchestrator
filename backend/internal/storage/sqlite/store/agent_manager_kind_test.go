package store_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestAgentManagerSessionOwnershipConversationAndRestart(t *testing.T) {
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	ctx := context.Background()
	seedProject(t, s, "project")
	seedProject(t, s, "other")
	worker, err := s.CreateSession(ctx, sampleRecord("project"))
	mustNoError(t, err)
	orchestrator := sampleRecord("project")
	orchestrator.Kind = domain.KindOrchestrator
	orchestrator, err = s.CreateSession(ctx, orchestrator)
	mustNoError(t, err)
	seed := sampleRecord("project")
	seed.Kind, seed.Mode = domain.KindAgentManager, domain.SessionModeChat
	var wg sync.WaitGroup
	created := make(chan domain.SessionRecord, 8)
	for range 8 {
		wg.Go(func() {
			if rec, err := s.CreateSession(ctx, seed); err == nil {
				created <- rec
			}
		})
	}
	wg.Wait()
	close(created)
	var manager domain.SessionRecord
	count := 0
	for rec := range created {
		manager = rec
		count++
	}
	if count != 1 || manager.Kind != domain.KindAgentManager {
		t.Fatalf("duplicate manager admission: %d %+v", count, manager)
	}
	projectConversation, err := s.CreateConversation(ctx, "project-conversation", domain.ConversationScopeProject, "project", orchestrator.ID, time.Now().UTC())
	mustNoError(t, err)
	managerConversation, err := s.CreateConversation(ctx, "manager-conversation", domain.ConversationScopeSession, "project", manager.ID, time.Now().UTC())
	mustNoError(t, err)
	other := manager
	other.Kind = domain.KindWorker
	if err := s.UpdateSession(ctx, other); err == nil {
		t.Fatal("manager relabeled as worker")
	}
	other = worker
	other.Kind = domain.KindAgentManager
	if err := s.UpdateSession(ctx, other); err == nil {
		t.Fatal("worker promoted into controller")
	}
	standalone := seed
	standalone.ProjectID = ""
	if _, err := s.CreateSession(ctx, standalone); err == nil {
		t.Fatal("projectless manager accepted")
	}
	otherProject := seed
	otherProject.ProjectID = "other"
	if _, err := s.CreateSession(ctx, otherProject); err != nil {
		t.Fatalf("independent project manager blocked: %v", err)
	}
	reopened, err := sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	retained, ok, err := reopened.GetSession(ctx, manager.ID)
	if err != nil || !ok || retained.Kind != domain.KindAgentManager || retained.Mode != domain.SessionModeChat {
		t.Fatalf("role lost across restart: %+v %v", retained, err)
	}
	for sessionID, want := range map[domain.SessionID]string{manager.ID: managerConversation.ID, orchestrator.ID: projectConversation.ID} {
		conversation, err := reopened.ConversationForSession(ctx, sessionID)
		if err != nil || conversation.ID != want {
			t.Fatalf("controller narratives collided: %+v %v", conversation, err)
		}
	}
	if _, err := reopened.CreateSession(ctx, seed); err == nil {
		t.Fatal("restart admitted duplicate controller")
	}
	retained.IsTerminated = true
	mustNoError(t, reopened.UpdateSession(ctx, retained))
	replacement, err := reopened.CreateSession(ctx, seed)
	if err != nil || replacement.ID == manager.ID {
		t.Fatalf("replacement did not retain old identity: %+v %v", replacement, err)
	}
	retained.IsTerminated = false
	if err := reopened.UpdateSession(ctx, retained); err == nil {
		t.Fatal("old controller resurrected over replacement")
	}
}
