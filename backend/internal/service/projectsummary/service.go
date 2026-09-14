package projectsummary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Store supplies the durable facts and projection used by the summary service.
type Store interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListSessions(ctx context.Context, project domain.ProjectID) ([]domain.SessionRecord, error)
	ListPRFactsForSessions(ctx context.Context, ids []domain.SessionID) (map[domain.SessionID][]domain.PRFacts, error)
	GetProjectSummary(ctx context.Context, projectID domain.ProjectID) (domain.ProjectSummary, bool, error)
	PutProjectSummary(ctx context.Context, summary domain.ProjectSummary) error
}

// Service builds and persists project summary projections.
type Service struct {
	store Store
	clock func() time.Time
}

// New constructs a project summary service.
func New(store Store) *Service { return &Service{store: store, clock: time.Now} }

// Get reads the current projection and regenerates it when requested or missing.
func (s *Service) Get(ctx context.Context, projectID domain.ProjectID, refresh bool) (domain.ProjectSummary, error) {
	if _, ok, err := s.store.GetProject(ctx, string(projectID)); err != nil {
		return domain.ProjectSummary{}, err
	} else if !ok {
		return domain.ProjectSummary{}, fmt.Errorf("project %s not found", projectID)
	}
	sessions, err := s.store.ListSessions(ctx, projectID)
	if err != nil {
		return domain.ProjectSummary{}, err
	}
	workerIDs := make([]domain.SessionID, 0, len(sessions))
	workers := make([]domain.SessionRecord, 0, len(sessions))
	for _, session := range sessions {
		if session.Kind == domain.KindWorker {
			workers = append(workers, session)
			workerIDs = append(workerIDs, session.ID)
		}
	}
	prs, err := s.store.ListPRFactsForSessions(ctx, workerIDs)
	if err != nil {
		return domain.ProjectSummary{}, err
	}
	next := project(projectID, workers, prs, s.clock())
	current, ok, err := s.store.GetProjectSummary(ctx, projectID)
	if err != nil {
		return domain.ProjectSummary{}, err
	}
	if ok && current.SourceWatermark == next.SourceWatermark {
		return current, nil
	}
	if !refresh && ok {
		return current, nil
	}
	if ok {
		next.NeedsAttention = preserveAttention(current.NeedsAttention, next.NeedsAttention)
		next.Narrative = narrative(next)
	}
	if err := s.store.PutProjectSummary(ctx, next); err != nil {
		return domain.ProjectSummary{}, err
	}
	return next, nil
}

func preserveAttention(previous, observed []domain.ProjectAttentionItem) []domain.ProjectAttentionItem {
	bySession := make(map[domain.SessionID]domain.ProjectAttentionItem, len(previous)+len(observed))
	for _, item := range previous {
		bySession[item.SessionID] = item
	}
	for _, item := range observed {
		bySession[item.SessionID] = item
	}
	result := make([]domain.ProjectAttentionItem, 0, len(bySession))
	for _, item := range bySession {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SessionID < result[j].SessionID })
	return result
}

func project(projectID domain.ProjectID, workers []domain.SessionRecord, prs map[domain.SessionID][]domain.PRFacts, at time.Time) domain.ProjectSummary {
	sort.Slice(workers, func(i, j int) bool { return workers[i].ID < workers[j].ID })
	h := sha256.New()
	result := domain.ProjectSummary{ProjectID: projectID, GeneratedAt: at, NeedsAttention: []domain.ProjectAttentionItem{}, Outputs: []domain.ProjectSummaryOutput{}}
	for _, worker := range workers {
		_, _ = fmt.Fprintf(h, "%s|%s|%t|%s|%s;", worker.ID, worker.Activity.State, worker.IsTerminated, worker.UpdatedAt.UTC(), worker.DisplayName)
		if worker.IsTerminated {
			result.CompletedWorkers++
		} else {
			result.ActiveWorkers++
		}
		if !worker.IsTerminated && worker.Activity.State == domain.ActivityWaitingInput {
			question := strings.TrimSpace(worker.Metadata.LatestAssistantUpdate)
			if question == "" {
				question = "This worker needs a decision before it can continue."
			}
			result.NeedsAttention = append(result.NeedsAttention, domain.ProjectAttentionItem{SessionID: worker.ID, SessionName: displayName(worker), Question: question})
		}
		rows := append([]domain.PRFacts(nil), prs[worker.ID]...)
		sort.Slice(rows, func(i, j int) bool { return rows[i].URL < rows[j].URL })
		for _, pr := range rows {
			_, _ = fmt.Fprintf(h, "%s|%s|%s|%s;", pr.URL, pr.CI, pr.Review, pr.UpdatedAt.UTC())
			state := "Open"
			if pr.Merged {
				state = "Merged"
			} else if pr.Closed {
				state = "Closed"
			} else if pr.CI == domain.CIFailing {
				state = "Checks failing"
			} else if pr.CI == domain.CIPassing {
				state = "Checks passing"
			}
			result.Outputs = append(result.Outputs, domain.ProjectSummaryOutput{SessionID: worker.ID, SessionName: displayName(worker), Kind: "pull_request", URL: pr.URL, Number: pr.Number, State: state})
		}
	}
	result.SourceWatermark = hex.EncodeToString(h.Sum(nil))
	result.Narrative = narrative(result)
	return result
}

func displayName(session domain.SessionRecord) string {
	if strings.TrimSpace(session.DisplayName) != "" {
		return session.DisplayName
	}
	return string(session.ID)
}
func narrative(summary domain.ProjectSummary) string {
	if summary.ActiveWorkers == 0 && summary.CompletedWorkers == 0 {
		return "No worker activity has been recorded for this project yet."
	}
	parts := []string{fmt.Sprintf("%d worker%s active", summary.ActiveWorkers, plural(summary.ActiveWorkers))}
	if summary.CompletedWorkers > 0 {
		parts = append(parts, fmt.Sprintf("%d completed", summary.CompletedWorkers))
	}
	if len(summary.Outputs) > 0 {
		parts = append(parts, fmt.Sprintf("%d pull request%s tracked", len(summary.Outputs), plural(len(summary.Outputs))))
	}
	text := strings.Join(parts, ", ") + "."
	if len(summary.NeedsAttention) > 0 {
		text += fmt.Sprintf(" %d decision%s need your input.", len(summary.NeedsAttention), plural(len(summary.NeedsAttention)))
	}
	return text
}
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
