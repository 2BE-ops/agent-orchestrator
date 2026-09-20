package sessionmanager

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestOrchestratorSystemPromptSealsPlanningProtocolOnlyWithGoal(t *testing.T) {
	ctx := context.Background()
	s := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := s.UpsertProject(ctx, domain.ProjectRecord{ID: "goal-project", Path: t.TempDir(), RegisteredAt: time.Now().UTC(), Config: domain.ProjectConfig{OrchestratorRules: "Prefer small verified tasks"}}); err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Store: s, Executable: func() (string, error) { return `C:\AO\ao.exe`, nil }, RunFilePath: `C:\AO\running.json`})

	legacy, err := m.buildSystemPrompt(ctx, domain.KindOrchestrator, "goal-project")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(legacy, "AO_ORCHESTRATOR_TOOLS_JSON") {
		t.Fatal("protocol rendered without a project goal")
	}

	if _, err := s.SetProjectGoal(ctx, "goal-project", "Ship the verified milestone", "Record intent", domain.AdaptiveActor{Kind: "USER", ID: "human"}); err != nil {
		t.Fatal(err)
	}
	sealed, err := m.buildSystemPrompt(ctx, domain.KindOrchestrator, "goal-project")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sealed, "AO_ORCHESTRATOR_TOOLS_JSON") || !strings.Contains(sealed, "\"goal\":{\"version\":1}") || !strings.Contains(sealed, "running.json") {
		t.Fatal("sealed planning protocol missing goal pin or literal routing")
	}

	worker, err := m.buildSystemPrompt(ctx, domain.KindWorker, "goal-project")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(worker, "AO_ORCHESTRATOR_TOOLS_JSON") {
		t.Fatal("planning protocol leaked into worker prompts")
	}
}
