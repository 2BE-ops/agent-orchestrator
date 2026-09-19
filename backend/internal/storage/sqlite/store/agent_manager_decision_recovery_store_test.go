package store_test

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestManagerDecisionPendingScanAndCheckpointSurviveRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	input, a := managerDecisionFixture(t, s, domain.ContextTechnical)
	items, err := s.ListUnassessedAgentManagerProposals(ctx, "", 1)
	mustNoError(t, err)
	if len(items) != 1 || items[0].ProposalID != input.ID || items[0].RequestID != input.RequestID {
		t.Fatalf("pending references: %+v", items)
	}
	mustNoError(t, s.SetAgentManagerDecisionCursor(ctx, input.ID))
	mustNoError(t, s.Close())
	s, err = sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	cursor, err := s.AgentManagerDecisionCursor(ctx)
	mustNoError(t, err)
	if cursor != input.ID {
		t.Fatal("checkpoint lost")
	}
	items, err = s.ListUnassessedAgentManagerProposals(ctx, cursor, 1)
	mustNoError(t, err)
	if len(items) != 0 {
		t.Fatal("cursor replayed prior page")
	}
	for _, limit := range []int{0, 101} {
		if _, err := s.ListUnassessedAgentManagerProposals(ctx, "", limit); err == nil {
			t.Fatal("unbounded scan")
		}
	}
	if err := s.SetAgentManagerDecisionCursor(ctx, "bad\ncursor"); err == nil {
		t.Fatal("control cursor accepted")
	}
	_, _, err = s.RecordAgentManagerDecision(ctx, a)
	mustNoError(t, err)
	mustNoError(t, s.SetAgentManagerDecisionCursor(ctx, ""))
	items, err = s.ListUnassessedAgentManagerProposals(ctx, "", 100)
	mustNoError(t, err)
	if len(items) != 0 {
		t.Fatal("committed decision remained pending")
	}
}
