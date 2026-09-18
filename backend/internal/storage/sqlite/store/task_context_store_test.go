package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func inlineContext(t *testing.T, kind, id string, version int64, hash string, content any) domain.ContextSource {
	t.Helper()
	encoded, _, err := domain.TaskContent(content)
	if err != nil {
		t.Fatal(err)
	}
	return domain.ContextSource{Kind: kind, ID: id, Version: version, SourceHash: hash, Content: string(encoded), ContentHash: domain.ContextTextHash(string(encoded)), Disposition: "inline", Reason: "Pinned exact evidence"}
}

func sealContext(t *testing.T, snapshot *domain.TaskContextSnapshot) {
	t.Helper()
	var err error
	snapshot.Prompt, err = domain.RenderTaskContextPrompt(snapshot.BasePrompt, snapshot.Sources)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.TotalBytes = len(snapshot.Prompt) + snapshot.SystemPromptBytes
	snapshot.EstimatedTokens = (snapshot.TotalBytes + 3) / 4
	snapshot.ContentHash = snapshot.Hash()
}

func taskContextFixture(t *testing.T, s *sqlite.Store) (domain.TaskContextSnapshot, domain.TaskLease) {
	t.Helper()
	ctx := context.Background()
	seed, lease := taskExecutionSeed(t, s)
	op := domain.TaskExecutionOperation{ID: "context-native", SessionID: seed.ID, Lease: lease.TaskLeaseToken, SourceOwner: seed.ControllerOwner(), Kind: "dispatch", CreatedAt: lease.HeartbeatAt}
	if _, err := s.BeginTaskExecution(ctx, op); err != nil {
		t.Fatal(err)
	}
	revision, err := s.GetTaskRevision(ctx, "work", 1)
	if err != nil {
		t.Fatal(err)
	}
	criteria, err := s.GetAcceptanceCriteria(ctx, "work", 1)
	if err != nil {
		t.Fatal(err)
	}
	config, ok, err := s.GetWorkerConfiguration(ctx, seed.ID)
	if err != nil || !ok {
		t.Fatalf("configuration: %v %v", ok, err)
	}
	snapshot := domain.TaskContextSnapshot{SchemaVersion: 1, AttemptID: lease.AttemptID, SessionID: seed.ID, Task: domain.TaskRevisionRef{TaskID: "work", Revision: 1, ContentHash: revision.ContentHash}, CriteriaVersion: 1, ConfigurationHash: config.ContentHash, ExecutionOperationID: op.ID, SystemPromptHash: domain.ContextTextHash(config.SystemPrompt), SystemPromptBytes: len(config.SystemPrompt), BasePrompt: "Complete the bounded task", Budget: domain.DefaultContextBudget(), CreatedAt: lease.HeartbeatAt}
	snapshot.Sources = []domain.ContextSource{
		inlineContext(t, "task", "work", 1, revision.ContentHash, revision.Definition),
		inlineContext(t, "criteria", "work", 1, criteria.ContentHash, criteria.Definition),
		{Kind: "agent_type", ID: config.AgentType.ID, Version: config.AgentType.Version, SourceHash: config.AgentType.ContentHash, Disposition: "reference", Reason: "Exact worker configuration"},
	}
	sealContext(t, &snapshot)
	return snapshot, lease
}

func TestTaskContextRetainsExactPromptAndSourcesAcrossReviewAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	snapshot, lease := taskContextFixture(t, s)
	definition := knowledgeDefinition()
	definition.Status = "accepted"
	if _, err := s.CreateProjectKnowledge(ctx, "fact", "project", definition, knowledgeMutation(0)); err != nil {
		t.Fatal(err)
	}
	version, err := s.GetKnowledgeVersion(ctx, "fact", 1)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Sources = append(snapshot.Sources, inlineContext(t, "knowledge", "fact", 1, version.ContentHash, definition))
	sealContext(t, &snapshot)
	if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); err != nil {
		t.Fatal(err)
	}
	definition.Status = "invalidated"
	if _, err := s.ReviseProjectKnowledge(ctx, "fact", definition, knowledgeMutation(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReviseAcceptanceCriteria(ctx, "work", taskCriteria(), taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); err != nil {
		t.Fatalf("historical replay: %v", err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	retained, found, err := reopened.GetTaskContextBySession(ctx, snapshot.SessionID)
	if err != nil || !found || retained.ContentHash != snapshot.ContentHash || retained.Prompt != snapshot.Prompt || retained.CriteriaVersion != 1 || retained.Sources[3].Version != 1 {
		t.Fatalf("historical context changed: %+v %v %v", retained, found, err)
	}
	if _, pending, err := reopened.PendingTaskExecution(ctx, snapshot.SessionID); err != nil || !pending {
		t.Fatalf("context sealing pretended process was connected: %v %v", pending, err)
	}
	changed := snapshot
	changed.BasePrompt = "Rewrite context"
	sealContext(t, &changed)
	if err := reopened.SaveTaskContext(ctx, lease.TaskLeaseToken, changed); !errors.Is(err, ports.ErrTaskConflict) {
		t.Fatalf("context overwritten: %v", err)
	}
	stale := lease.TaskLeaseToken
	stale.HolderID = "intruder"
	if err := reopened.SaveTaskContext(ctx, stale, snapshot); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("stale owner replay accepted: %v", err)
	}
}

func TestTaskContextRejectsForgedEvidenceAndLaunchScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*domain.TaskContextSnapshot)
	}{
		{"criteria_rewrite", func(s *domain.TaskContextSnapshot) {
			s.Sources[1].Content = `{"criteria":[]}`
			s.Sources[1].ContentHash = domain.ContextTextHash(s.Sources[1].Content)
		}},
		{"configuration", func(s *domain.TaskContextSnapshot) { s.ConfigurationHash = strings.Repeat("0", 64) }},
		{"native_operation", func(s *domain.TaskContextSnapshot) { s.ExecutionOperationID = "other-native" }},
		{"task_version", func(s *domain.TaskContextSnapshot) { s.Task.Revision = 2; s.Sources[0].Version = 2 }},
		{"missing_type", func(s *domain.TaskContextSnapshot) { s.Sources = s.Sources[:2] }},
		{"unselected_file", func(s *domain.TaskContextSnapshot) {
			s.Sources = append(s.Sources, domain.ContextSource{Kind: "file", ID: "other.txt", SourceHash: domain.ContextTextHash("text"), Content: "text", ContentHash: domain.ContextTextHash("text"), Disposition: "inline", Reason: "Not selected"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			snapshot, lease := taskContextFixture(t, s)
			tc.change(&snapshot)
			sealContext(t, &snapshot)
			if err := s.SaveTaskContext(context.Background(), lease.TaskLeaseToken, snapshot); err == nil {
				t.Fatal("forged context sealed")
			}
			if _, found, err := s.GetTaskContext(context.Background(), lease.AttemptID); err != nil || found {
				t.Fatalf("failed seal left context: %v %v", found, err)
			}
		})
	}
}

func TestTaskContextRechecksKnowledgeAndCancellationAtSeal(t *testing.T) {
	ctx := context.Background()
	for _, reason := range []string{"candidate", "invalidated", "foreign", "cancelled"} {
		t.Run(reason, func(t *testing.T) {
			s := newTestStore(t)
			snapshot, lease := taskContextFixture(t, s)
			definition := knowledgeDefinition()
			definition.Status = "accepted"
			project := domain.ProjectID("project")
			if reason == "foreign" {
				seedProject(t, s, "other")
				project = "other"
			}
			if reason == "candidate" {
				definition.Status = "candidate"
			}
			if _, err := s.CreateProjectKnowledge(ctx, "fact", project, definition, knowledgeMutation(0)); err != nil {
				t.Fatal(err)
			}
			version, err := s.GetKnowledgeVersion(ctx, "fact", 1)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.Sources = append(snapshot.Sources, inlineContext(t, "knowledge", "fact", 1, version.ContentHash, definition))
			sealContext(t, &snapshot)
			if reason == "invalidated" {
				definition.Status = "invalidated"
				if _, err := s.ReviseProjectKnowledge(ctx, "fact", definition, knowledgeMutation(1)); err != nil {
					t.Fatal(err)
				}
			}
			if reason == "cancelled" {
				cancelTask(t, s, "work")
			}
			if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); err == nil {
				t.Fatal("ineligible context sealed")
			}
		})
	}
}

func TestTaskContextAuditFailureIsAtomicAndSnapshotImmutable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	snapshot, lease := taskContextFixture(t, s)
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TRIGGER fail_context_audit BEFORE INSERT ON adaptive_task_audit WHEN NEW.action='context_sealed' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); err == nil {
		t.Fatal("seal ignored failed audit")
	}
	if _, found, err := s.GetTaskContext(ctx, lease.AttemptID); err != nil || found {
		t.Fatalf("partial context: %v %v", found, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_context_audit`); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`UPDATE adaptive_task_contexts SET snapshot='{}'`, `DELETE FROM adaptive_task_contexts`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("mutable context: %s", statement)
		}
	}
}
