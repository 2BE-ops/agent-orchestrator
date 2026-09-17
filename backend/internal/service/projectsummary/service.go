package projectsummary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const summaryProjectionVersion = "2"

// Store supplies the durable facts and projection used by the summary service.
type Store interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListSessions(ctx context.Context, project domain.ProjectID) ([]domain.SessionRecord, error)
	ListPRFactsForSessions(ctx context.Context, ids []domain.SessionID) (map[domain.SessionID][]domain.PRFacts, error)
	ListWorkspaceRepos(ctx context.Context, projectID string) ([]domain.WorkspaceRepoRecord, error)
	GetProjectSummary(ctx context.Context, projectID domain.ProjectID) (domain.ProjectSummary, bool, error)
	PutProjectSummary(ctx context.Context, summary domain.ProjectSummary) error
}

// ReportOutputFact is an opaque output reference from a persisted worker report.
type ReportOutputFact struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	Label     string `json:"label,omitempty"`
}

// ReportFact is the delivery-independent subset of a persisted worker report.
type ReportFact struct {
	ID          string
	SessionID   domain.SessionID
	ProjectID   domain.ProjectID
	State       string
	Note        string
	Message     string
	Outputs     []ReportOutputFact
	CreatedAt   time.Time
	RepeatCount int64
}

// GenerationReport is the meaningful worker-authored context sent to the narrative generator.
type GenerationReport struct {
	SessionID   domain.SessionID   `json:"sessionId"`
	SessionName string             `json:"sessionName"`
	State       string             `json:"state,omitempty"`
	Note        string             `json:"note,omitempty"`
	Message     string             `json:"message,omitempty"`
	Outputs     []ReportOutputFact `json:"outputs,omitempty"`
	CreatedAt   time.Time          `json:"createdAt"`
	RepeatCount int64              `json:"repeatCount,omitempty"`
}

// ReportReader returns persisted reports in creation and id order without mutating delivery state.
type ReportReader interface {
	ListProject(context.Context, domain.ProjectID) ([]ReportFact, error)
}

// Service builds and persists project summary projections.
type Service struct {
	store     Store
	generator NarrativeGenerator
	reports   ReportReader
	clock     func() time.Time
	locks     sync.Map
}

// New constructs a project summary service.
func New(store Store, generator NarrativeGenerator, reports ...ReportReader) *Service {
	service := &Service{store: store, generator: generator, clock: time.Now}
	if len(reports) > 0 {
		service.reports = reports[0]
	}
	return service
}

// Get reads the current projection and regenerates it when requested or missing.
func (s *Service) Get(ctx context.Context, projectID domain.ProjectID, refresh bool) (domain.ProjectSummary, error) {
	lock, _ := s.locks.LoadOrStore(projectID, &sync.Mutex{})
	projectLock, ok := lock.(*sync.Mutex)
	if !ok {
		return domain.ProjectSummary{}, fmt.Errorf("project %s summary lock has unexpected type", projectID)
	}
	projectLock.Lock()
	defer projectLock.Unlock()

	projectRecord, ok, err := s.store.GetProject(ctx, string(projectID))
	if err != nil {
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
	var workspaceRepos []domain.WorkspaceRepoRecord
	if projectRecord.Kind.WithDefault() == domain.ProjectKindWorkspace {
		workspaceRepos, err = s.store.ListWorkspaceRepos(ctx, projectRecord.ID)
		if err != nil {
			return domain.ProjectSummary{}, fmt.Errorf("list workspace repositories: %w", err)
		}
	}
	prs = projectPRFacts(projectRecord, workspaceRepos, prs)
	var reports []ReportFact
	if s.reports != nil {
		reports, err = s.reports.ListProject(ctx, projectID)
		if err != nil {
			return domain.ProjectSummary{}, fmt.Errorf("list project reports: %w", err)
		}
	}
	next := project(projectID, workers, prs, reports, s.clock())
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
	}
	harness := projectRecord.Config.Orchestrator.Harness
	model := projectRecord.Config.Orchestrator.AgentConfig.Model
	for _, session := range sessions {
		if session.Kind == domain.KindOrchestrator && !session.IsTerminated {
			harness = session.Harness
			break
		}
	}
	if model == "" {
		model = projectRecord.Config.AgentConfig.Model
	}
	if s.generator == nil {
		return failedGeneration(current, ok, "project summary generator is unavailable"), nil
	}
	narrative, err := s.generator.Update(ctx, GenerationRequest{Harness: harness, Model: model, WorkspacePath: projectRecord.Path, Existing: current.Narrative, Facts: next, Reports: generationReports(reports, workers)})
	if err != nil {
		return failedGeneration(current, ok, err.Error()), nil
	}
	next.Narrative = narrative
	if err := s.store.PutProjectSummary(ctx, next); err != nil {
		return domain.ProjectSummary{}, err
	}
	return next, nil
}

func generationReports(reports []ReportFact, workers []domain.SessionRecord) []GenerationReport {
	result := make([]GenerationReport, 0, len(reports))
	for _, report := range reports {
		result = append(result, GenerationReport{
			SessionID: report.SessionID, SessionName: workerName(workers, report.SessionID), State: report.State,
			Note: report.Note, Message: report.Message, Outputs: report.Outputs, CreatedAt: report.CreatedAt, RepeatCount: report.RepeatCount,
		})
	}
	return result
}

func projectPRFacts(project domain.ProjectRecord, repos []domain.WorkspaceRepoRecord, facts map[domain.SessionID][]domain.PRFacts) map[domain.SessionID][]domain.PRFacts {
	allowed := make([]domain.RepositoryIdentity, 0, len(repos)+2)
	for _, raw := range append([]string{project.RepoOriginURL, project.Config.CanonicalRepoURL}, repoOrigins(repos)...) {
		if identity, err := domain.ParseRepositoryIdentity(raw); err == nil {
			allowed = append(allowed, identity)
		}
	}
	if len(allowed) == 0 {
		return facts
	}
	filtered := make(map[domain.SessionID][]domain.PRFacts, len(facts))
	for sessionID, rows := range facts {
		filtered[sessionID] = []domain.PRFacts{}
		for _, row := range rows {
			identity, ok := pullRequestRepository(row.URL)
			if ok && repositoryAllowed(identity, allowed) {
				filtered[sessionID] = append(filtered[sessionID], row)
			}
		}
	}
	return filtered
}

func repoOrigins(repos []domain.WorkspaceRepoRecord) []string {
	result := make([]string, 0, len(repos))
	for _, repo := range repos {
		result = append(result, repo.RepoOriginURL)
	}
	return result
}

func pullRequestRepository(raw string) (domain.RepositoryIdentity, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return domain.RepositoryIdentity{}, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	repoParts := []string(nil)
	if domain.RepositoryProvider(u.Hostname()) == "github" {
		if len(parts) != 4 || parts[2] != "pull" {
			return domain.RepositoryIdentity{}, false
		}
		repoParts = parts[:2]
	} else {
		for i := 2; i+2 < len(parts); i++ {
			if parts[i] == "-" && parts[i+1] == "merge_requests" {
				repoParts = parts[:i]
				break
			}
		}
		if len(repoParts) < 2 {
			return domain.RepositoryIdentity{}, false
		}
	}
	identity, err := domain.ParseRepositoryIdentity("https://" + u.Host + "/" + strings.Join(repoParts, "/"))
	return identity, err == nil
}

func repositoryAllowed(candidate domain.RepositoryIdentity, allowed []domain.RepositoryIdentity) bool {
	for _, identity := range allowed {
		if strings.EqualFold(candidate.Host, identity.Host) && strings.EqualFold(candidate.Namespace, identity.Namespace) && strings.EqualFold(candidate.Name, identity.Name) {
			return true
		}
	}
	return false
}

func failedGeneration(current domain.ProjectSummary, exists bool, message string) domain.ProjectSummary {
	if !exists {
		current.NeedsAttention = []domain.ProjectAttentionItem{}
		current.Outputs = []domain.ProjectSummaryOutput{}
	}
	current.GenerationError = message
	return current
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

func project(projectID domain.ProjectID, workers []domain.SessionRecord, prs map[domain.SessionID][]domain.PRFacts, reports []ReportFact, at time.Time) domain.ProjectSummary {
	sort.Slice(workers, func(i, j int) bool { return workers[i].ID < workers[j].ID })
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "projection:%s;", summaryProjectionVersion)
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
	for _, report := range reports {
		_, _ = fmt.Fprintf(h, "%s|%s|%s|%s|%s|%d;", report.ID, report.SessionID, report.State, report.Note, report.Message, report.RepeatCount)
		if report.State == "needs_input" {
			question := strings.TrimSpace(report.Note)
			if question == "" {
				question = strings.TrimSpace(report.Message)
			}
			if question != "" {
				result.NeedsAttention = append(result.NeedsAttention, domain.ProjectAttentionItem{SessionID: report.SessionID, SessionName: workerName(workers, report.SessionID), Question: question})
			}
		}
		for _, output := range report.Outputs {
			_, _ = fmt.Fprintf(h, "%s|%s|%s;", output.Kind, output.Reference, output.Label)
			result.Outputs = append(result.Outputs, domain.ProjectSummaryOutput{SessionID: report.SessionID, SessionName: workerName(workers, report.SessionID), Kind: output.Kind, Reference: output.Reference, Label: output.Label})
		}
	}
	result.SourceWatermark = hex.EncodeToString(h.Sum(nil))
	return result
}

func workerName(workers []domain.SessionRecord, id domain.SessionID) string {
	for _, worker := range workers {
		if worker.ID == id {
			return displayName(worker)
		}
	}
	return string(id)
}

func displayName(session domain.SessionRecord) string {
	if strings.TrimSpace(session.DisplayName) != "" {
		return session.DisplayName
	}
	return string(session.ID)
}
