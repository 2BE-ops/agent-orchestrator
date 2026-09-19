package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.TaskReviewStore = (*Store)(nil)

// PrepareTaskReview reads the exact result/configuration/criteria before native
// configuration resolution. No filesystem or provider I/O occurs in this write.
func (s *Store) PrepareTaskReview(ctx context.Context, resultID string, actor domain.AdaptiveActor) (domain.TaskReviewPreparation, error) {
	if err := validateTaskMutation(domain.TaskMutation{Actor: actor, Reason: "Prepare independent review"}); err != nil {
		return domain.TaskReviewPreparation{}, err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.TaskReviewPreparation{}, err
	}
	defer s.writeMu.Unlock()
	var prepared domain.TaskReviewPreparation
	err := s.inTx(ctx, "prepare native task review", func(q *gen.Queries) error {
		var err error
		prepared, err = prepareTaskReview(ctx, q, resultID, actor)
		return err
	})
	return prepared, err
}

func prepareTaskReview(ctx context.Context, q *gen.Queries, resultID string, actor domain.AdaptiveActor) (domain.TaskReviewPreparation, error) {
	var prepared domain.TaskReviewPreparation
	row, err := q.GetTaskResult(ctx, resultID)
	if err != nil {
		return prepared, taskReadError(err)
	}
	prepared.Result, err = taskResultFromRow(row)
	if err != nil {
		return prepared, err
	}
	task, err := q.GetAdaptiveTask(ctx, prepared.Result.TaskID)
	if err != nil {
		return prepared, err
	}
	prepared.ProjectID = domain.ProjectID(task.ProjectID)
	if err := validateTaskActor(ctx, q, task.ProjectID, actor); err != nil {
		return prepared, err
	}
	latest, err := q.LatestTaskResult(ctx, prepared.Result.AttemptID)
	if err != nil {
		return prepared, err
	}
	if latest.ID != resultID {
		return prepared, ports.ErrTaskConflict
	}
	criteria, err := q.GetAdaptiveTaskCriteria(ctx, gen.GetAdaptiveTaskCriteriaParams{TaskID: prepared.Result.TaskID, Number: prepared.Result.CriteriaVersion})
	if err != nil {
		return prepared, err
	}
	prepared.Criteria, err = taskCriteriaFromRow(criteria)
	if err != nil {
		return prepared, err
	}
	if prepared.Criteria.Definition.ReviewPolicy == nil {
		return prepared, fmt.Errorf("%w: attempt has no frozen review policy", ports.ErrTaskInvalid)
	}
	prepared.Implementer, err = evaluationWorkerConfiguration(ctx, q, prepared.Result)
	return prepared, err
}

// InsertTaskReviewRun atomically binds an existing review pass to its sealed
// context. It repeats the result and mutable registry guards after preparation.
func (s *Store) InsertTaskReviewRun(ctx context.Context, run domain.ReviewRun, snapshot domain.TaskReviewContext) error {
	if err := validateTaskMutation(domain.TaskMutation{Actor: snapshot.Actor, Reason: "Start independent review"}); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ports.ErrTaskInvalid, err)
	}
	if run.TaskScope != snapshot.ScopeHash() || run.SessionID != snapshot.SessionID || run.TargetSHA != snapshot.TargetCommit || domain.AgentHarness(run.Harness) != snapshot.Reviewer.Effective.Harness || run.Status != domain.ReviewRunRunning || run.Verdict != domain.VerdictNone {
		return fmt.Errorf("%w: review pass does not match sealed context", ports.ErrTaskInvalid)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "insert native task review", func(q *gen.Queries) error {
		prepared, err := prepareTaskReview(ctx, q, snapshot.ResultID, snapshot.Actor)
		if err != nil {
			return err
		}
		r := prepared.Result
		if r.TaskID != snapshot.TaskID || r.AttemptID != snapshot.AttemptID || r.SessionID != snapshot.SessionID || r.ContentHash != snapshot.ResultHash || r.TaskRevision != snapshot.TaskRevision || r.CriteriaVersion != snapshot.CriteriaVersion || r.Definition.ClaimedCommit != snapshot.TargetCommit || r.ConfigurationHash != snapshot.ImplementingConfigurationHash || prepared.Criteria.ContentHash != snapshot.CriteriaHash || prepared.Implementer.AgentType != snapshot.ImplementingType || prepared.Implementer.Effective.Harness != snapshot.ImplementingHarness {
			return fmt.Errorf("%w: task review provenance changed", ports.ErrTaskConflict)
		}
		if err := validateTaskReviewerConfiguration(ctx, q, snapshot.Reviewer, prepared.ProjectID); err != nil {
			return err
		}
		review, err := q.GetReviewByID(ctx, run.ReviewID)
		if err != nil {
			return err
		}
		if review.SessionID != run.SessionID || review.Harness != run.Harness || review.ProjectID != prepared.ProjectID {
			return fmt.Errorf("%w: review belongs to another worker", ports.ErrTaskInvalid)
		}
		pr, err := q.GetPR(ctx, run.PRURL)
		if err != nil {
			return taskReadError(err)
		}
		if pr.SessionID != run.SessionID || pr.HeadSha != run.TargetSHA {
			return fmt.Errorf("%w: review PR ownership or head changed", ports.ErrTaskConflict)
		}
		count, err := q.CountTaskReviewContexts(ctx, r.ID)
		if err != nil {
			return err
		}
		if count >= 64 {
			return fmt.Errorf("%w: result reached 64 review passes", ports.ErrTaskInvalid)
		}
		if err := insertReviewRun(ctx, q, run); err != nil {
			return err
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if err := q.InsertTaskReviewContext(ctx, gen.InsertTaskReviewContextParams{RunID: run.ID, ResultID: r.ID, ScopeHash: run.TaskScope, Snapshot: string(encoded), ContentHash: snapshot.ContentHash, LaunchID: snapshot.LaunchID, CreatedAt: snapshot.CreatedAt}); err != nil {
			return err
		}
		return insertTaskAudit(ctx, q, r.TaskID, r.TaskRevision, "review_requested", domain.TaskMutation{Actor: snapshot.Actor, Reason: "Review exact result " + r.ID}, time.Now().UTC())
	})
}

func validateTaskReviewerConfiguration(ctx context.Context, q *gen.Queries, snapshot domain.WorkerConfiguration, projectID domain.ProjectID) error {
	if !reflect.DeepEqual(snapshot.Selection.Overrides, domain.WorkerOverrides{}) || snapshot.Selection.Version != snapshot.AgentType.Version {
		return fmt.Errorf("%w: task reviewer must use exact Type without overrides", ports.ErrTaskInvalid)
	}
	if err := validateWorkerReference(ctx, q, snapshot.AgentType, domain.RegistryAgentType, snapshot.Origin); err != nil {
		return err
	}
	row, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: snapshot.AgentType.ID, Number: snapshot.AgentType.Version})
	if err != nil {
		return err
	}
	version, err := registryVersionFromGen(row)
	if err != nil {
		return err
	}
	definition := version.Definition.AgentType
	if definition == nil || definition.Harness != snapshot.Effective.Harness || definition.Instructions != snapshot.Effective.Instructions || !reflect.DeepEqual(definition.Skills, snapshot.Effective.Skills) || definition.ProviderBindingID != snapshot.Effective.ProviderBindingID {
		return fmt.Errorf("%w: reviewer differs from pinned Type", ports.ErrTaskInvalid)
	}
	for _, skill := range snapshot.Skills {
		if err := validateWorkerReference(ctx, q, skill.Reference, domain.RegistrySkill, snapshot.Origin); err != nil {
			return err
		}
	}
	if snapshot.Provider != nil {
		row, err := q.GetProviderBinding(ctx, snapshot.Provider.ID)
		if err != nil {
			return err
		}
		binding := providerBindingFromGen(row)
		if !binding.Enabled || binding.Revision != snapshot.Provider.Revision || binding.Harness != snapshot.Effective.Harness || binding.Provider != snapshot.Provider.Provider || (binding.ProjectID != "" && binding.ProjectID != string(projectID)) {
			return ports.ErrRegistryConflict
		}
	}
	return nil
}

func taskReviewContextFromRow(row gen.AdaptiveTaskReviewContext) (domain.TaskReviewSnapshot, error) {
	snapshot := domain.TaskReviewSnapshot{RunID: row.RunID}
	if err := json.Unmarshal([]byte(row.Snapshot), &snapshot.Context); err != nil {
		return snapshot, err
	}
	c := snapshot.Context
	if err := c.Validate(); err != nil {
		return snapshot, err
	}
	if c.ResultID != row.ResultID || c.ScopeHash() != row.ScopeHash || c.ContentHash != row.ContentHash || c.LaunchID != row.LaunchID || !c.CreatedAt.Equal(row.CreatedAt) {
		return snapshot, fmt.Errorf("task review context provenance mismatch")
	}
	if row.StartedAt.Valid {
		snapshot.StartedAt = &row.StartedAt.Time
	}
	return snapshot, nil
}

// GetTaskReviewContext reads retained facts without re-resolving live Type state.
func (s *Store) GetTaskReviewContext(ctx context.Context, runID string) (domain.TaskReviewSnapshot, bool, error) {
	row, err := s.qr.GetTaskReviewContext(ctx, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskReviewSnapshot{}, false, nil
	}
	if err != nil {
		return domain.TaskReviewSnapshot{}, false, err
	}
	snapshot, err := taskReviewContextFromRow(row)
	return snapshot, err == nil, err
}

// MarkTaskReviewStarted requires the same native launch identity and a recorded
// handle. A preflight attempt or failed spawn cannot manufacture this witness.
func (s *Store) MarkTaskReviewStarted(ctx context.Context, runID, launchID string) (bool, error) {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return false, err
	}
	defer s.writeMu.Unlock()
	count, err := s.qw.MarkTaskReviewStarted(ctx, gen.MarkTaskReviewStartedParams{RunID: runID, LaunchID: launchID, StartedAt: sql.NullTime{Time: time.Now().UTC(), Valid: true}})
	return count == 1, err
}
