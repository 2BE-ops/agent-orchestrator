package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"

	moderncsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// isEvolutionDuplicate also accepts the primary-key constraint code: the
// experiments/recommendations tables are single-PK keyed, so a replayed
// creation surfaces SQLITE_CONSTRAINT_PRIMARYKEY instead of the
// secondary-index SQLITE_CONSTRAINT_UNIQUE the shared helper was written for.
func isEvolutionDuplicate(err error) bool {
	if isSQLiteUnique(err) {
		return true
	}
	var sqliteErr *moderncsqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}

var (
	_ ports.EvolutionExperimentStore     = (*Store)(nil)
	_ ports.EvolutionRecommendationStore = (*Store)(nil)
)

// CreateEvolutionExperiment seals one pinned comparison. Both versions must
// already exist as immutable versions of the same entry, so the cohort can
// never silently mix definitions. The creation snapshot is immutable; only the
// single conclusion transition may ever touch the row again.
func (s *Store) CreateEvolutionExperiment(ctx context.Context, experiment domain.EvolutionExperiment) (domain.EvolutionExperiment, error) {
	if experiment.Status != "running" || experiment.Conclusion != nil {
		return experiment, fmt.Errorf("%w: new experiments start running without a conclusion", ports.ErrEvolutionInvalid)
	}
	if err := experiment.Validate(); err != nil {
		return experiment, fmt.Errorf("%w: %w", ports.ErrEvolutionInvalid, err)
	}
	snapshot, hash, err := domain.TaskContent(experiment)
	if err != nil {
		return experiment, fmt.Errorf("%w: %w", ports.ErrEvolutionInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return experiment, err
	}
	defer s.writeMu.Unlock()
	err = s.inTx(ctx, "create evolution experiment", func(q *gen.Queries) error {
		if _, err := q.GetProject(ctx, experiment.ProjectID); err != nil {
			return evolutionReadError(err, "project")
		}
		for _, version := range []int64{experiment.ControlVersion, experiment.CandidateVersion} {
			row, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: experiment.EntryID, Number: version})
			if err != nil {
				return evolutionReadError(err, "registry version")
			}
			if row.Kind != string(experiment.Kind) {
				return fmt.Errorf("%w: version %d of %s is not a %s", ports.ErrEvolutionInvalid, version, experiment.EntryID, experiment.Kind)
			}
		}
		err := q.InsertEvolutionExperiment(ctx, gen.InsertEvolutionExperimentParams{
			ID: experiment.ID, ProjectID: string(experiment.ProjectID), Kind: string(experiment.Kind), EntryID: experiment.EntryID,
			ControlVersion: experiment.ControlVersion, CandidateVersion: experiment.CandidateVersion, Hypothesis: experiment.Hypothesis,
			MinimumSamples: experiment.MinimumSamples, Status: experiment.Status, Snapshot: string(snapshot), ContentHash: hash, CreatedAt: experiment.CreatedAt,
		})
		if isEvolutionDuplicate(err) {
			return fmt.Errorf("%w: experiment %s already exists", ports.ErrEvolutionStateConflict, experiment.ID)
		}
		return err
	})
	return experiment, err
}

// GetEvolutionExperiment reads one sealed experiment with any conclusion.
func (s *Store) GetEvolutionExperiment(ctx context.Context, projectID domain.ProjectID, experimentID string) (domain.EvolutionExperiment, error) {
	var experiment domain.EvolutionExperiment
	err := s.inTxLocked(ctx, "read evolution experiment", func(q *gen.Queries) error {
		row, err := q.GetEvolutionExperiment(ctx, gen.GetEvolutionExperimentParams{ProjectID: string(projectID), ID: experimentID})
		if err != nil {
			return evolutionReadError(err, "experiment")
		}
		experiment, err = evolutionExperimentFromRow(row)
		return err
	})
	return experiment, err
}

// ListEvolutionExperiments pages experiments by stable ID cursor.
//
//nolint:dupl // Experiments and recommendations are distinct durable resources; only the shared cursor walk below is common.
func (s *Store) ListEvolutionExperiments(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.EvolutionExperiment, error) {
	if err := evolutionPageArgs(afterID, limit); err != nil {
		return nil, err
	}
	return listEvolutionPage(ctx, s, "list evolution experiments", func(q *gen.Queries) ([]string, error) {
		return q.ListEvolutionExperimentIDs(ctx, gen.ListEvolutionExperimentIDsParams{ProjectID: string(projectID), AfterID: afterID, PageLimit: int64(limit)})
	}, func(q *gen.Queries, id string) (domain.EvolutionExperiment, error) {
		row, err := q.GetEvolutionExperiment(ctx, gen.GetEvolutionExperimentParams{ProjectID: string(projectID), ID: id})
		if err != nil {
			return domain.EvolutionExperiment{}, err
		}
		return evolutionExperimentFromRow(row)
	})
}

// evolutionPageArgs bounds the shared stable-ID cursor page contract.
func evolutionPageArgs(afterID string, limit int) error {
	if limit < 1 || limit > 100 || len(afterID) > 200 {
		return fmt.Errorf("%w: evolution page limit must be 1 to 100 with a bounded cursor", ports.ErrEvolutionPageInvalid)
	}
	return nil
}

// listEvolutionPage is the shared stable-ID cursor walk for both evolution
// histories: page the IDs, then read every row inside the same snapshot.
func listEvolutionPage[T any](ctx context.Context, s *Store, what string, ids func(*gen.Queries) ([]string, error), row func(*gen.Queries, string) (T, error)) ([]T, error) {
	var items []T
	err := s.inTxLocked(ctx, what, func(q *gen.Queries) error {
		page, err := ids(q)
		if err != nil {
			return err
		}
		items = make([]T, 0, len(page))
		for _, id := range page {
			item, err := row(q, id)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return nil
	})
	return items, err
}

// ConcludeEvolutionExperiment seals the one terminal decision. The evidence is
// recomputed inside the same transaction — never trusted from the caller — and
// a promote_candidate outcome is refused unless the promotion gates pass. The
// promotion path applies the entry's manager-versioning policy to the actor.
func (s *Store) ConcludeEvolutionExperiment(ctx context.Context, projectID domain.ProjectID, experimentID string, request domain.EvolutionConclusionRequest) (domain.EvolutionExperiment, error) {
	if err := request.Validate(); err != nil {
		return domain.EvolutionExperiment{}, fmt.Errorf("%w: %w", ports.ErrEvolutionInvalid, err)
	}
	var concluded domain.EvolutionExperiment
	if err := s.writeMu.LockContext(ctx); err != nil {
		return concluded, err
	}
	defer s.writeMu.Unlock()
	err := s.inTx(ctx, "conclude evolution experiment", func(q *gen.Queries) error {
		if _, err := q.GetProject(ctx, projectID); err != nil {
			return evolutionReadError(err, "project")
		}
		row, err := q.GetEvolutionExperiment(ctx, gen.GetEvolutionExperimentParams{ProjectID: string(projectID), ID: experimentID})
		if err != nil {
			return evolutionReadError(err, "experiment")
		}
		experiment, err := evolutionExperimentFromRow(row)
		if err != nil {
			return err
		}
		if experiment.Status != "running" {
			return fmt.Errorf("%w: experiment %s is already concluded", ports.ErrEvolutionStateConflict, experimentID)
		}
		now := time.Now().UTC()
		evidence, err := evolutionEvidence(ctx, q, experiment, request.From, request.To, now)
		if err != nil {
			return err
		}
		verdict := domain.EvaluateEvolutionEvidence(experiment, evidence)
		promotion := "none"
		if request.Outcome == "promote_candidate" {
			if !verdict.Eligible {
				return fmt.Errorf("%w: %s", ports.ErrEvolutionNotEligible, strings.Join(verdict.Findings, "; "))
			}
			promotion, err = evolutionPromotionPath(ctx, q, request.Actor, experiment)
			if err != nil {
				return err
			}
		}
		conclusion := domain.EvolutionConclusion{Outcome: request.Outcome, Promotion: promotion, Reason: request.Reason, Actor: request.Actor, DecidedAt: now, Evidence: evidence, Verdict: verdict}
		if err := conclusion.Validate(); err != nil {
			return fmt.Errorf("%w: %w", ports.ErrEvolutionInvalid, err)
		}
		encoded, err := json.Marshal(conclusion)
		if err != nil {
			return err
		}
		updated, err := q.ConcludeEvolutionExperiment(ctx, gen.ConcludeEvolutionExperimentParams{ProjectID: string(projectID), ID: experimentID, ConcludedAt: sql.NullTime{Time: now, Valid: true}, Conclusion: sql.NullString{String: string(encoded), Valid: true}})
		if err != nil {
			return err
		}
		if updated == 0 {
			return fmt.Errorf("%w: experiment %s is already concluded", ports.ErrEvolutionStateConflict, experimentID)
		}
		after, err := q.GetEvolutionExperiment(ctx, gen.GetEvolutionExperimentParams{ProjectID: string(projectID), ID: experimentID})
		if err != nil {
			return err
		}
		concluded, err = evolutionExperimentFromRow(after)
		return err
	})
	return concluded, err
}

// EvolutionExperimentEvidence derives both comparable cohorts from durable
// attempt facts in one locked snapshot. Nothing is stored or cached.
func (s *Store) EvolutionExperimentEvidence(ctx context.Context, query domain.EvolutionEvidenceQuery) (domain.EvolutionEvidence, error) {
	if err := query.Validate(); err != nil {
		return domain.EvolutionEvidence{}, fmt.Errorf("%w: %w", ports.ErrEvolutionInvalid, err)
	}
	var evidence domain.EvolutionEvidence
	err := s.inTxLocked(ctx, "derive evolution evidence", func(q *gen.Queries) error {
		if _, err := q.GetProject(ctx, query.ProjectID); err != nil {
			return evolutionReadError(err, "project")
		}
		row, err := q.GetEvolutionExperiment(ctx, gen.GetEvolutionExperimentParams{ProjectID: string(query.ProjectID), ID: query.ExperimentID})
		if err != nil {
			return evolutionReadError(err, "experiment")
		}
		experiment, err := evolutionExperimentFromRow(row)
		if err != nil {
			return err
		}
		evidence, err = evolutionEvidence(ctx, q, experiment, query.From, query.To, time.Now().UTC())
		return err
	})
	return evidence, err
}

// DiffEvolutionExperiment returns the complete field diff between the pinned
// control and candidate versions. The versions are immutable, so the diff is
// stable across restarts.
func (s *Store) DiffEvolutionExperiment(ctx context.Context, projectID domain.ProjectID, experimentID string) (domain.RegistryDefinitionDiff, error) {
	var diff domain.RegistryDefinitionDiff
	err := s.inTxLocked(ctx, "diff evolution versions", func(q *gen.Queries) error {
		row, err := q.GetEvolutionExperiment(ctx, gen.GetEvolutionExperimentParams{ProjectID: string(projectID), ID: experimentID})
		if err != nil {
			return evolutionReadError(err, "experiment")
		}
		experiment, err := evolutionExperimentFromRow(row)
		if err != nil {
			return err
		}
		from, to, err := evolutionRegistryVersions(ctx, q, experiment, experiment.ControlVersion, experiment.CandidateVersion)
		if err != nil {
			return err
		}
		diff, err = domain.DiffRegistryDefinitions(experiment.Kind, experiment.EntryID, experiment.ControlVersion, experiment.CandidateVersion, from.Definition, to.Definition)
		return err
	})
	return diff, err
}

func evolutionRegistryVersions(ctx context.Context, q *gen.Queries, experiment domain.EvolutionExperiment, fromNumber, toNumber int64) (domain.RegistryVersion, domain.RegistryVersion, error) {
	read := func(number int64) (domain.RegistryVersion, error) {
		row, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: experiment.EntryID, Number: number})
		if err != nil {
			return domain.RegistryVersion{}, evolutionReadError(err, "registry version")
		}
		if row.Kind != string(experiment.Kind) {
			return domain.RegistryVersion{}, fmt.Errorf("%w: version %d of %s is not a %s", ports.ErrEvolutionInvalid, number, experiment.EntryID, experiment.Kind)
		}
		return registryVersionFromGen(row)
	}
	from, err := read(fromNumber)
	if err != nil {
		return domain.RegistryVersion{}, domain.RegistryVersion{}, err
	}
	to, err := read(toNumber)
	if err != nil {
		return domain.RegistryVersion{}, domain.RegistryVersion{}, err
	}
	return from, to, nil
}

// evolutionPromotionPath applies the explicit policy: a manager may create the
// next version only when the entry allows manager versioning; otherwise the
// outcome must become a recommendation. Users always version through normal
// registry authoring, which has its own approval surfaces.
func evolutionPromotionPath(ctx context.Context, q *gen.Queries, actor domain.RegistryActor, experiment domain.EvolutionExperiment) (string, error) {
	if actor.Origin != domain.RegistryManager {
		return "version_permitted", nil
	}
	entry, err := q.GetRegistryEntry(ctx, experiment.EntryID)
	if err != nil {
		return "", evolutionReadError(err, "registry entry")
	}
	if entry.ManagerCanVersion == 0 {
		return "recommendation_required", nil
	}
	return "version_permitted", nil
}

func evolutionReadError(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s not found", ports.ErrEvolutionExperimentNotFound, what)
	}
	return err
}

func evolutionExperimentFromRow(row gen.AdaptiveExperiment) (domain.EvolutionExperiment, error) {
	experiment := domain.EvolutionExperiment{
		ID: row.ID, ProjectID: domain.ProjectID(row.ProjectID), Kind: domain.RegistryKind(row.Kind), EntryID: row.EntryID,
		ControlVersion: row.ControlVersion, CandidateVersion: row.CandidateVersion, Hypothesis: row.Hypothesis,
		MinimumSamples: row.MinimumSamples, Status: row.Status, CreatedAt: row.CreatedAt,
	}
	if row.Conclusion.Valid {
		var conclusion domain.EvolutionConclusion
		if err := json.Unmarshal([]byte(row.Conclusion.String), &conclusion); err != nil {
			return domain.EvolutionExperiment{}, fmt.Errorf("sealed conclusion is unreadable: %w", err)
		}
		experiment.Conclusion = &conclusion
	}
	if err := experiment.Validate(); err != nil {
		return domain.EvolutionExperiment{}, err
	}
	return experiment, nil
}

// evolutionEvidence walks the whole admission window in one snapshot and
// splits it into the experiment's two cohorts. Attempts attributed to other
// configurations count toward nothing; mixed and unseeded attempts are
// excluded with separate counters; tasks feeding both cohorts are counted as
// confounds. More than EvolutionCohortLimit attempts refuses the window.
func evolutionEvidence(ctx context.Context, q *gen.Queries, experiment domain.EvolutionExperiment, from, to, now time.Time) (domain.EvolutionEvidence, error) {
	evidence := domain.EvolutionEvidence{
		From: from, To: to, ObservedAt: now,
		Control:   domain.EvolutionCohort{Version: experiment.ControlVersion},
		Candidate: domain.EvolutionCohort{Version: experiment.CandidateVersion},
	}
	controlTasks, candidateTasks := map[string]struct{}{}, map[string]struct{}{}
	after, total := "", 0
	for {
		rows, err := q.ListTaskPerformanceAttempts(ctx, gen.ListTaskPerformanceAttemptsParams{
			ProjectID: string(experiment.ProjectID), WindowStart: from.UTC(), WindowEnd: to.UTC(), AfterID: after, PageLimit: 101,
		})
		if err != nil {
			return evidence, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows[:min(len(rows), 100)] {
			item, err := taskPerformanceAttempt(ctx, q, row, now)
			if err != nil {
				return evidence, err
			}
			total++
			if total > domain.EvolutionCohortLimit {
				return evidence, ports.ErrEvolutionCohortTooLarge
			}
			switch evolutionCohortSide(experiment, item, &evidence) {
			case "control":
				controlTasks[item.TaskID] = struct{}{}
				if err := addEvolutionMetrics(&evidence.Control, item); err != nil {
					return evidence, err
				}
			case "candidate":
				candidateTasks[item.TaskID] = struct{}{}
				if err := addEvolutionMetrics(&evidence.Candidate, item); err != nil {
					return evidence, err
				}
			}
		}
		if len(rows) <= 100 {
			break
		}
		after = rows[99].ID
	}
	for task := range controlTasks {
		if _, ok := candidateTasks[task]; ok {
			evidence.ConfoundedTasks++
		}
	}
	return evidence, nil
}

// evolutionCohortSide attributes one attempt to a cohort. Only attempts pinned
// to exactly the experiment's entry and one of its two versions compare;
// mixed-configuration and unseeded attempts are excluded, never guessed.
func evolutionCohortSide(experiment domain.EvolutionExperiment, item domain.TaskPerformanceAttempt, evidence *domain.EvolutionEvidence) string {
	if item.Configuration == nil {
		evidence.ExcludedUnseeded++
		return ""
	}
	if item.MixedConfigurations {
		evidence.ExcludedMixed++
		return ""
	}
	matches := func(ref domain.WorkerDefinitionRef) string {
		if ref.ID != experiment.EntryID {
			return ""
		}
		if ref.Version == experiment.ControlVersion {
			return "control"
		}
		if ref.Version == experiment.CandidateVersion {
			return "candidate"
		}
		return ""
	}
	if experiment.Kind == domain.RegistryAgentType {
		return matches(item.Configuration.AgentType)
	}
	for _, ref := range item.Configuration.Skills {
		if side := matches(ref); side != "" {
			return side
		}
	}
	return ""
}

func addEvolutionMetrics(cohort *domain.EvolutionCohort, item domain.TaskPerformanceAttempt) error {
	cohort.ComparableAttempts++
	metrics := &cohort.Metrics
	metrics.Attempts++
	switch item.AssessedOutcome {
	case "passed":
		metrics.AssessedPassed++
	case "failed":
		metrics.AssessedFailed++
	case "inconclusive":
		metrics.Inconclusive++
	case "unassessed":
		metrics.Unassessed++
	case "superseded":
		metrics.Superseded++
	default:
		return fmt.Errorf("unknown performance assessment outcome %q", item.AssessedOutcome)
	}
	if item.FirstPassCompleted {
		metrics.FirstPassCompleted++
	}
	metrics.ResultRevisions += max(0, item.ResultVersions-1)
	if item.AttemptNumber > 1 {
		metrics.RetryAttempts++
	}
	if item.ReservationOngoing {
		metrics.Ongoing++
	}
	return nil
}

// evolutionRecommendationSeal is the small immutable creation record. The
// proposed definition itself lives in its own bounded column with its own
// content hash so the seal never grows with the proposal.
type evolutionRecommendationSeal struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"projectId"`
	Kind         string    `json:"kind"`
	EntryID      string    `json:"entryId"`
	FromVersion  int64     `json:"fromVersion"`
	Observation  string    `json:"observation"`
	SampleSize   int64     `json:"sampleSize"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	ProposedHash string    `json:"proposedHash"`
}

// CreateEvolutionRecommendation persists one improvement proposal. The
// referenced version must already exist; the proposed definition is validated
// and sealed but never applied — adoption happens through registry authoring.
func (s *Store) CreateEvolutionRecommendation(ctx context.Context, recommendation domain.EvolutionRecommendation) (domain.EvolutionRecommendation, error) {
	if recommendation.Status != "pending" || recommendation.Decision != nil {
		return recommendation, fmt.Errorf("%w: new recommendations start pending without a decision", ports.ErrEvolutionInvalid)
	}
	if err := recommendation.Validate(); err != nil {
		return recommendation, fmt.Errorf("%w: %w", ports.ErrEvolutionInvalid, err)
	}
	proposed, proposedHash, err := domain.TaskContent(recommendation.Proposed)
	if err != nil {
		return recommendation, fmt.Errorf("%w: %w", ports.ErrEvolutionInvalid, err)
	}
	seal, hash, err := domain.TaskContent(evolutionRecommendationSeal{
		ID: recommendation.ID, ProjectID: string(recommendation.ProjectID), Kind: string(recommendation.Kind),
		EntryID: recommendation.EntryID, FromVersion: recommendation.FromVersion, Observation: recommendation.Observation,
		SampleSize: recommendation.SampleSize, Status: recommendation.Status, CreatedAt: recommendation.CreatedAt, ProposedHash: proposedHash,
	})
	if err != nil {
		return recommendation, fmt.Errorf("%w: %w", ports.ErrEvolutionInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return recommendation, err
	}
	defer s.writeMu.Unlock()
	err = s.inTx(ctx, "create evolution recommendation", func(q *gen.Queries) error {
		if _, err := q.GetProject(ctx, recommendation.ProjectID); err != nil {
			return recommendationReadError(err, "project")
		}
		version, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: recommendation.EntryID, Number: recommendation.FromVersion})
		if err != nil {
			return recommendationReadError(err, "registry version")
		}
		if version.Kind != string(recommendation.Kind) {
			return fmt.Errorf("%w: version %d of %s is not a %s", ports.ErrEvolutionInvalid, recommendation.FromVersion, recommendation.EntryID, recommendation.Kind)
		}
		err = q.InsertEvolutionRecommendation(ctx, gen.InsertEvolutionRecommendationParams{
			ID: recommendation.ID, ProjectID: string(recommendation.ProjectID), Kind: string(recommendation.Kind),
			EntryID: recommendation.EntryID, FromVersion: recommendation.FromVersion, Observation: recommendation.Observation,
			SampleSize: recommendation.SampleSize, Proposed: string(proposed), ProposedHash: proposedHash,
			Snapshot: string(seal), ContentHash: hash, CreatedAt: recommendation.CreatedAt,
		})
		if isEvolutionDuplicate(err) {
			return fmt.Errorf("%w: recommendation %s already exists", ports.ErrEvolutionStateConflict, recommendation.ID)
		}
		return err
	})
	return recommendation, err
}

// GetEvolutionRecommendation reads one sealed recommendation with any decision.
func (s *Store) GetEvolutionRecommendation(ctx context.Context, projectID domain.ProjectID, recommendationID string) (domain.EvolutionRecommendation, error) {
	var recommendation domain.EvolutionRecommendation
	err := s.inTxLocked(ctx, "read evolution recommendation", func(q *gen.Queries) error {
		row, err := q.GetEvolutionRecommendation(ctx, gen.GetEvolutionRecommendationParams{ProjectID: string(projectID), ID: recommendationID})
		if err != nil {
			return recommendationReadError(err, "recommendation")
		}
		recommendation, err = evolutionRecommendationFromRow(row)
		return err
	})
	return recommendation, err
}

// ListEvolutionRecommendations pages recommendations by stable ID cursor.
//
//nolint:dupl // Experiments and recommendations are distinct durable resources; only the shared cursor walk below is common.
func (s *Store) ListEvolutionRecommendations(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.EvolutionRecommendation, error) {
	if err := evolutionPageArgs(afterID, limit); err != nil {
		return nil, err
	}
	return listEvolutionPage(ctx, s, "list evolution recommendations", func(q *gen.Queries) ([]string, error) {
		return q.ListEvolutionRecommendationIDs(ctx, gen.ListEvolutionRecommendationIDsParams{ProjectID: string(projectID), AfterID: afterID, PageLimit: int64(limit)})
	}, func(q *gen.Queries, id string) (domain.EvolutionRecommendation, error) {
		row, err := q.GetEvolutionRecommendation(ctx, gen.GetEvolutionRecommendationParams{ProjectID: string(projectID), ID: id})
		if err != nil {
			return domain.EvolutionRecommendation{}, err
		}
		return evolutionRecommendationFromRow(row)
	})
}

// DecideEvolutionRecommendation seals the one-time disposition. Adoption is a
// recorded decision, not a mutation: versions are created only through normal
// registry authoring.
func (s *Store) DecideEvolutionRecommendation(ctx context.Context, projectID domain.ProjectID, recommendationID string, decision domain.EvolutionDecision) (domain.EvolutionRecommendation, error) {
	if err := decision.Validate(); err != nil {
		return domain.EvolutionRecommendation{}, fmt.Errorf("%w: %w", ports.ErrEvolutionInvalid, err)
	}
	var decided domain.EvolutionRecommendation
	if err := s.writeMu.LockContext(ctx); err != nil {
		return decided, err
	}
	defer s.writeMu.Unlock()
	err := s.inTx(ctx, "decide evolution recommendation", func(q *gen.Queries) error {
		if _, err := q.GetProject(ctx, projectID); err != nil {
			return recommendationReadError(err, "project")
		}
		row, err := q.GetEvolutionRecommendation(ctx, gen.GetEvolutionRecommendationParams{ProjectID: string(projectID), ID: recommendationID})
		if err != nil {
			return recommendationReadError(err, "recommendation")
		}
		recommendation, err := evolutionRecommendationFromRow(row)
		if err != nil {
			return err
		}
		if recommendation.Status != "pending" {
			return fmt.Errorf("%w: recommendation %s is already decided", ports.ErrEvolutionStateConflict, recommendationID)
		}
		encoded, err := json.Marshal(decision)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		updated, err := q.DecideEvolutionRecommendation(ctx, gen.DecideEvolutionRecommendationParams{
			Status: decision.Disposition, DecidedAt: sql.NullTime{Time: now, Valid: true}, Decision: sql.NullString{String: string(encoded), Valid: true}, ProjectID: string(projectID), ID: recommendationID,
		})
		if err != nil {
			return err
		}
		if updated == 0 {
			return fmt.Errorf("%w: recommendation %s is already decided", ports.ErrEvolutionStateConflict, recommendationID)
		}
		after, err := q.GetEvolutionRecommendation(ctx, gen.GetEvolutionRecommendationParams{ProjectID: string(projectID), ID: recommendationID})
		if err != nil {
			return err
		}
		decided, err = evolutionRecommendationFromRow(after)
		return err
	})
	return decided, err
}

// DiffEvolutionRecommendation returns the inspectable diff between the sealed
// from-version and the proposed definition. The proposed target is not yet a
// version, so its number is reported as zero.
func (s *Store) DiffEvolutionRecommendation(ctx context.Context, projectID domain.ProjectID, recommendationID string) (domain.RegistryDefinitionDiff, error) {
	var diff domain.RegistryDefinitionDiff
	err := s.inTxLocked(ctx, "diff evolution recommendation", func(q *gen.Queries) error {
		row, err := q.GetEvolutionRecommendation(ctx, gen.GetEvolutionRecommendationParams{ProjectID: string(projectID), ID: recommendationID})
		if err != nil {
			return recommendationReadError(err, "recommendation")
		}
		recommendation, err := evolutionRecommendationFromRow(row)
		if err != nil {
			return err
		}
		versionRow, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: recommendation.EntryID, Number: recommendation.FromVersion})
		if err != nil {
			return recommendationReadError(err, "registry version")
		}
		if versionRow.Kind != string(recommendation.Kind) {
			return fmt.Errorf("%w: version %d of %s is not a %s", ports.ErrEvolutionInvalid, recommendation.FromVersion, recommendation.EntryID, recommendation.Kind)
		}
		from, err := registryVersionFromGen(versionRow)
		if err != nil {
			return err
		}
		diff, err = domain.DiffRegistryDefinitions(recommendation.Kind, recommendation.EntryID, recommendation.FromVersion, 0, from.Definition, recommendation.Proposed)
		return err
	})
	return diff, err
}

func recommendationReadError(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s not found", ports.ErrEvolutionRecommendationNotFound, what)
	}
	return err
}

func evolutionRecommendationFromRow(row gen.AdaptiveRecommendation) (domain.EvolutionRecommendation, error) {
	recommendation := domain.EvolutionRecommendation{
		ID: row.ID, ProjectID: domain.ProjectID(row.ProjectID), Kind: domain.RegistryKind(row.Kind), EntryID: row.EntryID,
		FromVersion: row.FromVersion, Observation: row.Observation, SampleSize: row.SampleSize, Status: row.Status, CreatedAt: row.CreatedAt,
	}
	if err := json.Unmarshal([]byte(row.Proposed), &recommendation.Proposed); err != nil {
		return domain.EvolutionRecommendation{}, fmt.Errorf("sealed proposal is unreadable: %w", err)
	}
	if row.Decision.Valid {
		var decision domain.EvolutionDecision
		if err := json.Unmarshal([]byte(row.Decision.String), &decision); err != nil {
			return domain.EvolutionRecommendation{}, fmt.Errorf("sealed decision is unreadable: %w", err)
		}
		recommendation.Decision = &decision
	}
	if err := recommendation.Validate(); err != nil {
		return domain.EvolutionRecommendation{}, err
	}
	return recommendation, nil
}
