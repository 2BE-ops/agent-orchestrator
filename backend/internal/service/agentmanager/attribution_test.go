package agentmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func routingAPIError(t *testing.T, err error) string {
	t.Helper()
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected typed api error, got %v", err)
	}
	return apiErr.Code
}

func TestManagerRoutingAttributionReads(t *testing.T) {
	ctx := context.Background()
	store := sqlitetest.MustOpenAt(t, t.TempDir())
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{ID: "project", Path: "/repo", RegisteredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	m := New(store)
	outcomes, err := m.RoutingOutcomes(ctx, "project", 0, 20)
	if err != nil || len(outcomes) != 0 {
		t.Fatalf("empty routing outcomes: %+v %v", outcomes, err)
	}
	now := time.Now().UTC()
	summary, err := m.RoutingSummary(ctx, "project", domain.OutcomeAttributionQuery{ProjectID: "project", From: now.Add(-time.Hour), To: now.Add(time.Hour)})
	if err != nil || summary.Totals.Decisions != 0 || len(summary.Types) != 0 {
		t.Fatalf("empty routing summary: %+v %v", summary, err)
	}
	if _, err := m.RoutingOutcomes(ctx, "project", 0, 0); routingAPIError(t, err) == "" {
		t.Fatal("invalid page accepted")
	}
	if _, err := m.RoutingOutcomes(ctx, "project", -1, 20); routingAPIError(t, err) == "" {
		t.Fatal("negative cursor accepted")
	}
	if _, err := m.RoutingOutcomes(ctx, "project", 0, 101); routingAPIError(t, err) == "" {
		t.Fatal("oversized page accepted")
	}
	if _, err := m.RoutingSummary(ctx, "project", domain.OutcomeAttributionQuery{ProjectID: "project", From: now, To: now.Add(-time.Hour)}); routingAPIError(t, err) == "" {
		t.Fatal("inverted window accepted")
	}
	if _, err := m.RoutingSummary(ctx, "project", domain.OutcomeAttributionQuery{ProjectID: "project", From: time.Time{}, To: now}); routingAPIError(t, err) == "" {
		t.Fatal("missing window bound accepted")
	}
}
