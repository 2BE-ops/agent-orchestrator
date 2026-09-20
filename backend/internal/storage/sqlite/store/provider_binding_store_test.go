package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestProviderBindingOwnershipRevisionAndAudit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	binding := domain.ProviderBinding{ID: "native", Name: "Native subscription", Harness: domain.HarnessCodex, Enabled: true}
	if _, err := s.CreateProviderBinding(ctx, binding, registryMutation(domain.RegistryManager, 0)); !errors.Is(err, ports.ErrRegistryForbidden) {
		t.Fatalf("manager created binding: %v", err)
	}
	created, err := s.CreateProviderBinding(ctx, binding, registryMutation(domain.RegistryUser, 0))
	if err != nil || created.Revision != 1 {
		t.Fatalf("create: %+v %v", created, err)
	}
	if _, err := s.UpdateProviderBinding(ctx, binding.ID, "Hijack", false, registryMutation(domain.RegistryManager, 1)); !errors.Is(err, ports.ErrRegistryForbidden) {
		t.Fatalf("manager changed binding: %v", err)
	}
	updated, err := s.UpdateProviderBinding(ctx, binding.ID, "Renamed", false, registryMutation(domain.RegistryUser, 1))
	if err != nil || updated.Revision != 2 || updated.Enabled || updated.Harness != binding.Harness {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if _, err := s.UpdateProviderBinding(ctx, binding.ID, "Stale", true, registryMutation(domain.RegistryUser, 1)); !errors.Is(err, ports.ErrRegistryConflict) {
		t.Fatalf("stale write: %v", err)
	}
	events, err := s.ListProviderBindingAudit(ctx, binding.ID, 0, 100)
	if err != nil || len(events) != 2 || events[1].Name != "Renamed" || events[1].Enabled {
		t.Fatalf("audit: %+v %v", events, err)
	}
	cdc, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(cdc) != 2 {
		t.Fatalf("CDC: %+v %v", cdc, err)
	}
	listed, err := s.ListProviderBindings(ctx, "", 1)
	if err != nil || len(listed) != 1 || listed[0].Enabled {
		t.Fatalf("disabled reference not inspectable: %+v %v", listed, err)
	}
	if _, err := s.GetProviderBinding(ctx, "missing"); !errors.Is(err, ports.ErrRegistryNotFound) {
		t.Fatalf("missing reference: %v", err)
	}
}
