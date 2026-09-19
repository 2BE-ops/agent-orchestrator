package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func agentManagerDefinition(t *testing.T, s *sqlite.Store) domain.AgentManagerDefinition {
	t.Helper()
	seedProject(t, s, "project")
	_, snapshot := workerSnapshot(t, s)
	return domain.AgentManagerDefinition{SchemaVersion: 1, Enabled: true, AgentTypeID: snapshot.AgentType.ID, AgentTypeVersion: snapshot.AgentType.Version, Policy: domain.DefaultAgentManagerPolicy()}
}

func TestAgentManagerGovernanceRetainsPinsPolicyAuditAndRestart(t *testing.T) {
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	ctx := context.Background()
	definition := agentManagerDefinition(t, s)
	first, err := s.ConfigureAgentManager(ctx, "project", definition, taskMutation(0))
	mustNoError(t, err)
	if first.Number != 1 || first.ContentHash != first.Hash() || first.ControllerType.Version != 1 || first.Actor.Kind != "USER" {
		t.Fatalf("unsealed governance: %+v", first)
	}
	definition.Policy.Optimization = "quality"
	definition.Policy.AllowCreateSkills = true
	second, err := s.ConfigureAgentManager(ctx, "project", definition, taskMutation(1))
	mustNoError(t, err)
	if second.Number != 2 || second.ContentHash == first.ContentHash {
		t.Fatalf("configuration not versioned: %+v", second)
	}
	if _, err := s.ConfigureAgentManager(ctx, "project", definition, taskMutation(1)); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("stale policy edit: %v", err)
	}
	metadata := registryMetadata("Renamed unavailable controller")
	metadata.Enabled = false
	_, err = s.UpdateRegistryMetadata(ctx, definition.AgentTypeID, metadata, registryMutation(domain.RegistryUser, 1))
	mustNoError(t, err)
	if _, err := s.ConfigureAgentManager(ctx, "project", definition, taskMutation(2)); !errors.Is(err, ports.ErrAgentManagerInvalid) {
		t.Fatalf("enabled missing controller accepted: %v", err)
	}
	definition.Enabled = false
	third, err := s.ConfigureAgentManager(ctx, "project", definition, taskMutation(2))
	mustNoError(t, err)
	if third.Number != 3 || third.Definition.Enabled {
		t.Fatalf("could not disable manager after Type disabled: %+v", third)
	}
	reopened, err := sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	retained, err := reopened.GetAgentManagerConfiguration(ctx, "project", 1)
	if err != nil || !reflect.DeepEqual(retained, first) {
		t.Fatalf("live registry changed pinned governance: %+v %v", retained, err)
	}
	current, err := reopened.GetAgentManager(ctx, "project")
	if err != nil || !reflect.DeepEqual(current, third) {
		t.Fatalf("current desired policy lost: %+v %v", current, err)
	}
	page, err := reopened.ListAgentManagerConfigurations(ctx, "project", 1, 1)
	if err != nil || len(page) != 1 || page[0].Number != 2 {
		t.Fatalf("configuration page: %+v %v", page, err)
	}
	audit, err := reopened.ListAgentManagerAudit(ctx, "project", 0, 10)
	if err != nil || len(audit) != 3 || audit[0].Action != "configured" || audit[1].Action != "reconfigured" || audit[2].ConfigurationVersion != 3 || audit[0].Actor != first.Actor {
		t.Fatalf("governance audit: %+v %v", audit, err)
	}
	events, err := reopened.EventsAfter(ctx, 0, 100)
	mustNoError(t, err)
	count := 0
	for _, event := range events {
		if event.Type == cdc.EventAgentManagerChanged {
			count++
			if event.ProjectID != "project" || event.SessionID != "" {
				t.Fatalf("governance CDC scope: %+v", event)
			}
		}
	}
	if count != 3 {
		t.Fatalf("governance CDC count: %d", count)
	}
	sessions, err := reopened.ListAllSessions(ctx)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("configuring governance launched native state: %+v %v", sessions, err)
	}
}

func TestAgentManagerGovernanceRejectsSelfEscalationAndBadReferences(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	definition := agentManagerDefinition(t, s)
	for _, actor := range []domain.AdaptiveActor{{Kind: "AGENT_MANAGER", ID: "manager"}, {Kind: "WORKER", ID: "worker"}, {Kind: "ORCHESTRATOR", ID: "planner", SessionID: "planner"}, {Kind: "SYSTEM", ID: "daemon"}, {Kind: "USER", ID: "human", SessionID: "worker"}} {
		mutation := taskMutation(0)
		mutation.Actor = actor
		if _, err := s.ConfigureAgentManager(ctx, "project", definition, mutation); !errors.Is(err, ports.ErrAgentManagerForbidden) {
			t.Fatalf("policy escalation: %+v %v", actor, err)
		}
	}
	if _, err := s.GetAgentManager(ctx, "project"); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("denied actor created manager: %v", err)
	}
	if _, err := s.ConfigureAgentManager(ctx, "missing", definition, taskMutation(0)); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("missing project: %v", err)
	}
	bad := definition
	bad.AgentTypeVersion = 999
	if _, err := s.ConfigureAgentManager(ctx, "project", bad, taskMutation(0)); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("missing version: %v", err)
	}
	_, err := s.CreateRegistryEntry(ctx, "not-type", domain.RegistrySkill, registryMetadata("Skill"), domain.RegistryDefinition{Skill: &domain.SkillDefinition{Instructions: "Read the contract"}}, registryMutation(domain.RegistryUser, 0))
	mustNoError(t, err)
	bad = definition
	bad.AgentTypeID = "not-type"
	if _, err := s.ConfigureAgentManager(ctx, "project", bad, taskMutation(0)); !errors.Is(err, ports.ErrAgentManagerInvalid) {
		t.Fatalf("Skill became manager controller: %v", err)
	}
	bad = definition
	bad.Policy.MaxPendingRequests = 1001
	if _, err := s.ConfigureAgentManager(ctx, "project", bad, taskMutation(0)); !errors.Is(err, ports.ErrAgentManagerInvalid) {
		t.Fatalf("unbounded inbox: %v", err)
	}
	_, err = s.ConfigureAgentManager(ctx, "project", definition, taskMutation(0))
	mustNoError(t, err)
	if _, err := s.ListAgentManagerConfigurations(ctx, "project", 0, 101); !errors.Is(err, ports.ErrAgentManagerInvalid) {
		t.Fatalf("unbounded history: %v", err)
	}
	if _, err := s.ListAgentManagerAudit(ctx, "project", -1, 20); !errors.Is(err, ports.ErrAgentManagerInvalid) {
		t.Fatalf("negative cursor: %v", err)
	}
	if _, err := s.GetAgentManagerConfiguration(ctx, "other", 1); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("cross-project configuration: %v", err)
	}
}

func TestAgentManagerGovernanceCASAndAtomicAuditRollback(t *testing.T) {
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	ctx := context.Background()
	definition := agentManagerDefinition(t, s)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, err := s.ConfigureAgentManager(ctx, "project", definition, taskMutation(0)); results <- err })
	}
	wg.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, ports.ErrAgentManagerConflict) {
			t.Fatal(err)
		}
	}
	if created != 1 {
		t.Fatalf("concurrent configuration writers: %d", created)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TRIGGER fail_manager_audit BEFORE INSERT ON adaptive_agent_manager_audit BEGIN SELECT RAISE(ABORT,'injected audit failure'); END;`)
	mustNoError(t, err)
	events, err := s.EventsAfter(ctx, 0, 100)
	mustNoError(t, err)
	definition.Policy.AllowCreateTypes = true
	if got, err := s.ConfigureAgentManager(ctx, "project", definition, taskMutation(1)); err == nil || got.Number != 0 {
		t.Fatalf("audit failure committed policy: %+v %v", got, err)
	}
	current, err := s.GetAgentManager(ctx, "project")
	if err != nil || current.Number != 1 || current.Definition.Policy.AllowCreateTypes {
		t.Fatalf("partial policy escaped: %+v %v", current, err)
	}
	after, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(after) != len(events) {
		t.Fatalf("rollback emitted CDC: %d -> %d %v", len(events), len(after), err)
	}
	for _, statement := range []string{`UPDATE adaptive_agent_manager_configurations SET snapshot='{}'`, `DELETE FROM adaptive_agent_manager_configurations`, `UPDATE adaptive_agent_manager_audit SET reason='edited'`, `DELETE FROM adaptive_agent_manager_audit`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("mutable governance history: %s", statement)
		}
	}
}
