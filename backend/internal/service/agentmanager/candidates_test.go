package agentmanager

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type recordingCandidateAssessor struct {
	calls []string
	tasks []domain.TaskDefinition
	err   error
}

func (a *recordingCandidateAssessor) AssessManagerCandidate(_ context.Context, id string, version int64, task domain.TaskDefinition, project domain.ProjectRecord) (domain.AgentManagerCandidate, error) {
	a.calls = append(a.calls, fmt.Sprintf("%s/%d/%s", id, version, project.ID))
	a.tasks = append(a.tasks, task)
	return domain.AgentManagerCandidate{AgentType: domain.WorkerDefinitionRef{ID: id, Version: version}, Eligible: id != "manager-type"}, a.err
}

func TestManagerCandidatePagesPreserveExclusionsAndExactRequestPins(t *testing.T) {
	ctx := context.Background()
	_, _, s := inboxFixture(t, domain.ContextMission)
	request := inboxRequest(t, s, "project", "candidate-task", domain.ContextMission, "client-a")
	for i := 0; i < 21; i++ {
		if _, err := s.CreateRegistryEntry(ctx, fmt.Sprintf("worker-%02d", i), domain.RegistryAgentType, domain.RegistryMetadata{Name: "Worker", Enabled: true}, domain.RegistryDefinition{AgentType: &domain.AgentTypeDefinition{Harness: domain.HarnessCodex, MaxParallelWorkers: 1}}, domain.RegistryMutation{Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, Reason: "Candidate catalog"}); err != nil {
			t.Fatal(err)
		}
	}
	m := New(s)
	a := &recordingCandidateAssessor{}
	m.SetCandidateAssessor(a)
	page, err := m.Candidates(ctx, "project", request.ID, "", 20)
	if err != nil || len(page.Items) != 20 || page.Items[0].Eligible || page.NextCursor != "worker-18" || page.RequestHash != request.ContentHash || page.TaskClassification != domain.ContextMission || page.ObservedAt.IsZero() {
		t.Fatalf("first page: %+v %v", page, err)
	}
	last, err := m.Candidates(ctx, "project", request.ID, page.NextCursor, 20)
	if err != nil || len(last.Items) != 2 || last.NextCursor != "" || len(a.calls) != 22 {
		t.Fatalf("pagination dropped catalog: %+v %v", last, err)
	}
	for _, task := range a.tasks {
		if task.Classification != domain.ContextMission || task.EngagementID != "client-a" {
			t.Fatal("unpinned requirements")
		}
	}
	if _, err := m.Candidate(ctx, "project", request.ID, "worker-00", 7); err != nil || a.calls[len(a.calls)-1] != "worker-00/7/project" {
		t.Fatalf("active version substituted: %+v %v", a.calls, err)
	}
	before := len(a.calls)
	if _, err := m.Candidates(ctx, "other", request.ID, "", 20); err == nil || len(a.calls) != before {
		t.Fatal("cross-project candidate query escaped scope")
	}
	for _, limit := range []int{0, 21} {
		if _, err := m.Candidates(ctx, "project", request.ID, "", limit); err == nil {
			t.Fatal("unbounded candidate scan")
		}
	}
	if _, err := m.Candidates(ctx, "project", request.ID, "bad\ncursor", 20); err == nil {
		t.Fatal("control cursor accepted")
	}
	if _, err := m.Candidate(ctx, "project", request.ID, "worker-00", 0); err == nil {
		t.Fatal("unpinned candidate accepted")
	}
	a.err = errors.New("candidate storage unavailable")
	if page, err := m.Candidates(ctx, "project", request.ID, "", 20); !errors.Is(err, a.err) || len(page.Items) != 0 {
		t.Fatalf("partial results hid scan failure: %+v %v", page, err)
	}
	a.err = nil
	revision, err := s.GetTaskRevision(ctx, request.TaskID, 1)
	if err != nil {
		t.Fatal(err)
	}
	changed := revision.Definition
	changed.Classification = domain.ContextTechnical
	if _, err := s.ReviseAdaptiveTask(ctx, request.TaskID, changed, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Changed routing intent", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	before = len(a.calls)
	if _, err := m.Candidates(ctx, "project", request.ID, "", 20); err == nil || len(a.calls) != before {
		t.Fatal("stale requirements reached candidate assessment")
	}
}

func TestManagerCandidateAssessmentRequiresConfiguredBoundary(t *testing.T) {
	ctx := context.Background()
	_, _, s := inboxFixture(t, domain.ContextTechnical)
	request := inboxRequest(t, s, "project", "candidate-task", domain.ContextTechnical, "")
	if _, err := New(s).Candidates(ctx, "project", request.ID, "", 20); err == nil {
		t.Fatal("missing candidate boundary accepted")
	}
	m := New(s)
	a := &recordingCandidateAssessor{}
	m.SetCandidateAssessor(a)
	configuration, err := s.GetAgentManager(ctx, "project")
	if err != nil {
		t.Fatal(err)
	}
	configuration.Definition.Enabled = false
	if _, err := s.ConfigureAgentManager(ctx, "project", configuration.Definition, domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "USER", ID: "human"}, Reason: "Disable routing", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Candidate(ctx, "project", request.ID, "manager-type", 1); err == nil || len(a.calls) != 0 {
		t.Fatal("disabled governance reached native probes")
	}
}
