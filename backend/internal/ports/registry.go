package ports

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Registry mutation errors retain their identity across service/API boundaries.
var (
	ErrRegistryNotFound  = errors.New("registry entry or version not found")
	ErrRegistryConflict  = errors.New("registry revision conflict")
	ErrRegistryForbidden = errors.New("registry ownership policy forbids action")
)

// RegistryStore atomically persists typed definitions, immutable versions,
// ownership policy and audit history. It never starts or configures a process.
type RegistryStore interface {
	CreateRegistryEntry(context.Context, string, domain.RegistryKind, domain.RegistryMetadata, domain.RegistryDefinition, domain.RegistryMutation) (domain.RegistryEntry, error)
	GetRegistryEntry(context.Context, string) (domain.RegistryEntry, error)
	ListRegistryEntries(context.Context, domain.RegistryKind, string, int) ([]domain.RegistryEntry, error)
	GetRegistryVersion(context.Context, string, int64) (domain.RegistryVersion, error)
	ListRegistryVersions(context.Context, string, int64, int) ([]domain.RegistryVersion, error)
	AppendRegistryVersion(context.Context, string, domain.RegistryDefinition, domain.RegistryMutation) (domain.RegistryVersion, error)
	UpdateRegistryMetadata(context.Context, string, domain.RegistryMetadata, domain.RegistryMutation) (domain.RegistryEntry, error)
	ActivateRegistryVersion(context.Context, string, int64, domain.RegistryMutation) (domain.RegistryEntry, error)
	ListRegistryAudit(context.Context, string, int64, int) ([]domain.RegistryAudit, error)
}
