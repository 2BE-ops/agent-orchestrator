package ports

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Knowledge errors preserve authority, provenance and version failures.
var (
	ErrKnowledgeNotFound  = errors.New("knowledge or version not found")
	ErrKnowledgeConflict  = errors.New("knowledge version conflict")
	ErrKnowledgeForbidden = errors.New("knowledge action is not permitted")
	ErrKnowledgeInvalid   = errors.New("invalid project knowledge")
)

// ProjectKnowledgeStore owns immutable claims, review authority and provenance.
type ProjectKnowledgeStore interface {
	CreateProjectKnowledge(context.Context, string, domain.ProjectID, domain.KnowledgeDefinition, domain.KnowledgeMutation) (domain.ProjectKnowledge, error)
	ReviseProjectKnowledge(context.Context, string, domain.KnowledgeDefinition, domain.KnowledgeMutation) (domain.KnowledgeVersion, error)
	GetProjectKnowledge(context.Context, string) (domain.ProjectKnowledge, error)
	ListProjectKnowledge(context.Context, domain.KnowledgeFilter) ([]domain.ProjectKnowledge, error)
	GetKnowledgeVersion(context.Context, string, int64) (domain.KnowledgeVersion, error)
	ListKnowledgeVersions(context.Context, string, int64, int) ([]domain.KnowledgeVersion, error)
}
