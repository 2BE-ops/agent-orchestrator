package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func evolutionDefinition(instructions string) domain.RegistryDefinition {
	definition := registryAgentDefinition()
	definition.AgentType.SessionMode = domain.SessionModeTUI
	definition.AgentType.Config.Permissions = domain.PermissionModeAuto
	// Tests keep several attributed attempts concurrent, past the shared
	// fixture default of three workers per type.
	definition.AgentType.MaxParallelWorkers = 50
	definition.AgentType.Instructions = instructions
	return definition
}

// evolutionEntry seeds an agent-type entry with two versions so experiments
// have exact immutable versions to pin.
func evolutionEntry(t *testing.T, s *sqlite.Store, id string, policy domain.RegistryPolicy) {
	t.Helper()
	metadata := registryMetadata("Evolution candidate")
	metadata.Policy = policy
	if _, err := s.CreateRegistryEntry(context.Background(), id, domain.RegistryAgentType, metadata, evolutionDefinition("Check work carefully"), registryMutation(domain.RegistryUser, 0)); err != nil {
		t.Fatal(err)
	}
	second := evolutionDefinition("Check work and component tests")
	if _, err := s.AppendRegistryVersion(context.Background(), id, second, registryMutation(domain.RegistryUser, 1)); err != nil {
		t.Fatal(err)
	}
}

// evolutionWorker reserves one attempt on a fresh task and seeds a worker
// session whose pinned configuration attributes that attempt to the exact
// entry version, the way real dispatch attribution does.
func evolutionWorker(t *testing.T, s *sqlite.Store, taskID, attemptID, entryID string, version int64) {
	t.Helper()
	ctx := context.Background()
	createTask(t, s, taskID, taskDefinition())
	_, lease := reserveTask(t, s, attemptID, taskID)
	entry, err := s.GetRegistryEntry(ctx, entryID)
	if err != nil {
		t.Fatal(err)
	}
	versionRow, err := s.GetRegistryVersion(ctx, entryID, version)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := domain.WorkerConfiguration{
		SchemaVersion: 1,
		AgentType:     domain.WorkerDefinitionRef{ID: entryID, Version: version, Name: entry.Metadata.Name, ContentHash: versionRow.ContentHash},
		Selection:     domain.WorkerSelection{AgentTypeID: entryID},
		Effective:     *versionRow.Definition.AgentType, Origin: domain.RegistryUser, ActorID: "human",
		SystemPrompt: "Fixed project instructions", CreatedAt: time.Now().UTC(),
	}
	snapshot.ContentHash = snapshot.Hash()
	rec := sampleRecord("project")
	rec.Harness = snapshot.Effective.Harness
	rec.Mode = snapshot.Effective.SessionMode
	rec.Metadata = domain.SessionMetadata{Permissions: snapshot.Effective.Config.Permissions}
	if _, _, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.HeartbeatAt); err != nil {
		t.Fatal(err)
	}
}

func evolutionExperimentFixture(projectID, entryID string, minimumSamples int64) domain.EvolutionExperiment {
	return domain.EvolutionExperiment{
		ID: "experiment-1", ProjectID: domain.ProjectID(projectID), Kind: domain.RegistryAgentType, EntryID: entryID,
		ControlVersion: 1, CandidateVersion: 2, Hypothesis: "Candidate reduces review revisions",
		MinimumSamples: minimumSamples, Status: "running", CreatedAt: time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC),
	}
}

// evolutionWindow covers the shared reservation fixture's pinned admission
// time (2026-09-18 12:00 UTC) rather than the wall clock.
func evolutionWindow() (time.Time, time.Time) {
	return time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
}

func TestEvolutionExperimentSealsComparisonsAndRefusesBrokenPins(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "project")
	evolutionEntry(t, s, "evolution-type", domain.RegistryPolicy{ManagerCanSelect: true})

	created, err := s.CreateEvolutionExperiment(ctx, evolutionExperimentFixture("project", "evolution-type", 2))
	if err != nil || created.Status != "running" || created.Conclusion != nil {
		t.Fatalf("create: %+v %v", created, err)
	}
	if _, err := s.CreateEvolutionExperiment(ctx, evolutionExperimentFixture("project", "evolution-type", 2)); !errors.Is(err, ports.ErrEvolutionStateConflict) {
		t.Fatalf("duplicate id: %v", err)
	}
	sameVersions := evolutionExperimentFixture("project", "evolution-type", 2)
	sameVersions.ID, sameVersions.CandidateVersion = "experiment-2", 1
	if _, err := s.CreateEvolutionExperiment(ctx, sameVersions); !errors.Is(err, ports.ErrEvolutionInvalid) {
		t.Fatalf("identical versions: %v", err)
	}
	missingVersion := evolutionExperimentFixture("project", "evolution-type", 2)
	missingVersion.ID, missingVersion.CandidateVersion = "experiment-3", 9
	if _, err := s.CreateEvolutionExperiment(ctx, missingVersion); !errors.Is(err, ports.ErrEvolutionExperimentNotFound) {
		t.Fatalf("unknown version: %v", err)
	}
	foreignProject := evolutionExperimentFixture("other", "evolution-type", 2)
	if _, err := s.CreateEvolutionExperiment(ctx, foreignProject); !errors.Is(err, ports.ErrEvolutionExperimentNotFound) {
		t.Fatalf("unknown project: %v", err)
	}
	skillKind := evolutionExperimentFixture("project", "evolution-type", 2)
	skillKind.ID, skillKind.Kind = "experiment-4", domain.RegistrySkill
	if _, err := s.CreateEvolutionExperiment(ctx, skillKind); !errors.Is(err, ports.ErrEvolutionInvalid) {
		t.Fatalf("kind mismatch: %v", err)
	}

	read, err := s.GetEvolutionExperiment(ctx, "project", "experiment-1")
	if err != nil || read.Hypothesis != created.Hypothesis {
		t.Fatalf("read: %+v %v", read, err)
	}
	if _, err := s.GetEvolutionExperiment(ctx, "project", "missing"); !errors.Is(err, ports.ErrEvolutionExperimentNotFound) {
		t.Fatalf("missing experiment: %v", err)
	}
	second := evolutionExperimentFixture("project", "evolution-type", 2)
	second.ID = "experiment-0"
	if _, err := s.CreateEvolutionExperiment(ctx, second); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListEvolutionExperiments(ctx, "project", "", 1)
	if err != nil || len(page) != 1 || page[0].ID != "experiment-0" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	page, err = s.ListEvolutionExperiments(ctx, "project", page[0].ID, 100)
	if err != nil || len(page) != 1 || page[0].ID != "experiment-1" {
		t.Fatalf("second page: %+v %v", page, err)
	}
	if _, err := s.ListEvolutionExperiments(ctx, "project", "", 0); !errors.Is(err, ports.ErrEvolutionPageInvalid) {
		t.Fatalf("unbounded page: %v", err)
	}

	diff, err := s.DiffEvolutionExperiment(ctx, "project", "experiment-1")
	if err != nil || len(diff.Fields) == 0 || diff.FromNumber != 1 || diff.ToNumber != 2 {
		t.Fatalf("experiment diff: %+v %v", diff, err)
	}
	changed := false
	for _, field := range diff.Fields {
		if field.Path == "instructions" && field.Change == "changed" {
			changed = true
		}
	}
	if !changed {
		t.Fatalf("instructions change missing: %+v", diff.Fields)
	}
	if _, err := s.DiffEvolutionExperiment(ctx, "project", "missing"); !errors.Is(err, ports.ErrEvolutionExperimentNotFound) {
		t.Fatalf("missing diff target: %v", err)
	}
}

func TestEvolutionEvidenceSplitsCohortsAndRefusesConfounds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "project")
	evolutionEntry(t, s, "evolution-type", domain.RegistryPolicy{})
	// Two comparable attempts per side, plus one task fed by both versions.
	evolutionWorker(t, s, "control-a", "c-attempt-a", "evolution-type", 1)
	evolutionWorker(t, s, "control-b", "c-attempt-b", "evolution-type", 1)
	evolutionWorker(t, s, "candidate-a", "d-attempt-a", "evolution-type", 2)
	evolutionWorker(t, s, "candidate-b", "d-attempt-b", "evolution-type", 2)
	// One task fed by both versions becomes a confound.
	createTask(t, s, "shared", taskDefinition())
	sharedEntry, err := s.GetRegistryEntry(ctx, "evolution-type")
	if err != nil {
		t.Fatal(err)
	}
	for i, version := range []int64{1, 2} {
		versionRow, err := s.GetRegistryVersion(ctx, "evolution-type", version)
		if err != nil {
			t.Fatal(err)
		}
		_, lease := reserveTask(t, s, "shared-attempt-"+string(rune('a'+i)), "shared")
		snapshot := domain.WorkerConfiguration{
			SchemaVersion: 1,
			AgentType:     domain.WorkerDefinitionRef{ID: "evolution-type", Version: version, Name: sharedEntry.Metadata.Name, ContentHash: versionRow.ContentHash},
			Selection:     domain.WorkerSelection{AgentTypeID: "evolution-type"},
			Effective:     *versionRow.Definition.AgentType, Origin: domain.RegistryUser, ActorID: "human",
			SystemPrompt: "Fixed project instructions", CreatedAt: time.Now().UTC(),
		}
		snapshot.ContentHash = snapshot.Hash()
		rec := sampleRecord("project")
		rec.Harness, rec.Mode = snapshot.Effective.Harness, snapshot.Effective.SessionMode
		rec.Metadata = domain.SessionMetadata{Permissions: snapshot.Effective.Config.Permissions}
		seed, _, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, snapshot, lease.HeartbeatAt)
		if err != nil {
			t.Fatal(err)
		}
		// The task is exclusive: release the finished attempt before the other
		// version can reserve it. Attribution survives the release.
		seed.IsTerminated = true
		if err := s.UpdateSession(ctx, seed); err != nil {
			t.Fatal(err)
		}
		owner := seed.ControllerOwner()
		release := domain.TaskLeaseRecovery{Token: lease.TaskLeaseToken, SessionID: seed.ID, ObservedOwner: &owner, Reason: "Shared task attempt finished", Now: lease.ExpiresAt.Add(time.Hour)}
		if err := s.ReleaseTaskLease(ctx, release); err != nil {
			t.Fatal(err)
		}
	}

	experiment, err := s.CreateEvolutionExperiment(ctx, evolutionExperimentFixture("project", "evolution-type", 2))
	if err != nil {
		t.Fatal(err)
	}
	from, to := evolutionWindow()
	evidence, err := s.EvolutionExperimentEvidence(ctx, domain.EvolutionEvidenceQuery{ProjectID: "project", ExperimentID: experiment.ID, From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Control.Version != 1 || evidence.Candidate.Version != 2 || evidence.Control.ComparableAttempts != 3 || evidence.Candidate.ComparableAttempts != 3 {
		t.Fatalf("cohort split: %+v", evidence)
	}
	if evidence.Control.Metrics.Attempts != 3 || evidence.Candidate.Metrics.Attempts != 3 || evidence.Control.Metrics.Ongoing != 2 || evidence.Candidate.Metrics.Ongoing != 2 {
		t.Fatalf("cohort metrics: %+v", evidence)
	}
	if evidence.ConfoundedTasks != 1 {
		t.Fatalf("shared task must be a confound: %+v", evidence)
	}
	verdict := domain.EvaluateEvolutionEvidence(experiment, evidence)
	if verdict.Eligible {
		t.Fatalf("confounded evidence must not be eligible: %+v", verdict)
	}
	// A window that excludes every attempt reports empty cohorts honestly.
	empty := domain.EvolutionEvidenceQuery{ProjectID: "project", ExperimentID: experiment.ID, From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)}
	evidence, err = s.EvolutionExperimentEvidence(ctx, empty)
	if err != nil || evidence.Control.ComparableAttempts != 0 || evidence.Candidate.ComparableAttempts != 0 || evidence.ObservedAt.IsZero() {
		t.Fatalf("empty window: %+v %v", evidence, err)
	}
	inverted := domain.EvolutionEvidenceQuery{ProjectID: "project", ExperimentID: experiment.ID, From: to, To: from}
	if _, err := s.EvolutionExperimentEvidence(ctx, inverted); !errors.Is(err, ports.ErrEvolutionInvalid) {
		t.Fatalf("inverted window: %v", err)
	}
	if _, err := s.EvolutionExperimentEvidence(ctx, domain.EvolutionEvidenceQuery{ProjectID: "project", ExperimentID: "missing", From: from, To: to}); !errors.Is(err, ports.ErrEvolutionExperimentNotFound) {
		t.Fatalf("missing experiment evidence: %v", err)
	}
}

func TestEvolutionConclusionEnforcesGatesAndPolicyPath(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	evolutionEntry(t, s, "permitted", domain.RegistryPolicy{ManagerCanVersion: true})
	evolutionEntry(t, s, "restricted", domain.RegistryPolicy{ManagerCanVersion: false})
	for _, entry := range []string{"permitted", "restricted"} {
		evolutionWorker(t, s, entry+"-a", entry+"-attempt-a", entry, 1)
		evolutionWorker(t, s, entry+"-b", entry+"-attempt-b", entry, 1)
		evolutionWorker(t, s, entry+"-c", entry+"-attempt-c", entry, 2)
		evolutionWorker(t, s, entry+"-d", entry+"-attempt-d", entry, 2)
	}
	from, to := evolutionWindow()

	permitted, err := s.CreateEvolutionExperiment(ctx, evolutionExperimentFixture("project", "permitted", 5))
	if err != nil {
		t.Fatal(err)
	}
	early := domain.EvolutionConclusionRequest{Outcome: "promote_candidate", Reason: "Too early", Actor: domain.RegistryActor{Origin: domain.RegistryManager, ID: "manager"}, From: from, To: to}
	if _, err := s.ConcludeEvolutionExperiment(ctx, "project", permitted.ID, early); !errors.Is(err, ports.ErrEvolutionNotEligible) {
		t.Fatalf("promotion without evidence must be refused: %v", err)
	}
	keeper := domain.EvolutionConclusionRequest{Outcome: "insufficient_evidence", Reason: "Not enough comparable work yet", Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, From: from, To: to}
	if _, err := s.ConcludeEvolutionExperiment(ctx, "project", permitted.ID, keeper); err != nil {
		t.Fatal(err)
	}
	sealed, err := s.GetEvolutionExperiment(ctx, "project", permitted.ID)
	if err != nil || sealed.Status != "concluded" || sealed.Conclusion == nil || sealed.Conclusion.Outcome != "insufficient_evidence" || sealed.Conclusion.Promotion != "none" {
		t.Fatalf("sealed keep: %+v %v", sealed, err)
	}
	again := domain.EvolutionConclusionRequest{Outcome: "keep_control", Reason: "Already terminal", Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, From: from, To: to}
	if _, err := s.ConcludeEvolutionExperiment(ctx, "project", permitted.ID, again); !errors.Is(err, ports.ErrEvolutionStateConflict) {
		t.Fatalf("second conclusion: %v", err)
	}

	promote := domain.EvolutionConclusionRequest{Outcome: "promote_candidate", Reason: "Both cohorts passed the minimum", Actor: domain.RegistryActor{Origin: domain.RegistryManager, ID: "manager"}, From: from, To: to}
	permittedFresh, err := s.CreateEvolutionExperiment(ctx, func() domain.EvolutionExperiment {
		experiment := evolutionExperimentFixture("project", "permitted", 2)
		experiment.ID = "experiment-permitted"
		return experiment
	}())
	if err != nil {
		t.Fatal(err)
	}
	concluded, err := s.ConcludeEvolutionExperiment(ctx, "project", permittedFresh.ID, promote)
	if err != nil || concluded.Conclusion == nil || concluded.Conclusion.Promotion != "version_permitted" {
		t.Fatalf("manager promotion under permissive policy: %+v %v", concluded, err)
	}
	restricted, err := s.CreateEvolutionExperiment(ctx, func() domain.EvolutionExperiment {
		experiment := evolutionExperimentFixture("project", "restricted", 2)
		experiment.ID = "experiment-restricted"
		return experiment
	}())
	if err != nil {
		t.Fatal(err)
	}
	concluded, err = s.ConcludeEvolutionExperiment(ctx, "project", restricted.ID, promote)
	if err != nil || concluded.Conclusion == nil || concluded.Conclusion.Promotion != "recommendation_required" {
		t.Fatalf("manager promotion under restrictive policy must require a recommendation: %+v %v", concluded, err)
	}
	verdict := concluded.Conclusion.Verdict
	if !verdict.Eligible || len(verdict.Findings) != 0 || concluded.Conclusion.Evidence.Control.ComparableAttempts != 2 {
		t.Fatalf("sealed evidence: %+v", concluded.Conclusion)
	}
	badOutcome := domain.EvolutionConclusionRequest{Outcome: "rollback", Reason: "Unknown", Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, From: from, To: to}
	if _, err := s.ConcludeEvolutionExperiment(ctx, "project", "experiment-permitted", badOutcome); !errors.Is(err, ports.ErrEvolutionInvalid) {
		t.Fatalf("unknown outcome: %v", err)
	}
	missing := domain.EvolutionConclusionRequest{Outcome: "keep_control", Reason: "Nothing to conclude", Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, From: from, To: to}
	if _, err := s.ConcludeEvolutionExperiment(ctx, "project", "missing", missing); !errors.Is(err, ports.ErrEvolutionExperimentNotFound) {
		t.Fatalf("missing experiment conclusion: %v", err)
	}
}

func TestEvolutionRecommendationSealsProposalAndOneTimeDecision(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	seedProject(t, s, "project")
	evolutionEntry(t, s, "evolution-type", domain.RegistryPolicy{})
	recommendation := domain.EvolutionRecommendation{
		ID: "recommendation-1", ProjectID: "project", Kind: domain.RegistryAgentType, EntryID: "evolution-type",
		FromVersion: 1, Observation: "Three of four recent tasks needed a revision", SampleSize: 4,
		Status: "pending", CreatedAt: time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC),
		Proposed: evolutionDefinition("Check work, component tests and accessibility"),
	}
	created, err := s.CreateEvolutionRecommendation(ctx, recommendation)
	if err != nil || created.Status != "pending" || created.Decision != nil {
		t.Fatalf("create: %+v %v", created, err)
	}
	if _, err := s.CreateEvolutionRecommendation(ctx, recommendation); !errors.Is(err, ports.ErrEvolutionStateConflict) {
		t.Fatalf("duplicate id: %v", err)
	}
	zeroSample := recommendation
	zeroSample.ID, zeroSample.SampleSize = "recommendation-2", 0
	if _, err := s.CreateEvolutionRecommendation(ctx, zeroSample); !errors.Is(err, ports.ErrEvolutionInvalid) {
		t.Fatalf("zero sample: %v", err)
	}
	missingVersion := recommendation
	missingVersion.ID, missingVersion.FromVersion = "recommendation-3", 9
	if _, err := s.CreateEvolutionRecommendation(ctx, missingVersion); !errors.Is(err, ports.ErrEvolutionRecommendationNotFound) {
		t.Fatalf("unknown version: %v", err)
	}

	diff, err := s.DiffEvolutionRecommendation(ctx, "project", "recommendation-1")
	if err != nil || diff.ToNumber != 0 || diff.FromNumber != 1 || len(diff.Fields) == 0 {
		t.Fatalf("recommendation diff: %+v %v", diff, err)
	}
	changed := false
	for _, field := range diff.Fields {
		if field.Path == "instructions" && field.Change == "changed" && field.To != "" {
			changed = true
		}
	}
	if !changed {
		t.Fatalf("proposed instructions missing from diff: %+v", diff.Fields)
	}

	decision := domain.EvolutionDecision{Disposition: "adopted", Reason: "Create the version through registry authoring", Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, DecidedAt: time.Now().UTC()}
	decided, err := s.DecideEvolutionRecommendation(ctx, "project", "recommendation-1", decision)
	if err != nil || decided.Status != "adopted" || decided.Decision == nil || decided.Decision.Reason != decision.Reason {
		t.Fatalf("decide: %+v %v", decided, err)
	}
	if _, err := s.DecideEvolutionRecommendation(ctx, "project", "recommendation-1", decision); !errors.Is(err, ports.ErrEvolutionStateConflict) {
		t.Fatalf("second decision: %v", err)
	}
	badDecision := domain.EvolutionDecision{Disposition: "deferred", Reason: "Unknown", Actor: domain.RegistryActor{Origin: domain.RegistryUser, ID: "human"}, DecidedAt: time.Now().UTC()}
	if _, err := s.DecideEvolutionRecommendation(ctx, "project", "recommendation-1", badDecision); !errors.Is(err, ports.ErrEvolutionInvalid) {
		t.Fatalf("unknown disposition: %v", err)
	}

	page, err := s.ListEvolutionRecommendations(ctx, "project", "", 100)
	if err != nil || len(page) != 1 || page[0].ID != "recommendation-1" {
		t.Fatalf("list: %+v %v", page, err)
	}
	if _, err := s.ListEvolutionRecommendations(ctx, "project", "", 101); !errors.Is(err, ports.ErrEvolutionPageInvalid) {
		t.Fatalf("unbounded page: %v", err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	reread, err := reopened.GetEvolutionRecommendation(ctx, "project", "recommendation-1")
	if err != nil || reread.Status != "adopted" || reread.Decision == nil || reread.Observation != recommendation.Observation {
		t.Fatalf("restart stability: %+v %v", reread, err)
	}
	if diff, err := reopened.DiffEvolutionRecommendation(ctx, "project", "recommendation-1"); err != nil || len(diff.Fields) == 0 {
		t.Fatalf("restart diff: %+v %v", diff, err)
	}
}
