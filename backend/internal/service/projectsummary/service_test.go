package projectsummary

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type fakeStore struct {
	sessions   []domain.SessionRecord
	prs        map[domain.SessionID][]domain.PRFacts
	summary    domain.ProjectSummary
	hasSummary bool
	writes     int
}

func (f *fakeStore) GetProject(context.Context, string) (domain.ProjectRecord, bool, error) {
	return domain.ProjectRecord{ID: "demo"}, true, nil
}
func (f *fakeStore) ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error) {
	return f.sessions, nil
}
func (f *fakeStore) ListPRFactsForSessions(context.Context, []domain.SessionID) (map[domain.SessionID][]domain.PRFacts, error) {
	return f.prs, nil
}
func (f *fakeStore) GetProjectSummary(context.Context, domain.ProjectID) (domain.ProjectSummary, bool, error) {
	return f.summary, f.hasSummary, nil
}
func (f *fakeStore) PutProjectSummary(_ context.Context, summary domain.ProjectSummary) error {
	f.summary, f.hasSummary, f.writes = summary, true, f.writes+1
	return nil
}

func TestRefreshIsStableUntilObservedFactsChange(t *testing.T) {
	base := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	store := &fakeStore{sessions: []domain.SessionRecord{{ID: "demo-1", ProjectID: "demo", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityActive}, UpdatedAt: base}}, prs: map[domain.SessionID][]domain.PRFacts{}}
	svc := New(store)
	svc.clock = func() time.Time { return base }
	first, err := svc.Get(context.Background(), "demo", true)
	if err != nil {
		t.Fatal(err)
	}
	svc.clock = func() time.Time { return base.Add(time.Hour) }
	second, err := svc.Get(context.Background(), "demo", true)
	if err != nil {
		t.Fatal(err)
	}
	if store.writes != 1 {
		t.Fatalf("writes = %d, want 1", store.writes)
	}
	if !second.GeneratedAt.Equal(first.GeneratedAt) {
		t.Fatalf("no-op refresh changed generatedAt")
	}
	store.sessions[0].Activity.State = domain.ActivityWaitingInput
	store.sessions[0].Metadata.LatestAssistantUpdate = "Choose the API shape."
	store.sessions[0].UpdatedAt = base.Add(2 * time.Hour)
	third, err := svc.Get(context.Background(), "demo", true)
	if err != nil {
		t.Fatal(err)
	}
	if store.writes != 2 || len(third.NeedsAttention) != 1 {
		t.Fatalf("changed refresh = writes %d, attention %d", store.writes, len(third.NeedsAttention))
	}
}

func TestAttentionClearsWhenWorkerAdvances(t *testing.T) {
	base := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	store := &fakeStore{sessions: []domain.SessionRecord{{ID: "demo-1", ProjectID: "demo", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityWaitingInput}, UpdatedAt: base}}, prs: map[domain.SessionID][]domain.PRFacts{}}
	svc := New(store)
	svc.clock = func() time.Time { return base }
	if got, _ := svc.Get(context.Background(), "demo", true); len(got.NeedsAttention) != 1 {
		t.Fatalf("attention = %d, want 1", len(got.NeedsAttention))
	}
	store.sessions[0].Activity.State = domain.ActivityActive
	store.sessions[0].UpdatedAt = base.Add(time.Minute)
	if got, _ := svc.Get(context.Background(), "demo", true); len(got.NeedsAttention) != 0 {
		t.Fatalf("attention = %d, want cleared", len(got.NeedsAttention))
	}
}
