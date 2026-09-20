package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestAgentManagerExecutionSurvivesRestartAndFencesUnknownNativeEffects(t *testing.T) {
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	ctx := context.Background()
	controller, seed := managerControllerSeed(t, s)
	op := domain.AgentManagerExecutionOperation{ID: "native-manager-launch", ControllerID: controller.ID, SessionID: seed.ID, SourceOwner: seed.ControllerOwner(), Kind: "dispatch", CreatedAt: time.Now().UTC()}
	created, err := s.BeginAgentManagerExecution(ctx, op)
	if err != nil || !created {
		t.Fatalf("reserve native effects: %v %v", created, err)
	}
	if created, err := s.BeginAgentManagerExecution(ctx, op); err != nil || created {
		t.Fatalf("replay relaunched native effects: %v %v", created, err)
	}
	other := op
	other.ID = "overlapping-launch"
	if _, err := s.BeginAgentManagerExecution(ctx, other); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("overlapping launch: %v", err)
	}
	resolution := domain.AgentManagerExecutionResolution{OperationID: op.ID, ObservedOwner: seed.ControllerOwner(), Outcome: "connected", Reason: "Connected controller", CreatedAt: op.CreatedAt}
	if err := s.ResolveAgentManagerExecution(ctx, resolution); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("wrong target generation connected: %v", err)
	}
	seed.Metadata.RuntimeLaunchID = op.ID
	mustNoError(t, s.UpdateSession(ctx, seed))
	resolution.ObservedOwner = seed.ControllerOwner()
	mustNoError(t, s.ResolveAgentManagerExecution(ctx, resolution))
	other.SourceOwner = seed.ControllerOwner()
	if _, err := s.BeginAgentManagerExecution(ctx, other); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("second dispatch after success: %v", err)
	}
	seed.IsTerminated = true
	mustNoError(t, s.UpdateSession(ctx, seed))
	restore := domain.AgentManagerExecutionOperation{ID: "native-manager-restore", ControllerID: controller.ID, SessionID: seed.ID, SourceOwner: seed.ControllerOwner(), Kind: "restore", CreatedAt: time.Now().UTC()}
	created, err = s.BeginAgentManagerExecution(ctx, restore)
	if err != nil || !created {
		t.Fatalf("restore reservation: %v %v", created, err)
	}
	reopened, err := sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	pending, found, err := reopened.PendingAgentManagerExecution(ctx, seed.ID)
	if err != nil || !found || pending.ID != restore.ID {
		t.Fatalf("pending lost: %+v %v %v", pending, found, err)
	}
	owner := seed.ControllerOwner()
	release := domain.AgentManagerControllerRelease{Token: controller.AgentManagerControllerToken, ObservedOwner: &owner, Reason: "Native termination confirmed", Now: time.Now().UTC().Add(time.Hour)}
	if err := reopened.ReleaseAgentManagerController(ctx, release); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("unresolved effects released ownership: %v", err)
	}
	resolution = domain.AgentManagerExecutionResolution{OperationID: restore.ID, ObservedOwner: owner, Outcome: "terminated", Reason: "Verified reserved native generation stopped", CreatedAt: release.Now}
	bad := resolution
	bad.ObservedOwner.RuntimeLaunchID = "stale-owner"
	if err := reopened.ResolveAgentManagerExecution(ctx, bad); !errors.Is(err, ports.ErrAgentManagerFenced) {
		t.Fatalf("stale owner resolved: %v", err)
	}
	mustNoError(t, reopened.ResolveAgentManagerExecution(ctx, resolution))
	audit, err := reopened.ListAgentManagerAudit(ctx, "project", 0, 100)
	mustNoError(t, err)
	mustNoError(t, reopened.ResolveAgentManagerExecution(ctx, resolution))
	after, err := reopened.ListAgentManagerAudit(ctx, "project", 0, 100)
	if err != nil || len(after) != len(audit) {
		t.Fatalf("duplicate resolution audit: %v", err)
	}
	mustNoError(t, reopened.ReleaseAgentManagerController(ctx, release))
	seed.IsTerminated = false
	if err := reopened.UpdateSession(ctx, seed); err == nil {
		t.Fatal("released Manager resurrected")
	}
	if _, found, err := reopened.PendingAgentManagerExecution(ctx, seed.ID); err != nil || found {
		t.Fatalf("resolution lost: %v %v", found, err)
	}
}

func TestAgentManagerRestoreAndReleaseCannotBothWin(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	controller, seed := managerControllerSeed(t, s)
	seed.IsTerminated = true
	mustNoError(t, s.UpdateSession(ctx, seed))
	owner := seed.ControllerOwner()
	now := time.Now().UTC()
	op := domain.AgentManagerExecutionOperation{ID: "restore", ControllerID: controller.ID, SessionID: seed.ID, SourceOwner: owner, Kind: "restore", CreatedAt: now}
	release := domain.AgentManagerControllerRelease{Token: controller.AgentManagerControllerToken, ObservedOwner: &owner, Reason: "Confirmed stopped before replacement", Now: now}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Go(func() { _, err := s.BeginAgentManagerExecution(ctx, op); results <- err })
	wg.Go(func() { results <- s.ReleaseAgentManagerController(ctx, release) })
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ports.ErrAgentManagerFenced) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("restore and replacement both admitted: %d", winners)
	}
}

func TestAgentManagerDatabaseGuardsNativeHistoryAndRelease(t *testing.T) {
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	ctx := context.Background()
	controller, seed := managerControllerSeed(t, s)
	seed.IsTerminated = true
	mustNoError(t, s.UpdateSession(ctx, seed))
	seed.IsTerminated = false
	if err := s.UpdateSession(ctx, seed); err == nil {
		t.Fatal("unreserved restoration")
	}
	seed.IsTerminated = true
	op := domain.AgentManagerExecutionOperation{ID: "restore", ControllerID: controller.ID, SessionID: seed.ID, SourceOwner: seed.ControllerOwner(), Kind: "restore", CreatedAt: time.Now().UTC()}
	_, err := s.BeginAgentManagerExecution(ctx, op)
	mustNoError(t, err)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{`UPDATE adaptive_agent_manager_controllers SET released_at=CURRENT_TIMESTAMP,release_reason='bypass'`, `UPDATE adaptive_agent_manager_execution_operations SET kind='dispatch'`, `DELETE FROM adaptive_agent_manager_execution_operations`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("native history bypass: %s", statement)
		}
	}
	resolution := domain.AgentManagerExecutionResolution{OperationID: op.ID, ObservedOwner: seed.ControllerOwner(), Outcome: "terminated", Reason: "Native teardown confirmed", CreatedAt: time.Now().UTC()}
	mustNoError(t, s.ResolveAgentManagerExecution(ctx, resolution))
	for _, statement := range []string{`UPDATE adaptive_agent_manager_execution_resolutions SET outcome='connected'`, `DELETE FROM adaptive_agent_manager_execution_resolutions`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("mutable resolution: %s", statement)
		}
	}
}
