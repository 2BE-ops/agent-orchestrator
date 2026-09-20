package agentmanager

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

// CandidateAssessor reuses registry/native configuration validation. It never
// provides semantic ranking or authority to create a process.
type CandidateAssessor interface {
	AssessManagerCandidate(context.Context, string, int64, domain.TaskDefinition, domain.ProjectRecord) (domain.AgentManagerCandidate, error)
}

// SetCandidateAssessor wires the daemon's existing registry service at startup.
func (m *Manager) SetCandidateAssessor(assessor CandidateAssessor) { m.candidates = assessor }

type candidateStore interface {
	ListRegistryEntries(context.Context, domain.RegistryKind, string, int) ([]domain.RegistryEntry, error)
	GetTaskRevision(context.Context, string, int64) (domain.TaskRevision, error)
	GetAdaptiveTask(context.Context, string) (domain.AdaptiveTask, error)
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
}

// CandidatePage includes exclusions and a cursor over all Types. A page is never
// represented as the complete registry or as a persisted selection decision.
type CandidatePage struct {
	RequestHash        string                         `json:"requestHash"`
	TaskClassification domain.ContextClass            `json:"taskClassification" enum:"technical,engagement,mission"`
	Items              []domain.AgentManagerCandidate `json:"items"`
	NextCursor         string                         `json:"nextCursor,omitempty"`
	ObservedAt         time.Time                      `json:"observedAt"`
}

func (m *Manager) candidateInput(ctx context.Context, project domain.ProjectID, requestID string) (candidateStore, domain.AgentManagerRequest, domain.TaskRevision, domain.ProjectRecord, error) {
	var revision domain.TaskRevision
	var rec domain.ProjectRecord
	request, err := m.Request(ctx, project, requestID)
	if err != nil {
		return nil, request, revision, rec, err
	}
	store, ok := m.store.(candidateStore)
	if !ok || m.candidates == nil {
		return nil, request, revision, rec, apierr.NotImplemented("AGENT_MANAGER_CANDIDATES_UNAVAILABLE", "Manager candidate assessment is unavailable")
	}
	governance, err := m.Get(ctx, project)
	if err != nil {
		return nil, request, revision, rec, err
	}
	task, err := store.GetAdaptiveTask(ctx, request.TaskID)
	if err != nil {
		return nil, request, revision, rec, mapInboxError(err)
	}
	if !governance.Definition.Enabled || governance.ContentHash != request.ConfigurationHash || task.Revision != request.TaskRevision {
		return nil, request, revision, rec, apierr.Conflict("AGENT_MANAGER_WORK_FENCED", "Current governance or task intent supersedes this routing request", nil)
	}
	revision, err = store.GetTaskRevision(ctx, request.TaskID, request.TaskRevision)
	if err != nil {
		return nil, request, revision, rec, mapInboxError(err)
	}
	if revision.ContentHash != request.TaskContentHash {
		return nil, request, revision, rec, apierr.Conflict("AGENT_MANAGER_WORK_FENCED", "Routing requirements do not match their sealed hash", nil)
	}
	var found bool
	rec, found, err = store.GetProject(ctx, string(project))
	if err != nil {
		return nil, request, revision, rec, err
	}
	if !found {
		return nil, request, revision, rec, apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
	}
	return store, request, revision, rec, nil
}

// Candidates checks a bounded page of active exact versions, including rejected
// definitions. After a full page, callers must follow nextCursor before concluding
// no suitable reusable Type exists. Registry changes can require a fresh scan.
func (m *Manager) Candidates(ctx context.Context, project domain.ProjectID, requestID, after string, limit int) (CandidatePage, error) {
	page := CandidatePage{Items: []domain.AgentManagerCandidate{}}
	if limit < 1 || limit > 20 {
		return page, apierr.Invalid("INVALID_MANAGER_CANDIDATE_PAGE", "Candidate page limit must be 1 to 20", nil)
	}
	if after != "" {
		if err := validateProject(domain.ProjectID(after)); err != nil {
			return page, err
		}
	}
	store, request, revision, rec, err := m.candidateInput(ctx, project, requestID)
	if err != nil {
		return page, err
	}
	entries, err := store.ListRegistryEntries(ctx, domain.RegistryAgentType, after, limit+1)
	if err != nil {
		return page, err
	}
	if len(entries) > limit {
		entries = entries[:limit]
		page.NextCursor = entries[len(entries)-1].ID
	}
	for _, entry := range entries {
		candidate, err := m.candidates.AssessManagerCandidate(ctx, entry.ID, entry.ActiveVersion, revision.Definition, rec)
		if err != nil {
			return CandidatePage{}, err
		}
		page.Items = append(page.Items, candidate)
	}
	page.RequestHash, page.TaskClassification, page.ObservedAt = request.ContentHash, revision.Definition.Classification.Effective(), time.Now().UTC()
	return page, nil
}

// Candidate checks a specific historical version without following activation.
func (m *Manager) Candidate(ctx context.Context, project domain.ProjectID, requestID, typeID string, version int64) (domain.AgentManagerCandidate, error) {
	if err := validateProject(domain.ProjectID(typeID)); err != nil {
		return domain.AgentManagerCandidate{}, err
	}
	if version < 1 {
		return domain.AgentManagerCandidate{}, apierr.Invalid("INVALID_MANAGER_CANDIDATE", "An exact Type version is required", nil)
	}
	_, _, revision, rec, err := m.candidateInput(ctx, project, requestID)
	if err != nil {
		return domain.AgentManagerCandidate{}, err
	}
	return m.candidates.AssessManagerCandidate(ctx, typeID, version, revision.Definition, rec)
}
