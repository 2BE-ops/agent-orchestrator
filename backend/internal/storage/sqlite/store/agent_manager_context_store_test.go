package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func managerContextRequest(t *testing.T, s *sqlite.Store, native domain.AgentManagerProposalSubmission, id string, class domain.ContextClass, engagement string) domain.AgentManagerContextSeal {
	t.Helper()
	d := taskDefinition()
	d.Classification, d.EngagementID = class, engagement
	d.Brief = "Exact routing input for " + id
	createTask(t, s, id, d)
	_, _, err := s.EnqueueAgentManagerRequest(context.Background(), domain.AgentManagerEnqueue{ID: id + "-request", ProjectID: "project", TaskID: id, TaskRevision: 1, ConfigurationVersion: 1, Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Unclassified caller reason MUST NOT enter native context", Now: time.Now().UTC()})
	mustNoError(t, err)
	return domain.AgentManagerContextSeal{ID: id + "-context", ProjectID: "project", RequestID: id + "-request", SessionID: native.SessionID, SourceOwner: native.SourceOwner, Now: time.Now().UTC()}
}

func TestAgentManagerContextPinnedClearanceLattice(t *testing.T) {
	ctx := context.Background()
	for _, clearance := range []domain.ContextClass{domain.ContextTechnical, domain.ContextEngagement, domain.ContextMission} {
		for _, class := range []domain.ContextClass{domain.ContextTechnical, domain.ContextEngagement, domain.ContextMission} {
			t.Run(string(clearance)+"/"+string(class), func(t *testing.T) {
				s := newTestStore(t)
				native := managerProposalFixture(t, s, domain.SessionModeTUI, clearance)
				input := managerContextRequest(t, s, native, "classified", class, "client-a")
				definition := registryAgentDefinition()
				definition.AgentType.MaxContextClass = domain.ContextMission
				if clearance == domain.ContextMission {
					definition.AgentType.MaxContextClass = domain.ContextTechnical
				}
				_, err := s.AppendRegistryVersion(ctx, "snapshot-type", definition, registryMutation(domain.RegistryUser, 1))
				mustNoError(t, err)
				_, err = s.ActivateRegistryVersion(ctx, "snapshot-type", 2, registryMutation(domain.RegistryUser, 2))
				mustNoError(t, err)
				sealed, created, err := s.SealAgentManagerContext(ctx, input)
				if !clearance.Allows(class) {
					if !errors.Is(err, ports.ErrAgentManagerForbidden) || created || sealed.ID != "" {
						t.Fatalf("higher-class input escaped: %+v %v %v", sealed, created, err)
					}
					items, err := s.ListAgentManagerContexts(ctx, "project", input.RequestID)
					if err != nil || len(items) != 0 {
						t.Fatalf("refusal retained input: %+v %v", items, err)
					}
					return
				}
				if err != nil || !created || sealed.MaxContextClass != clearance || sealed.Classification != class || sealed.NativeGeneration != native.SourceOwner.RuntimeLaunchID || len(sealed.Sources) != 2 {
					t.Fatalf("wrong pinned receipt: %+v %v %v", sealed, created, err)
				}
				if !strings.Contains(sealed.Prompt, "Exact routing input for classified") || strings.Contains(sealed.Prompt, "MUST NOT") {
					t.Fatal("prompt lost exact task or embedded unclassified enqueue reason")
				}
			})
		}
	}
}

func TestAgentManagerContextConversationScopeAndSensitivitySurviveGenerations(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []domain.SessionMode{domain.SessionModeTUI, domain.SessionModeChat} {
		t.Run(string(mode), func(t *testing.T) {
			s := newTestStore(t)
			native := managerProposalFixture(t, s, mode, domain.ContextMission)
			firstInput := managerContextRequest(t, s, native, "first", domain.ContextMission, "")
			first, _, err := s.SealAgentManagerContext(ctx, firstInput)
			mustNoError(t, err)
			secondInput := managerContextRequest(t, s, native, "second", domain.ContextEngagement, "client-a")
			second, _, err := s.SealAgentManagerContext(ctx, secondInput)
			if err != nil || second.PreviousContextHash != first.ContentHash || second.Classification != domain.ContextMission || second.EngagementID != "client-a" {
				t.Fatalf("conversation lost sensitivity: %+v %v", second, err)
			}
			// A connected replacement generation inherits the same conversation boundary.
			op := domain.AgentManagerExecutionOperation{ID: "replacement", ControllerID: "controller", SessionID: native.SessionID, SourceOwner: native.SourceOwner, Kind: "restore", CreatedAt: time.Now().UTC()}
			_, err = s.BeginAgentManagerExecution(ctx, op)
			mustNoError(t, err)
			rec, _, err := s.GetSession(ctx, native.SessionID)
			mustNoError(t, err)
			if mode == domain.SessionModeChat {
				rec.Metadata.ControllerGeneration = op.ID
			} else {
				rec.Metadata.RuntimeLaunchID = op.ID
			}
			mustNoError(t, s.UpdateSession(ctx, rec))
			mustNoError(t, s.ResolveAgentManagerExecution(ctx, domain.AgentManagerExecutionResolution{OperationID: op.ID, ObservedOwner: rec.ControllerOwner(), Outcome: "connected", Reason: "Confirmed replacement", CreatedAt: time.Now().UTC()}))
			native.SourceOwner = rec.ControllerOwner()
			for _, class := range []domain.ContextClass{domain.ContextTechnical, domain.ContextEngagement, domain.ContextMission} {
				foreign := managerContextRequest(t, s, native, "foreign-"+string(class), class, "client-b")
				if _, _, err := s.SealAgentManagerContext(ctx, foreign); !errors.Is(err, ports.ErrAgentManagerForbidden) {
					t.Fatalf("cross-engagement conversation accepted: %v", err)
				}
			}
			technical := managerContextRequest(t, s, native, "technical", domain.ContextTechnical, "")
			third, _, err := s.SealAgentManagerContext(ctx, technical)
			if err != nil || third.Classification != domain.ContextMission || third.EngagementID != "client-a" || third.PreviousContextHash != second.ContentHash || third.NativeGeneration != "replacement" || third.Sources[0].Classification != domain.ContextTechnical {
				t.Fatalf("legal upward input lost cumulative provenance: %+v %v", third, err)
			}
		})
	}
}

func TestAgentManagerContextRaceRestartBoundsAndExactReplay(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	native := managerProposalFixture(t, s, domain.SessionModeTUI, domain.ContextEngagement)
	input := managerContextRequest(t, s, native, "bounded", domain.ContextEngagement, "client-a")
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for range 8 {
		wg.Go(func() {
			_, created, err := s.SealAgentManagerContext(ctx, input)
			if err != nil {
				t.Error(err)
			}
			results <- created
		})
	}
	wg.Wait()
	close(results)
	winners := 0
	for created := range results {
		if created {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("sealed %d duplicates", winners)
	}
	first, err := s.GetAgentManagerContext(ctx, "project", input.RequestID, input.ID)
	mustNoError(t, err)
	for i := 2; i <= 32; i++ {
		next := input
		next.ID, next.Now = fmt.Sprintf("bounded-%d", i), time.Now().UTC()
		sealed, created, err := s.SealAgentManagerContext(ctx, next)
		if err != nil || !created || sealed.Number != int64(i) {
			t.Fatalf("version %d: %+v %v %v", i, sealed, created, err)
		}
	}
	extra := input
	extra.ID, extra.Now = "too-many", time.Now().UTC()
	if _, _, err := s.SealAgentManagerContext(ctx, extra); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("unbounded context generation: %v", err)
	}
	mustNoError(t, s.Close())
	s, err = sqlite.Open(dir)
	mustNoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	_, err = s.ReviseAdaptiveTask(ctx, "bounded", taskDefinition(), taskMutation(1))
	mustNoError(t, err)
	configuration, err := s.GetAgentManager(ctx, "project")
	mustNoError(t, err)
	configuration.Definition.Enabled = false
	_, err = s.ConfigureAgentManager(ctx, "project", configuration.Definition, taskMutation(1))
	mustNoError(t, err)
	replayed, created, err := s.SealAgentManagerContext(ctx, input)
	if err != nil || created || replayed.ContentHash != first.ContentHash || replayed.Prompt != first.Prompt {
		t.Fatalf("retry rebuilt sealed content: %+v %v %v", replayed, created, err)
	}
	if _, err := s.GetAgentManagerContext(ctx, "other-project", input.RequestID, input.ID); !errors.Is(err, ports.ErrAgentManagerNotFound) {
		t.Fatalf("cross-project receipt read: %v", err)
	}
	input.SourceOwner.RuntimeLaunchID = "different-owner"
	if _, _, err := s.SealAgentManagerContext(ctx, input); !errors.Is(err, ports.ErrAgentManagerConflict) {
		t.Fatalf("changed replay owner: %v", err)
	}
}

func TestAgentManagerContextAuditRollbackAndSQLProtection(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	native := managerProposalFixture(t, s, domain.SessionModeTUI, domain.ContextMission)
	input := managerContextRequest(t, s, native, "atomic", domain.ContextEngagement, "client-a")
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TRIGGER fail_manager_context_audit BEFORE INSERT ON adaptive_agent_manager_audit WHEN NEW.action='context_sealed' BEGIN SELECT RAISE(ABORT,'injected audit failure'); END`)
	mustNoError(t, err)
	var before, after int
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&before))
	if _, _, err := s.SealAgentManagerContext(ctx, input); err == nil {
		t.Fatal("audit failure ignored")
	}
	items, err := s.ListAgentManagerContexts(ctx, "project", input.RequestID)
	if err != nil || len(items) != 0 {
		t.Fatalf("partial seal after rollback: %+v %v", items, err)
	}
	mustNoError(t, db.QueryRow(`SELECT count(*) FROM change_log`).Scan(&after))
	if before != after {
		t.Fatal("rollback emitted CDC")
	}
	_, err = db.Exec(`DROP TRIGGER fail_manager_context_audit`)
	mustNoError(t, err)
	_, _, err = s.SealAgentManagerContext(ctx, input)
	mustNoError(t, err)
	for _, statement := range []string{`UPDATE adaptive_agent_manager_contexts SET engagement_id='client-b'`, `DELETE FROM adaptive_agent_manager_contexts`, `INSERT INTO adaptive_agent_manager_contexts(id,request_id,number,controller_id,session_id,native_generation,source_owner,classification,engagement_id,previous_context_hash,snapshot,content_hash,created_at) SELECT 'forged',request_id,2,controller_id,session_id,native_generation,source_owner,'technical','',content_hash,snapshot,content_hash,created_at FROM adaptive_agent_manager_contexts`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatalf("SQL stripped retained classification: %s", statement)
		}
	}
}
