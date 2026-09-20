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
	knowledgesvc "github.com/aoagents/agent-orchestrator/backend/internal/service/knowledge"
)

// ProjectKnowledgeService is the shared authoring and provenance boundary.
type ProjectKnowledgeService interface {
	Create(context.Context, domain.AdaptiveActor, domain.ProjectID, knowledgesvc.CreateInput) (knowledgesvc.View, error)
	Get(context.Context, string) (knowledgesvc.View, error)
	List(context.Context, domain.KnowledgeFilter) ([]knowledgesvc.View, error)
	Revise(context.Context, domain.AdaptiveActor, string, knowledgesvc.RevisionInput) (domain.KnowledgeVersion, error)
	Version(context.Context, string, int64) (domain.KnowledgeVersion, error)
	Versions(context.Context, string, int64, int) ([]domain.KnowledgeVersion, error)
}

// ProjectKnowledgeController assigns human authority server-side.
type ProjectKnowledgeController struct{ Svc ProjectKnowledgeService }

// Register mounts bounded authoring, search and immutable history routes.
func (c *ProjectKnowledgeController) Register(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.Svc == nil {
					envelope.WriteError(w, r, apierr.NotImplemented("KNOWLEDGE_UNAVAILABLE", "Knowledge service is unavailable"))
					return
				}
				next.ServeHTTP(w, r)
			})
		})
		r.Get("/projects/{id}/knowledge", c.list)
		r.Post("/projects/{id}/knowledge", c.create)
		r.Get("/knowledge/{knowledgeId}", c.get)
		r.Get("/knowledge/{knowledgeId}/versions", c.versions)
		r.Post("/knowledge/{knowledgeId}/versions", c.revise)
		r.Get("/knowledge/{knowledgeId}/versions/{version}", c.version)
	})
}

func decodeKnowledgeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_KNOWLEDGE_JSON", "Expected supported knowledge fields (maximum 128 KiB)", nil))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_KNOWLEDGE_JSON", "Expected exactly one JSON object", nil))
		return false
	}
	return true
}

func (c *ProjectKnowledgeController) create(w http.ResponseWriter, r *http.Request) {
	var input KnowledgeCreateRequest
	if !decodeKnowledgeBody(w, r, &input) {
		return
	}
	view, err := c.Svc.Create(r.Context(), domain.AdaptiveActor{Kind: "USER", ID: "local-user"}, domain.ProjectID(chi.URLParam(r, "id")), knowledgesvc.CreateInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, KnowledgeResponse(view))
}

func (c *ProjectKnowledgeController) get(w http.ResponseWriter, r *http.Request) {
	view, err := c.Svc.Get(r.Context(), chi.URLParam(r, "knowledgeId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, KnowledgeResponse(view))
}

func (c *ProjectKnowledgeController) revise(w http.ResponseWriter, r *http.Request) {
	var input KnowledgeReviseRequest
	if !decodeKnowledgeBody(w, r, &input) {
		return
	}
	version, err := c.Svc.Revise(r.Context(), domain.AdaptiveActor{Kind: "USER", ID: "local-user"}, chi.URLParam(r, "knowledgeId"), knowledgesvc.RevisionInput(input))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, version)
}

func (c *ProjectKnowledgeController) list(w http.ResponseWriter, r *http.Request) {
	_, limit, err := knowledgePage(r, false)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	query := r.URL.Query()
	items, err := c.Svc.List(r.Context(), domain.KnowledgeFilter{ProjectID: domain.ProjectID(chi.URLParam(r, "id")), After: query.Get("cursor"), Limit: limit, Status: query.Get("status"), Kind: query.Get("kind"), Search: query.Get("search")})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := KnowledgeListResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = items[len(items)-1].Knowledge.ID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *ProjectKnowledgeController) version(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.ParseInt(chi.URLParam(r, "version"), 10, 64)
	if err != nil || number < 1 {
		envelope.WriteError(w, r, apierr.Invalid("INVALID_KNOWLEDGE_VERSION", "Version must be positive", nil))
		return
	}
	version, err := c.Svc.Version(r.Context(), chi.URLParam(r, "knowledgeId"), number)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, version)
}

func (c *ProjectKnowledgeController) versions(w http.ResponseWriter, r *http.Request) {
	after, limit, err := knowledgePage(r, true)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	items, err := c.Svc.Versions(r.Context(), chi.URLParam(r, "knowledgeId"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := KnowledgeVersionsResponse{Items: items}
	if len(items) == limit {
		response.NextCursor = strconv.FormatInt(items[len(items)-1].Number, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func knowledgePage(r *http.Request, history bool) (int64, int, error) {
	limit, after := 20, int64(0)
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err != nil || limit < 1 || limit > 100 {
		return 0, 0, apierr.Invalid("INVALID_KNOWLEDGE_PAGE", "Limit must be between 1 and 100", nil)
	}
	if raw := r.URL.Query().Get("cursor"); history && raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
	}
	if err != nil || after < 0 {
		return 0, 0, apierr.Invalid("INVALID_KNOWLEDGE_PAGE", "History cursor must be non-negative", nil)
	}
	return after, limit, nil
}
