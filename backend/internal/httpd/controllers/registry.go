package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
)

// RegistryService is the authoring boundary shared with native manager tools.
type RegistryService interface {
	Export(context.Context, domain.RegistryKind, string, int64) (registrysvc.PortableBundle, error)
	Import(context.Context, domain.RegistryActor, domain.RegistryKind, registrysvc.ImportInput) (registrysvc.Imported, error)
	List(context.Context, domain.RegistryKind, string, int) ([]registrysvc.View, error)
	Get(context.Context, domain.RegistryKind, string) (registrysvc.View, error)
	Create(context.Context, domain.RegistryActor, domain.RegistryKind, registrysvc.CreateInput) (registrysvc.View, error)
	Versions(context.Context, domain.RegistryKind, string, int64, int) ([]domain.RegistryVersion, error)
	Version(context.Context, domain.RegistryKind, string, int64) (domain.RegistryVersion, error)
	Append(context.Context, domain.RegistryActor, domain.RegistryKind, string, registrysvc.VersionInput) (domain.RegistryVersion, error)
	Update(context.Context, domain.RegistryActor, domain.RegistryKind, string, registrysvc.MetadataInput) (domain.RegistryEntry, error)
	Activate(context.Context, domain.RegistryActor, domain.RegistryKind, string, registrysvc.ActivateInput) (domain.RegistryEntry, error)
	Clone(context.Context, domain.RegistryActor, domain.RegistryKind, string, registrysvc.CloneInput) (registrysvc.View, error)
	Audit(context.Context, domain.RegistryKind, string, int64, int) ([]domain.RegistryAudit, error)
}

// RegistryController mounts the human authoring API. Manager tool calls use the
// same service with an application-established manager actor, never a JSON role.
type RegistryController struct{ Svc RegistryService }

// Register exposes distinct Agent Type and Skill resources over shared rules.
func (c *RegistryController) Register(r chi.Router) {
	for _, resource := range []struct {
		path string
		kind domain.RegistryKind
	}{{"/agent-types", domain.RegistryAgentType}, {"/skills", domain.RegistrySkill}} {
		k := &registryKindController{svc: c.Svc, kind: resource.kind}
		r.Group(func(r chi.Router) {
			r.Use(k.available)
			r.Get(resource.path, k.list)
			r.Post(resource.path, k.create)
			r.Post(resource.path+"/import", k.importBundle)
			r.Get(resource.path+"/{id}", k.get)
			r.Patch(resource.path+"/{id}", k.update)
			r.Post(resource.path+"/{id}/clone", k.clone)
			r.Get(resource.path+"/{id}/versions", k.versions)
			r.Post(resource.path+"/{id}/versions", k.appendVersion)
			r.Get(resource.path+"/{id}/versions/{version}", k.version)
			r.Get(resource.path+"/{id}/versions/{version}/export", k.exportBundle)
			r.Post(resource.path+"/{id}/activate", k.activate)
			r.Get(resource.path+"/{id}/audit", k.audit)
		})
	}
}

type registryKindController struct {
	svc  RegistryService
	kind domain.RegistryKind
}

func (c *registryKindController) available(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.svc == nil {
			envelope.WriteError(w, r, apierr.NotImplemented("REGISTRY_UNAVAILABLE", "Registry service is unavailable"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func registryHumanActor() domain.RegistryActor {
	return domain.RegistryActor{Origin: domain.RegistryUser, ID: "local-user"}
}

func (c *registryKindController) importBundle(w http.ResponseWriter, r *http.Request) {
	var input registrysvc.ImportInput
	if !decodeRegistryBody(w, r, &input) {
		return
	}
	result, err := c.svc.Import(r.Context(), registryHumanActor(), c.kind, input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := RegistryImportResponse{Root: registryViewResponse(result.Root), ImportedSkills: make([]RegistryEntryResponse, 0, len(result.Skills)), Requirements: result.Requirements}
	for _, skill := range result.Skills {
		response.ImportedSkills = append(response.ImportedSkills, registryEntryResponse(skill))
	}
	envelope.WriteJSON(w, http.StatusCreated, response)
}

func (c *registryKindController) exportBundle(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.ParseInt(chi.URLParam(r, "version"), 10, 64)
	if err != nil || number < 1 {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_REGISTRY_VERSION", "Version must be positive", nil))
		return
	}
	bundle, err := c.svc.Export(r.Context(), c.kind, chi.URLParam(r, "id"), number)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, bundle)
}

func (c *registryKindController) list(w http.ResponseWriter, r *http.Request) {
	limit, err := registryLimit(r)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	views, err := c.svc.List(r.Context(), c.kind, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := RegistryListResponse{Items: make([]RegistryViewResponse, 0, len(views))}
	for _, view := range views {
		response.Items = append(response.Items, registryViewResponse(view))
	}
	if len(views) == limit {
		response.NextCursor = views[len(views)-1].Entry.ID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *registryKindController) get(w http.ResponseWriter, r *http.Request) {
	view, err := c.svc.Get(r.Context(), c.kind, chi.URLParam(r, "id"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, registryViewResponse(view))
}

func (c *registryKindController) create(w http.ResponseWriter, r *http.Request) {
	var input registrysvc.CreateInput
	if !decodeRegistryBody(w, r, &input) {
		return
	}
	view, err := c.svc.Create(r.Context(), registryHumanActor(), c.kind, input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, registryViewResponse(view))
}

func (c *registryKindController) update(w http.ResponseWriter, r *http.Request) {
	var input registrysvc.MetadataInput
	if !decodeRegistryBody(w, r, &input) {
		return
	}
	entry, err := c.svc.Update(r.Context(), registryHumanActor(), c.kind, chi.URLParam(r, "id"), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, registryEntryResponse(entry))
}

func (c *registryKindController) activate(w http.ResponseWriter, r *http.Request) {
	var input registrysvc.ActivateInput
	if !decodeRegistryBody(w, r, &input) {
		return
	}
	entry, err := c.svc.Activate(r.Context(), registryHumanActor(), c.kind, chi.URLParam(r, "id"), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, registryEntryResponse(entry))
}

func (c *registryKindController) clone(w http.ResponseWriter, r *http.Request) {
	var input registrysvc.CloneInput
	if !decodeRegistryBody(w, r, &input) {
		return
	}
	view, err := c.svc.Clone(r.Context(), registryHumanActor(), c.kind, chi.URLParam(r, "id"), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, registryViewResponse(view))
}

func (c *registryKindController) appendVersion(w http.ResponseWriter, r *http.Request) {
	var input registrysvc.VersionInput
	if !decodeRegistryBody(w, r, &input) {
		return
	}
	version, err := c.svc.Append(r.Context(), registryHumanActor(), c.kind, chi.URLParam(r, "id"), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, registryVersionResponse(version))
}

func (c *registryKindController) versions(w http.ResponseWriter, r *http.Request) {
	after, limit, err := registryHistoryPage(r)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	versions, err := c.svc.Versions(r.Context(), c.kind, chi.URLParam(r, "id"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := RegistryVersionsResponse{Versions: make([]RegistryVersionResponse, 0, len(versions))}
	for _, version := range versions {
		response.Versions = append(response.Versions, registryVersionResponse(version))
	}
	if len(versions) == limit {
		response.NextCursor = strconv.FormatInt(versions[len(versions)-1].Number, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *registryKindController) version(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.ParseInt(chi.URLParam(r, "version"), 10, 64)
	if err != nil || number < 1 {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_REGISTRY_VERSION", "Version must be a positive integer", nil))
		return
	}
	version, err := c.svc.Version(r.Context(), c.kind, chi.URLParam(r, "id"), number)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, registryVersionResponse(version))
}

func (c *registryKindController) audit(w http.ResponseWriter, r *http.Request) {
	after, limit, err := registryHistoryPage(r)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	events, err := c.svc.Audit(r.Context(), c.kind, chi.URLParam(r, "id"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := RegistryAuditListResponse{Events: make([]RegistryAuditResponse, 0, len(events))}
	for _, event := range events {
		response.Events = append(response.Events, RegistryAuditResponse{Sequence: event.Sequence, EntryID: event.EntryID,
			Revision: event.Revision, VersionNumber: event.VersionNumber, Action: event.Action, Origin: string(event.Actor.Origin), ActorID: event.Actor.ID, Reason: event.Reason, CreatedAt: event.CreatedAt})
	}
	if len(events) == limit {
		response.NextCursor = strconv.FormatInt(events[len(events)-1].Sequence, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func decodeRegistryBody(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_REGISTRY_JSON", "Expected a registry object with supported fields (maximum 2 MiB)", nil))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_REGISTRY_JSON", "Expected exactly one JSON object", nil))
		return false
	}
	return true
}

func registryLimit(r *http.Request) (int, error) {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return 100, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > 200 {
		return 0, apierr.Invalid("INVALID_REGISTRY_PAGE", "Limit must be between 1 and 200", nil)
	}
	return limit, nil
}

func registryHistoryPage(r *http.Request) (int64, int, error) {
	limit, err := registryLimit(r)
	if err != nil {
		return 0, 0, err
	}
	value := r.URL.Query().Get("cursor")
	if value == "" {
		return 0, limit, nil
	}
	after, err := strconv.ParseInt(value, 10, 64)
	if err != nil || after < 0 {
		return 0, 0, apierr.Invalid("INVALID_REGISTRY_CURSOR", "Cursor must be a non-negative integer", nil)
	}
	return after, limit, nil
}
