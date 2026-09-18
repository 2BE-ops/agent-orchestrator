// Package registry is the shared authoring boundary for user and Agent Manager
// definitions. It does not own worker lifecycle or native skill discovery.
package registry

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Manager applies shared authoring validation before atomic persistence.
type Manager struct {
	store  ports.RegistryStore
	native NativeConfiguration
}

// New constructs the registry service over the daemon's existing store.
func New(store ports.RegistryStore) *Manager { return &Manager{store: store} }

// NewWithNative uses the daemon's shared native catalog/readiness authority.
func NewWithNative(store ports.RegistryStore, native NativeConfiguration) *Manager {
	return &Manager{store: store, native: native}
}

// View combines a stable registry identity with its pinned active configuration.
type View struct {
	Entry   domain.RegistryEntry
	Version domain.RegistryVersion
}

// CreateInput contains authorable fields only. Actor/origin comes from the
// trusted application action context, not from this payload.
type CreateInput struct {
	Metadata   domain.RegistryMetadata   `json:"metadata"`
	Definition domain.RegistryDefinition `json:"definition"`
	Reason     string                    `json:"reason"`
}

// VersionInput appends configuration without silently promoting it.
type VersionInput struct {
	Definition       domain.RegistryDefinition `json:"definition"`
	ExpectedRevision int64                     `json:"expectedRevision"`
	Reason           string                    `json:"reason"`
}

// MetadataInput changes descriptive and ownership fields at a known revision.
type MetadataInput struct {
	Metadata         domain.RegistryMetadata `json:"metadata"`
	ExpectedRevision int64                   `json:"expectedRevision"`
	Reason           string                  `json:"reason"`
}

// ActivateInput changes the active pointer; all old versions remain available.
type ActivateInput struct {
	Version          int64  `json:"version"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

// CloneInput explicitly identifies the source version and new display name.
type CloneInput struct {
	Version int64  `json:"version"`
	Name    string `json:"name"`
	Reason  string `json:"reason"`
}

// List returns a stable, bounded page, including disabled definitions.
func (m *Manager) List(ctx context.Context, kind domain.RegistryKind, after string, limit int) ([]View, error) {
	if err := validatePage(limit); err != nil {
		return nil, err
	}
	entries, err := m.store.ListRegistryEntries(ctx, kind, after, limit)
	if err != nil {
		return nil, mapError(err)
	}
	views := make([]View, 0, len(entries))
	for _, entry := range entries {
		version, err := m.store.GetRegistryVersion(ctx, entry.ID, entry.ActiveVersion)
		if err != nil {
			return nil, mapError(err)
		}
		views = append(views, View{Entry: entry, Version: version})
	}
	return views, nil
}

// Get resolves the active immutable version and rejects cross-kind paths.
func (m *Manager) Get(ctx context.Context, kind domain.RegistryKind, id string) (View, error) {
	entry, err := m.entry(ctx, kind, id)
	if err != nil {
		return View{}, err
	}
	version, err := m.store.GetRegistryVersion(ctx, id, entry.ActiveVersion)
	if err != nil {
		return View{}, mapError(err)
	}
	return View{Entry: entry, Version: version}, nil
}

// Create is shared by human actions and validated Agent Manager proposals.
func (m *Manager) Create(ctx context.Context, actor domain.RegistryActor, kind domain.RegistryKind, input CreateInput) (View, error) {
	if err := input.Metadata.Validate(); err != nil {
		return View{}, invalid(err)
	}
	if err := input.Definition.Validate(kind); err != nil {
		return View{}, invalid(err)
	}
	mutation := domain.RegistryMutation{Actor: actor, Reason: input.Reason}
	if err := mutation.Validate(); err != nil {
		return View{}, invalid(err)
	}
	entry, err := m.store.CreateRegistryEntry(ctx, uuid.NewString(), kind, input.Metadata, input.Definition, mutation)
	if err != nil {
		return View{}, mapError(err)
	}
	version, err := m.store.GetRegistryVersion(ctx, entry.ID, entry.ActiveVersion)
	if err != nil {
		return View{}, mapError(err)
	}
	return View{Entry: entry, Version: version}, nil
}

// Versions returns bounded immutable history, not just the active version.
func (m *Manager) Versions(ctx context.Context, kind domain.RegistryKind, id string, after int64, limit int) ([]domain.RegistryVersion, error) {
	if err := validatePage(limit); err != nil {
		return nil, err
	}
	if _, err := m.entry(ctx, kind, id); err != nil {
		return nil, err
	}
	versions, err := m.store.ListRegistryVersions(ctx, id, after, limit)
	return versions, mapError(err)
}

// Version returns an exact historical configuration for comparison or export.
func (m *Manager) Version(ctx context.Context, kind domain.RegistryKind, id string, number int64) (domain.RegistryVersion, error) {
	if _, err := m.entry(ctx, kind, id); err != nil {
		return domain.RegistryVersion{}, err
	}
	version, err := m.store.GetRegistryVersion(ctx, id, number)
	return version, mapError(err)
}

// Append creates an inactive configuration version with an audited parent.
func (m *Manager) Append(ctx context.Context, actor domain.RegistryActor, kind domain.RegistryKind, id string, input VersionInput) (domain.RegistryVersion, error) {
	if _, err := m.entry(ctx, kind, id); err != nil {
		return domain.RegistryVersion{}, err
	}
	if err := input.Definition.Validate(kind); err != nil {
		return domain.RegistryVersion{}, invalid(err)
	}
	mutation := domain.RegistryMutation{Actor: actor, ExpectedRevision: input.ExpectedRevision, Reason: input.Reason}
	if err := validateMutation(mutation); err != nil {
		return domain.RegistryVersion{}, err
	}
	version, err := m.store.AppendRegistryVersion(ctx, id, input.Definition, mutation)
	return version, mapError(err)
}

// Update changes metadata through the same ownership rules for all callers.
func (m *Manager) Update(ctx context.Context, actor domain.RegistryActor, kind domain.RegistryKind, id string, input MetadataInput) (domain.RegistryEntry, error) {
	if _, err := m.entry(ctx, kind, id); err != nil {
		return domain.RegistryEntry{}, err
	}
	if err := input.Metadata.Validate(); err != nil {
		return domain.RegistryEntry{}, invalid(err)
	}
	mutation := domain.RegistryMutation{Actor: actor, ExpectedRevision: input.ExpectedRevision, Reason: input.Reason}
	if err := validateMutation(mutation); err != nil {
		return domain.RegistryEntry{}, err
	}
	entry, err := m.store.UpdateRegistryMetadata(ctx, id, input.Metadata, mutation)
	return entry, mapError(err)
}

// Activate selects a version for future work; it never edits that version.
func (m *Manager) Activate(ctx context.Context, actor domain.RegistryActor, kind domain.RegistryKind, id string, input ActivateInput) (domain.RegistryEntry, error) {
	if _, err := m.entry(ctx, kind, id); err != nil {
		return domain.RegistryEntry{}, err
	}
	if input.Version < 1 {
		return domain.RegistryEntry{}, apierr.Invalid("INVALID_REGISTRY_VERSION", "Version must be positive", nil)
	}
	mutation := domain.RegistryMutation{Actor: actor, ExpectedRevision: input.ExpectedRevision, Reason: input.Reason}
	if err := validateMutation(mutation); err != nil {
		return domain.RegistryEntry{}, err
	}
	entry, err := m.store.ActivateRegistryVersion(ctx, id, input.Version, mutation)
	return entry, mapError(err)
}

// Clone reuses the normal creation path and pins an explicitly selected source.
func (m *Manager) Clone(ctx context.Context, actor domain.RegistryActor, kind domain.RegistryKind, id string, input CloneInput) (View, error) {
	entry, err := m.entry(ctx, kind, id)
	if err != nil {
		return View{}, err
	}
	if actor.Origin == domain.RegistryManager && (!entry.Metadata.Enabled || !entry.Metadata.Policy.ManagerCanSelect) {
		return View{}, mapError(ports.ErrRegistryForbidden)
	}
	version, err := m.store.GetRegistryVersion(ctx, id, input.Version)
	if err != nil {
		return View{}, mapError(err)
	}
	metadata := entry.Metadata
	metadata.Name = input.Name
	return m.Create(ctx, actor, kind, CreateInput{Metadata: metadata, Definition: version.Definition, Reason: fmt.Sprintf("Clone %s v%d: %s", id, input.Version, input.Reason)})
}

// Audit returns durable mutation provenance for this definition.
func (m *Manager) Audit(ctx context.Context, kind domain.RegistryKind, id string, after int64, limit int) ([]domain.RegistryAudit, error) {
	if err := validatePage(limit); err != nil {
		return nil, err
	}
	if _, err := m.entry(ctx, kind, id); err != nil {
		return nil, err
	}
	items, err := m.store.ListRegistryAudit(ctx, id, after, limit)
	return items, mapError(err)
}

func (m *Manager) entry(ctx context.Context, kind domain.RegistryKind, id string) (domain.RegistryEntry, error) {
	entry, err := m.store.GetRegistryEntry(ctx, id)
	if err != nil {
		return domain.RegistryEntry{}, mapError(err)
	}
	if entry.Kind != kind {
		return domain.RegistryEntry{}, mapError(ports.ErrRegistryNotFound)
	}
	return entry, nil
}

func validateMutation(mutation domain.RegistryMutation) error {
	if err := mutation.Validate(); err != nil {
		return invalid(err)
	}
	if mutation.ExpectedRevision < 1 {
		return apierr.Invalid("INVALID_REGISTRY_REVISION", "Expected revision must be positive", nil)
	}
	return nil
}

func validatePage(limit int) error {
	if limit < 1 || limit > 200 {
		return apierr.Invalid("INVALID_REGISTRY_PAGE", "Page limit must be between 1 and 200", nil)
	}
	return nil
}

func invalid(err error) error { return apierr.Invalid("INVALID_REGISTRY_DEFINITION", err.Error(), nil) }

func mapError(err error) error {
	switch {
	case errors.Is(err, ports.ErrRegistryInvalid):
		return invalid(err)
	case errors.Is(err, ports.ErrRegistryNotFound):
		return apierr.NotFound("REGISTRY_NOT_FOUND", "Definition or version was not found")
	case errors.Is(err, ports.ErrRegistryConflict):
		return apierr.Conflict("REGISTRY_REVISION_CONFLICT", "Definition changed; reload before saving", nil)
	case errors.Is(err, ports.ErrRegistryForbidden):
		return apierr.Forbidden("REGISTRY_POLICY_DENIED", "Ownership policy does not allow this action")
	default:
		return err
	}
}
