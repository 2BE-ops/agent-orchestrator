package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/taskcontext"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func classifiedContextFixture(t *testing.T, s *sqlite.Store, definition domain.TaskDefinition, clearance domain.ContextClass) ports.TaskContextRequest {
	t.Helper()
	ctx := context.Background()
	seedProject(t, s, "project")
	createTask(t, s, "classified-task", definition)
	reservation := taskReservation("classified-attempt", "classified-task")
	reservation.Now, reservation.TTL = time.Now().UTC(), 5*time.Minute
	_, lease, err := s.ReserveTask(ctx, reservation)
	if err != nil {
		t.Fatal(err)
	}
	rec, config := workerSnapshot(t, s, clearance)
	rec.ProjectID = "project"
	rec, _, err = s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, config, lease.HeartbeatAt)
	if err != nil {
		t.Fatal(err)
	}
	op := domain.TaskExecutionOperation{ID: "classified-native", SessionID: rec.ID, Lease: lease.TaskLeaseToken, SourceOwner: rec.ControllerOwner(), Kind: "dispatch", CreatedAt: lease.HeartbeatAt}
	if _, err := s.BeginTaskExecution(ctx, op); err != nil {
		t.Fatal(err)
	}
	return ports.TaskContextRequest{Lease: lease.TaskLeaseToken, SessionID: rec.ID, ExecutionOperationID: op.ID, SystemPrompt: config.SystemPrompt, Prompt: "Complete the pinned work", WorkspacePath: t.TempDir()}
}

func classifiedKnowledge(t *testing.T, s *sqlite.Store, id string, class domain.ContextClass, engagement string) domain.KnowledgeVersion {
	t.Helper()
	d := knowledgeDefinition()
	d.Status = "accepted"
	d.Pinned = true
	d.Classification = class
	d.EngagementID = engagement
	d.Content = "SEALED_CONTENT_" + id
	if _, err := s.CreateProjectKnowledge(context.Background(), id, "project", d, knowledgeMutation(0)); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetKnowledgeVersion(context.Background(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestContextClassificationUsesPinnedClearanceAndFiltersBeforeLimit(t *testing.T) {
	ctx := context.Background()
	for _, clearance := range []domain.ContextClass{domain.ContextTechnical, domain.ContextEngagement, domain.ContextMission} {
		t.Run(string(clearance), func(t *testing.T) {
			s := newTestStore(t)
			d := taskDefinition()
			d.EngagementID = "client-a"
			request := classifiedContextFixture(t, s, d, clearance)
			for i := range 35 {
				classifiedKnowledge(t, s, fmt.Sprintf("a-foreign-%02d", i), domain.ContextEngagement, "client-b")
			}
			classifiedKnowledge(t, s, "z-technical", domain.ContextTechnical, "")
			classifiedKnowledge(t, s, "z-engagement", domain.ContextEngagement, "client-a")
			classifiedKnowledge(t, s, "z-mission", domain.ContextMission, "")
			// A live version with the opposite clearance must not change this attempt.
			definition := registryAgentDefinition()
			definition.AgentType.MaxContextClass = domain.ContextMission
			if clearance == domain.ContextMission {
				definition.AgentType.MaxContextClass = domain.ContextTechnical
			}
			if _, err := s.AppendRegistryVersion(ctx, "snapshot-type", definition, registryMutation(domain.RegistryUser, 1)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ActivateRegistryVersion(ctx, "snapshot-type", 2, registryMutation(domain.RegistryUser, 2)); err != nil {
				t.Fatal(err)
			}
			selected, err := s.SelectContextKnowledge(ctx, request.Lease.AttemptID, 33)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if clearance == domain.ContextEngagement {
				want = 2
			}
			if clearance == domain.ContextMission {
				want = 3
			}
			if len(selected) != want {
				t.Fatalf("classification/ranking: got %d want %d", len(selected), want)
			}
			snapshot, err := taskcontext.New(s).Build(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.MaxContextClass != clearance || snapshot.Classification != clearance || snapshot.SchemaVersion != 2 {
				t.Fatalf("wrong pinned classification: %+v", snapshot)
			}
			encoded, _ := json.Marshal(snapshot)
			if strings.Contains(string(encoded), "foreign") || strings.Contains(string(encoded), "client-b") {
				t.Fatal("prohibited identity or content leaked into manifest")
			}
			for _, source := range snapshot.Sources {
				if source.Classification == "" {
					t.Fatal("unlabelled manifest item")
				}
			}
			delegation, err := s.GetTaskDelegation(ctx, request.Lease.AttemptID, 1)
			if err != nil || delegation.ContextHash != snapshot.ContentHash || delegation.Prompt != snapshot.Prompt || delegation.SystemPrompt != request.SystemPrompt || delegation.MaxContextClass != clearance {
				t.Fatalf("wrong delegation: %+v %v", delegation, err)
			}
		})
	}
}

func TestContextClassificationRefusesTaskAboveClearance(t *testing.T) {
	ctx := context.Background()
	for _, class := range []domain.ContextClass{domain.ContextEngagement, domain.ContextMission} {
		t.Run(string(class), func(t *testing.T) {
			s := newTestStore(t)
			d := taskDefinition()
			d.Classification = class
			d.EngagementID = "client-a"
			request := classifiedContextFixture(t, s, d, domain.ContextTechnical)
			if _, err := taskcontext.New(s).Build(ctx, request); !errors.Is(err, ports.ErrTaskForbidden) {
				t.Fatalf("higher task admitted: %v", err)
			}
			if _, err := s.SelectContextKnowledge(ctx, request.Lease.AttemptID, 33); !errors.Is(err, ports.ErrTaskForbidden) {
				t.Fatalf("higher task selected knowledge: %v", err)
			}
			if _, found, err := s.GetTaskContext(ctx, request.Lease.AttemptID); err != nil || found {
				t.Fatalf("failed context persisted: %v %v", found, err)
			}
		})
	}
}

func TestContextClassificationRejectsForgedLabelsIncludingOmissionMetadata(t *testing.T) {
	ctx := context.Background()
	for _, disposition := range []string{"inline", "omitted"} {
		t.Run(disposition, func(t *testing.T) {
			s := newTestStore(t)
			snapshot, lease := taskContextFixture(t, s)
			v := classifiedKnowledge(t, s, "private", domain.ContextMission, "")
			source := inlineContext(t, "knowledge", v.KnowledgeID, v.Number, v.ContentHash, v.Definition)
			if disposition == "omitted" {
				source.Content, source.ContentHash, source.Disposition = "", "", "omitted"
			}
			// Schema v1 and a forged technical label cannot bypass the new store gate.
			snapshot.Sources = append(snapshot.Sources, source)
			sealContext(t, &snapshot)
			if err := s.SaveTaskContext(ctx, lease.TaskLeaseToken, snapshot); !errors.Is(err, ports.ErrTaskForbidden) {
				t.Fatalf("forged source sealed: %v", err)
			}
		})
	}
}

func TestClassifiedDelegationSealsAtomicallyAndRetainsHistory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	d := taskDefinition()
	d.Classification = domain.ContextEngagement
	d.EngagementID = "client-a"
	request := classifiedContextFixture(t, s, d, domain.ContextMission)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TRIGGER reject_classified_delegation BEFORE INSERT ON adaptive_task_delegations BEGIN SELECT RAISE(ABORT,'injected seal failure'); END`); err != nil {
		t.Fatal(err)
	}
	before, err := s.EventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := taskcontext.New(s).Build(ctx, request); err == nil {
		t.Fatal("delegation failure ignored")
	}
	if _, found, err := s.GetTaskContext(ctx, request.Lease.AttemptID); err != nil || found {
		t.Fatalf("partial manifest: %v %v", found, err)
	}
	after, err := s.EventsAfter(ctx, 0, 100)
	if err != nil || len(after) != len(before) {
		t.Fatalf("failed seal emitted CDC: %v", err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_classified_delegation`); err != nil {
		t.Fatal(err)
	}
	snapshot, err := taskcontext.New(s).Build(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	d.Classification, d.EngagementID = domain.ContextTechnical, ""
	if _, err := s.ReviseAdaptiveTask(ctx, "classified-task", d, taskMutation(1)); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	got, err := reopened.GetTaskDelegation(ctx, request.Lease.AttemptID, 1)
	if err != nil || got.Classification != domain.ContextEngagement || got.EngagementID != "client-a" || got.ContextHash != snapshot.ContentHash {
		t.Fatalf("delegation lost pin: %+v %v", got, err)
	}
	if _, err := taskcontext.New(reopened).Build(ctx, request); err != nil {
		t.Fatalf("historical replay failed: %v", err)
	}
	items, err := reopened.ListTaskDelegations(ctx, request.Lease.AttemptID, 0, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("history: %+v %v", items, err)
	}
	items, err = reopened.ListTaskDelegations(ctx, request.Lease.AttemptID, 1, 1)
	if err != nil || len(items) != 0 {
		t.Fatalf("cursor: %+v %v", items, err)
	}
	for _, statement := range []string{`UPDATE adaptive_task_delegations SET snapshot='{}'`, `DELETE FROM adaptive_task_delegations`, `INSERT INTO adaptive_task_delegations SELECT attempt_id,2,'other-native',session_id,configuration_hash,context_hash,snapshot,content_hash,created_at FROM adaptive_task_delegations`} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatal("immutable delegation mutation accepted")
		}
	}
}

func TestContextClassificationAllowsUpwardResultsAndRejectsDownwardOrForeignArtifacts(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name                                 string
		sourceClass, receiverClearance       domain.ContextClass
		sourceEngagement, receiverEngagement string
		allowed                              bool
	}{
		{"technical_findings_flow_up", domain.ContextTechnical, domain.ContextMission, "", "client-b", true},
		{"mission_findings_stay_classified", domain.ContextMission, domain.ContextTechnical, "", "", false},
		{"engagement_findings_stay_scoped", domain.ContextEngagement, domain.ContextMission, "client-a", "client-b", false},
		{"same_engagement_flows_up", domain.ContextEngagement, domain.ContextMission, "client-a", "client-a", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			d := taskDefinition()
			d.EngagementID = tc.sourceEngagement
			// The task itself is technical. Higher knowledge must taint its output;
			// looking only at the task label would leak these worker results.
			source := classifiedContextFixture(t, s, d, tc.sourceClass)
			classifiedKnowledge(t, s, "source-input", tc.sourceClass, tc.sourceEngagement)
			if _, err := taskcontext.New(s).Build(ctx, source); err != nil {
				t.Fatal(err)
			}
			rec, found, err := s.GetSession(ctx, source.SessionID)
			if err != nil || !found {
				t.Fatalf("source session: %v %v", found, err)
			}
			rec.Metadata.RuntimeLaunchID = source.ExecutionOperationID
			if err := s.UpdateSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			if err := s.ResolveTaskExecution(ctx, domain.TaskExecutionResolution{OperationID: source.ExecutionOperationID, ObservedOwner: rec.ControllerOwner(), Outcome: "connected", Reason: "Fixture native source connected", CreatedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			result, _, err := s.SubmitTaskResult(ctx, domain.TaskResultSubmission{ID: "source-result", AttemptID: source.Lease.AttemptID, SessionID: source.SessionID, SourceOwner: rec.ControllerOwner(), IdempotencyKey: "result", Definition: domain.TaskResultDefinition{SchemaVersion: 1, ClaimedOutcome: "partial", Summary: "RESULT_PRIVATE_MARKER", Findings: []string{"RESULT_PRIVATE_MARKER"}}})
			if err != nil {
				t.Fatal(err)
			}
			targetDefinition := taskDefinition()
			targetDefinition.EngagementID = tc.receiverEngagement
			targetDefinition.Dependencies = []string{"classified-task"}
			createTask(t, s, "receiver", targetDefinition)
			message, _, err := s.SubmitTaskMessage(ctx, domain.TaskMessageSubmission{ID: "source-contract", AttemptID: source.Lease.AttemptID, SessionID: source.SessionID, SourceOwner: rec.ControllerOwner(), IdempotencyKey: "contract", Definition: domain.TaskMessageDefinition{SchemaVersion: 1, Kind: "interface_contract", TargetTaskID: "receiver", Subject: "Interface", Body: "CONTRACT_PRIVATE_MARKER", CorrelationID: "contract", Interface: &domain.TaskInterfaceClaim{Name: "API", Contract: "CONTRACT_PRIVATE_MARKER"}}})
			if err != nil {
				t.Fatal(err)
			}
			config, found, err := s.GetWorkerConfiguration(ctx, source.SessionID)
			if err != nil || !found {
				t.Fatalf("configuration: %v %v", found, err)
			}
			config.Effective.MaxContextClass = tc.receiverClearance
			version, err := s.AppendRegistryVersion(ctx, "snapshot-type", domain.RegistryDefinition{AgentType: &config.Effective}, registryMutation(domain.RegistryUser, 1))
			if err != nil {
				t.Fatal(err)
			}
			config.AgentType.Version, config.AgentType.ContentHash, config.Selection.Version = version.Number, version.ContentHash, version.Number
			config.ContentHash = config.Hash()
			reservation := taskReservation("receiver-attempt", "receiver")
			reservation.Now, reservation.TTL = time.Now().UTC(), 5*time.Minute
			_, lease, err := s.ReserveTask(ctx, reservation)
			if err != nil {
				t.Fatal(err)
			}
			rec.ID = ""
			rec.Metadata.RuntimeLaunchID = ""
			rec, _, err = s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, config, lease.HeartbeatAt)
			if err != nil {
				t.Fatal(err)
			}
			op := domain.TaskExecutionOperation{ID: "receiver-native", SessionID: rec.ID, Lease: lease.TaskLeaseToken, SourceOwner: rec.ControllerOwner(), Kind: "dispatch", CreatedAt: lease.HeartbeatAt}
			if _, err := s.BeginTaskExecution(ctx, op); err != nil {
				t.Fatal(err)
			}
			snapshot, err := taskcontext.New(s).Build(ctx, ports.TaskContextRequest{Lease: lease.TaskLeaseToken, SessionID: rec.ID, ExecutionOperationID: op.ID, Prompt: "Use authorized historical evidence", SystemPrompt: config.SystemPrompt, WorkspacePath: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			for _, marker := range []string{"RESULT_PRIVATE_MARKER", "CONTRACT_PRIVATE_MARKER"} {
				if strings.Contains(snapshot.Prompt, marker) != tc.allowed {
					t.Fatalf("wrong result flow for %s", marker)
				}
			}
			for _, item := range snapshot.Sources {
				if item.ID == result.ID || item.ID == message.ID {
					if !tc.allowed || item.Classification != tc.sourceClass || item.EngagementID != tc.sourceEngagement {
						t.Fatalf("wrong artifact provenance: %+v", item)
					}
				}
			}
		})
	}
}

func TestContextClassificationPreventsWorkerKnowledgeLaundering(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	d := taskDefinition()
	d.EngagementID = "client-a"
	request := classifiedContextFixture(t, s, d, domain.ContextMission)
	classifiedKnowledge(t, s, "private-input", domain.ContextEngagement, "client-a")
	if _, err := taskcontext.New(s).Build(ctx, request); err != nil {
		t.Fatal(err)
	}
	claim := knowledgeDefinition()
	claim.Sources = []domain.KnowledgeSource{{Kind: "worker", Reference: "Worker finding", TaskID: "classified-task", AttemptID: request.Lease.AttemptID, SessionID: request.SessionID}}
	mutation := domain.KnowledgeMutation{Actor: domain.AdaptiveActor{Kind: "WORKER", ID: "worker", SessionID: request.SessionID}, Reason: "Propose reusable finding"}
	for _, tc := range []struct {
		class      domain.ContextClass
		engagement string
		allowed    bool
	}{
		{domain.ContextTechnical, "", false},
		{domain.ContextEngagement, "client-b", false},
		{domain.ContextMission, "", false},
		{domain.ContextEngagement, "client-a", true},
		{domain.ContextMission, "client-a", true},
	} {
		claim.Classification, claim.EngagementID = tc.class, tc.engagement
		_, err := s.CreateProjectKnowledge(ctx, string(tc.class)+"-"+tc.engagement, "project", claim, mutation)
		if tc.allowed && err != nil {
			t.Fatal(err)
		}
		if !tc.allowed && !errors.Is(err, ports.ErrKnowledgeForbidden) {
			t.Fatalf("knowledge reclassification: %v", err)
		}
	}
}
