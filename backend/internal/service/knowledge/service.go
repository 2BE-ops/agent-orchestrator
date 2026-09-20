// Package knowledge owns reviewable project claims and exact history. It never
// fetches source URLs, reads repository files, or treats candidate claims as facts.
package knowledge

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Store is shared by human authoring, worker proposals and context selection.
type Store interface {
	ports.ProjectKnowledgeStore
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
}

// Manager applies the same knowledge validation across daemon action callers.
type Manager struct{ store Store }

// New constructs the knowledge service without external side effects.
func New(store Store) *Manager { return &Manager{store: store} }

// CreateInput records a claim and its explicit review disposition.
type CreateInput struct {
	Definition domain.KnowledgeDefinition `json:"definition"`
	Reason     string                     `json:"reason"`
}

// RevisionInput edits, pins, invalidates, supersedes or withdraws future context.
type RevisionInput struct {
	Definition      domain.KnowledgeDefinition `json:"definition"`
	ExpectedVersion int64                      `json:"expectedVersion"`
	Reason          string                     `json:"reason"`
}

// View resolves the current pointer once and retains exact content provenance.
type View struct {
	Knowledge domain.ProjectKnowledge `json:"knowledge"`
	Version   domain.KnowledgeVersion `json:"version"`
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrKnowledgeNotFound):
		return apierr.NotFound("KNOWLEDGE_NOT_FOUND", "Knowledge, version or provenance was not found")
	case errors.Is(err, ports.ErrKnowledgeForbidden):
		return apierr.Forbidden("KNOWLEDGE_FORBIDDEN", "Knowledge review or source ownership is not permitted")
	case errors.Is(err, ports.ErrKnowledgeConflict):
		return apierr.Conflict("KNOWLEDGE_VERSION_CONFLICT", "Knowledge changed; reload its current version", nil)
	case errors.Is(err, ports.ErrKnowledgeInvalid):
		return apierr.Invalid("INVALID_KNOWLEDGE", err.Error(), nil)
	default:
		return err
	}
}

// Create accepts actor identity only from trusted daemon action context.
func (m *Manager) Create(ctx context.Context, actor domain.AdaptiveActor, projectID domain.ProjectID, input CreateInput) (View, error) {
	identity, err := m.store.CreateProjectKnowledge(ctx, uuid.NewString(), projectID, input.Definition, domain.KnowledgeMutation{Actor: actor, Reason: input.Reason})
	if err != nil {
		return View{}, mapError(err)
	}
	return m.view(ctx, identity)
}

func (m *Manager) view(ctx context.Context, identity domain.ProjectKnowledge) (View, error) {
	version, err := m.store.GetKnowledgeVersion(ctx, identity.ID, identity.Version)
	return View{Knowledge: identity, Version: version}, mapError(err)
}

// Get includes soft-deleted knowledge for explicit provenance inspection.
func (m *Manager) Get(ctx context.Context, id string) (View, error) {
	identity, err := m.store.GetProjectKnowledge(ctx, id)
	if err != nil {
		return View{}, mapError(err)
	}
	return m.view(ctx, identity)
}

// Revise retains the prior claim; accepting a candidate is an explicit revision.
func (m *Manager) Revise(ctx context.Context, actor domain.AdaptiveActor, id string, input RevisionInput) (domain.KnowledgeVersion, error) {
	version, err := m.store.ReviseProjectKnowledge(ctx, id, input.Definition, domain.KnowledgeMutation{Actor: actor, Reason: input.Reason, ExpectedVersion: input.ExpectedVersion})
	return version, mapError(err)
}

// Version reads exact immutable history for manifests and audit.
func (m *Manager) Version(ctx context.Context, id string, number int64) (domain.KnowledgeVersion, error) {
	if number < 1 {
		return domain.KnowledgeVersion{}, apierr.Invalid("INVALID_KNOWLEDGE_VERSION", "Version must be positive", nil)
	}
	version, err := m.store.GetKnowledgeVersion(ctx, id, number)
	return version, mapError(err)
}

// Versions pages retained review history in version order.
func (m *Manager) Versions(ctx context.Context, id string, after int64, limit int) ([]domain.KnowledgeVersion, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, apierr.Invalid("INVALID_KNOWLEDGE_PAGE", "Cursor must be non-negative and limit between 1 and 100", nil)
	}
	if _, err := m.store.GetProjectKnowledge(ctx, id); err != nil {
		return nil, mapError(err)
	}
	versions, err := m.store.ListKnowledgeVersions(ctx, id, after, limit)
	return versions, mapError(err)
}

// List searches current project content with bounded pages and literal text.
func (m *Manager) List(ctx context.Context, filter domain.KnowledgeFilter) ([]View, error) {
	switch filter.Status {
	case "", "candidate", "accepted", "invalidated", "superseded", "deleted":
	default:
		return nil, apierr.Invalid("INVALID_KNOWLEDGE_FILTER", "Unknown review status", nil)
	}
	switch filter.Kind {
	case "", "architecture", "convention", "interface", "constraint", "pitfall", "failed_approach", "file_relationship", "external_behavior", "question":
	default:
		return nil, apierr.Invalid("INVALID_KNOWLEDGE_FILTER", "Unknown knowledge kind", nil)
	}
	_, found, err := m.store.GetProject(ctx, string(filter.ProjectID))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, mapError(ports.ErrKnowledgeNotFound)
	}
	identities, err := m.store.ListProjectKnowledge(ctx, filter)
	if err != nil {
		return nil, mapError(err)
	}
	views := make([]View, 0, len(identities))
	for _, identity := range identities {
		view, err := m.view(ctx, identity)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}
