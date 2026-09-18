package controllers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	registrysvc "github.com/aoagents/agent-orchestrator/backend/internal/service/registry"
)

func (c *registryKindController) listBindings(w http.ResponseWriter, r *http.Request) {
	limit, err := registryLimit(r)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	bindings, err := c.svc.Bindings(r.Context(), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := ProviderBindingListResponse{Items: make([]ProviderBindingResponse, 0, len(bindings))}
	for _, binding := range bindings {
		response.Items = append(response.Items, providerBindingResponse(binding))
	}
	if len(bindings) == limit {
		response.NextCursor = bindings[len(bindings)-1].ID
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *registryKindController) createBinding(w http.ResponseWriter, r *http.Request) {
	var input registrysvc.BindingCreateInput
	if !decodeRegistryBody(w, r, &input) {
		return
	}
	binding, err := c.svc.CreateBinding(r.Context(), registryHumanActor(), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, providerBindingResponse(binding))
}

func (c *registryKindController) getBinding(w http.ResponseWriter, r *http.Request) {
	binding, err := c.svc.Binding(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, providerBindingResponse(binding))
}

func (c *registryKindController) updateBinding(w http.ResponseWriter, r *http.Request) {
	var input registrysvc.BindingUpdateInput
	if !decodeRegistryBody(w, r, &input) {
		return
	}
	binding, err := c.svc.UpdateBinding(r.Context(), registryHumanActor(), chi.URLParam(r, "id"), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, providerBindingResponse(binding))
}

func (c *registryKindController) bindingAudit(w http.ResponseWriter, r *http.Request) {
	after, limit, err := registryHistoryPage(r)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	events, err := c.svc.BindingAudit(r.Context(), chi.URLParam(r, "id"), after, limit)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response := ProviderBindingAuditListResponse{Events: make([]ProviderBindingAuditResponse, 0, len(events))}
	for _, event := range events {
		response.Events = append(response.Events, ProviderBindingAuditResponse{Sequence: event.Sequence, BindingID: event.BindingID, Revision: event.Revision, Action: event.Action, ActorID: event.ActorID, Reason: event.Reason, Name: event.Name, Enabled: event.Enabled, CreatedAt: event.CreatedAt})
	}
	if len(events) == limit {
		response.NextCursor = strconv.FormatInt(events[len(events)-1].Sequence, 10)
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *registryKindController) checkConfiguration(w http.ResponseWriter, r *http.Request) {
	var input registrysvc.CheckInput
	if !decodeRegistryBody(w, r, &input) {
		return
	}
	result, err := c.svc.Check(r.Context(), chi.URLParam(r, "id"), input)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}
