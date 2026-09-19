package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.ProjectGoalStore = (*Store)(nil)
var _ ports.OrchestratorPlanStore = (*Store)(nil)

// projectGoalEnumerationBound caps how many tasks one completion verification
// walks; a larger project must be decomposed before its goal can complete.
const projectGoalEnumerationBound = 10000

func projectGoalVersionFromRow(row gen.AdaptiveProjectGoalVersion) (domain.ProjectGoalVersion, error) {
	result := domain.ProjectGoalVersion{ProjectID: domain.ProjectID(row.ProjectID), Number: row.Number, Goal: row.Goal, Reason: row.Reason, ContentHash: row.ContentHash, CreatedAt: row.CreatedAt}
	err := json.Unmarshal([]byte(row.Actor), &result.Actor)
	return result, err
}

func projectGoalCompletionFromRow(row gen.AdaptiveProjectGoalCompletion) (domain.ProjectGoalCompletion, error) {
	result := domain.ProjectGoalCompletion{ID: row.ID, ProjectID: domain.ProjectID(row.ProjectID), GoalVersion: row.GoalVersion, Summary: row.Summary, Reason: row.Reason, CreatedAt: row.CreatedAt}
	if err := json.Unmarshal([]byte(row.Evidence), &result.Evidence); err != nil {
		return result, err
	}
	return result, json.Unmarshal([]byte(row.Actor), &result.Actor)
}

// SetProjectGoal appends one immutable goal wording and moves the pointer.
// Only user or system authority passes domain validation.
func (s *Store) SetProjectGoal(ctx context.Context, projectID domain.ProjectID, goal, reason string, actor domain.AdaptiveActor) (domain.ProjectGoalVersion, error) {
	if err := domain.ValidateGoalAuthoring(actor, goal, reason); err != nil {
		return domain.ProjectGoalVersion{}, fmt.Errorf("%w: %w", ports.ErrGoalInvalid, err)
	}
	if projectID == "" || len(projectID) > 200 || strings.ContainsRune(string(projectID), 0) {
		return domain.ProjectGoalVersion{}, fmt.Errorf("%w: invalid project identity", ports.ErrGoalInvalid)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.ProjectGoalVersion{}, err
	}
	defer s.writeMu.Unlock()
	var result domain.ProjectGoalVersion
	err := s.inTx(ctx, "set project goal", func(q *gen.Queries) error {
		if _, err := q.GetProject(ctx, projectID); err != nil {
			return taskReadError(err)
		}
		number, err := q.NextProjectGoalVersion(ctx, string(projectID))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		version := domain.ProjectGoalVersion{ProjectID: projectID, Number: number, Goal: goal, Actor: actor, Reason: reason, CreatedAt: now}
		hash, err := domain.GoalContent(version)
		if err != nil {
			return err
		}
		version.ContentHash = hash
		encoded, err := json.Marshal(actor)
		if err != nil {
			return err
		}
		if err := q.InsertProjectGoalVersion(ctx, gen.InsertProjectGoalVersionParams{ProjectID: string(projectID), Number: number, Goal: goal, Actor: string(encoded), Reason: reason, ContentHash: hash, CreatedAt: now}); err != nil {
			return err
		}
		if _, err := q.UpsertProjectGoalPointer(ctx, gen.UpsertProjectGoalPointerParams{ProjectID: string(projectID), CurrentVersion: number, UpdatedAt: now}); err != nil {
			return err
		}
		result = version
		return nil
	})
	return result, err
}

// GetProjectGoal returns the current goal wording of a project.
func (s *Store) GetProjectGoal(ctx context.Context, projectID domain.ProjectID) (domain.ProjectGoalVersion, error) {
	pointer, err := s.qr.GetProjectGoalPointer(ctx, string(projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectGoalVersion{}, ports.ErrGoalNotFound
	}
	if err != nil {
		return domain.ProjectGoalVersion{}, err
	}
	row, err := s.qr.GetProjectGoalVersion(ctx, gen.GetProjectGoalVersionParams{ProjectID: string(projectID), Number: pointer.CurrentVersion})
	if err != nil {
		return domain.ProjectGoalVersion{}, err
	}
	return projectGoalVersionFromRow(row)
}

// ListProjectGoalVersions pages immutable goal history (limit 1-100).
func (s *Store) ListProjectGoalVersions(ctx context.Context, projectID domain.ProjectID, after int64, limit int) ([]domain.ProjectGoalVersion, error) {
	if limit < 1 || limit > 100 || after < 0 {
		return nil, fmt.Errorf("%w: goal history page must be after>=0 with limit 1-100", ports.ErrGoalInvalid)
	}
	rows, err := s.qr.ListProjectGoalVersions(ctx, gen.ListProjectGoalVersionsParams{ProjectID: string(projectID), Number: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.ProjectGoalVersion, 0, len(rows))
	for _, row := range rows {
		version, err := projectGoalVersionFromRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, version)
	}
	return result, nil
}

// CompleteProjectGoal verifies the current goal version against the project's
// durable task facts and retains the sealed decision. created=false means an
// identical completion already exists for that goal version.
func (s *Store) CompleteProjectGoal(ctx context.Context, request domain.ProjectGoalCompletionRequest) (domain.ProjectGoalCompletion, bool, error) {
	if err := request.Validate(); err != nil {
		return domain.ProjectGoalCompletion{}, false, fmt.Errorf("%w: %w", ports.ErrGoalInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.ProjectGoalCompletion{}, false, err
	}
	defer s.writeMu.Unlock()
	var completion domain.ProjectGoalCompletion
	created := false
	err := s.inTx(ctx, "complete project goal", func(q *gen.Queries) error {
		if _, err := q.GetProjectGoalVersion(ctx, gen.GetProjectGoalVersionParams{ProjectID: string(request.ProjectID), Number: request.GoalVersion}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ports.ErrGoalNotFound
			}
			return err
		}
		pointer, err := q.GetProjectGoalPointer(ctx, string(request.ProjectID))
		if err != nil {
			return ports.ErrGoalNotFound
		}
		if pointer.CurrentVersion != request.GoalVersion {
			return ports.ErrGoalConflict
		}
		row, err := q.GetProjectGoalCompletion(ctx, gen.GetProjectGoalCompletionParams{ProjectID: string(request.ProjectID), GoalVersion: request.GoalVersion})
		switch {
		case err == nil:
			completion, err = projectGoalCompletionFromRow(row)
			return err
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		evidence, blockers, err := verifyProjectGoalFacts(ctx, q, request)
		if err != nil {
			return err
		}
		if len(blockers.Items) > 0 {
			return fmt.Errorf("%w: %w", ports.ErrGoalIncomplete, blockers)
		}
		encodedEvidence, err := json.Marshal(evidence)
		if err != nil {
			return err
		}
		encodedActor, err := json.Marshal(request.Actor)
		if err != nil {
			return err
		}
		if err := q.InsertProjectGoalCompletion(ctx, gen.InsertProjectGoalCompletionParams{ID: request.ID, ProjectID: string(request.ProjectID), GoalVersion: request.GoalVersion, Summary: request.Summary, Evidence: string(encodedEvidence), Actor: string(encodedActor), Reason: request.Reason, CreatedAt: request.Now}); err != nil {
			return err
		}
		completion = domain.ProjectGoalCompletion{ID: request.ID, ProjectID: request.ProjectID, GoalVersion: request.GoalVersion, Summary: request.Summary, Evidence: evidence, Actor: request.Actor, Reason: request.Reason, CreatedAt: request.Now}
		created = true
		return nil
	})
	if err != nil {
		return domain.ProjectGoalCompletion{}, false, err
	}
	return completion, created, nil
}

// verifyProjectGoalFacts walks every project task and derives the terminal
// fact set. Non-terminal work is reported as blockers, never stored.
func verifyProjectGoalFacts(ctx context.Context, q *gen.Queries, request domain.ProjectGoalCompletionRequest) (domain.ProjectGoalEvidence, domain.ProjectGoalBlockers, error) {
	goalRow, err := q.GetProjectGoalVersion(ctx, gen.GetProjectGoalVersionParams{ProjectID: string(request.ProjectID), Number: request.GoalVersion})
	if err != nil {
		return domain.ProjectGoalEvidence{}, domain.ProjectGoalBlockers{}, err
	}
	evidence := domain.ProjectGoalEvidence{ProjectID: request.ProjectID, GoalVersion: request.GoalVersion, GoalHash: goalRow.ContentHash, VerifiedTasks: []domain.ProjectGoalEvidenceTask{}}
	blockers := domain.ProjectGoalBlockers{ProjectID: request.ProjectID, GoalVersion: request.GoalVersion, Items: []domain.ProjectGoalBlocker{}}
	after := ""
	total := 0
	for {
		ids, err := q.ListAdaptiveTaskIDs(ctx, gen.ListAdaptiveTaskIDsParams{ProjectID: string(request.ProjectID), ID: after, Limit: 500})
		if err != nil {
			return evidence, blockers, err
		}
		for _, id := range ids {
			total++
			if total > projectGoalEnumerationBound {
				return evidence, blockers, fmt.Errorf("%w: project exceeds goal verification enumeration", ports.ErrGoalInvalid)
			}
			after = id
			task, err := q.GetAdaptiveTask(ctx, id)
			if err != nil {
				return evidence, blockers, err
			}
			cancelledBy, err := q.CancelledTaskAncestor(ctx, id)
			if errors.Is(err, sql.ErrNoRows) {
				err = nil
			}
			if err != nil {
				return evidence, blockers, err
			}
			if cancelledBy != "" {
				evidence.VerifiedTasks = append(evidence.VerifiedTasks, domain.ProjectGoalEvidenceTask{TaskID: id, Revision: task.Revision, State: "cancelled", FactHash: taskFactHash(id, task.Revision, "cancelled", "", "")})
				continue
			}
			proof, err := currentTaskCompletion(ctx, q, id, task.Revision, request.Now)
			if err != nil {
				return evidence, blockers, err
			}
			if !proof.Verified {
				blockers.Items = append(blockers.Items, domain.ProjectGoalBlocker{TaskID: id, Revision: task.Revision, State: taskGoalBlockerState(proof), Reason: proof.Reason})
				continue
			}
			evidence.VerifiedTasks = append(evidence.VerifiedTasks, domain.ProjectGoalEvidenceTask{TaskID: id, Revision: task.Revision, State: "completed", ResultID: proof.ResultID, EvaluationID: proof.EvaluationID, FactHash: taskFactHash(id, task.Revision, "completed", proof.ResultID, proof.EvaluationID)})
		}
		if len(ids) < 500 {
			// A satisfied goal must rest on at least one independently verified
			// completed task; all-cancelled or empty projects do not complete.
			completed := false
			for _, task := range evidence.VerifiedTasks {
				completed = completed || task.State == "completed"
			}
			if !completed {
				blockers.Items = append(blockers.Items, domain.ProjectGoalBlocker{State: "pending", Reason: "No independently verified completed task exists for this goal"})
			}
			return evidence, blockers, nil
		}
	}
}

func taskGoalBlockerState(proof domain.TaskCompletion) string {
	switch {
	case proof.AttemptID != "":
		return "working"
	case proof.ResultID != "":
		return "pending"
	default:
		return "pending"
	}
}

func taskFactHash(taskID string, revision int64, state, resultID, evaluationID string) string {
	data, _ := json.Marshal(struct {
		TaskID       string `json:"taskId"`
		Revision     int64  `json:"revision"`
		State        string `json:"state"`
		ResultID     string `json:"resultId"`
		EvaluationID string `json:"evaluationId"`
	}{taskID, revision, state, resultID, evaluationID})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// GetProjectGoalCompletion reads one retained completion by goal version.
func (s *Store) GetProjectGoalCompletion(ctx context.Context, projectID domain.ProjectID, goalVersion int64) (domain.ProjectGoalCompletion, error) {
	row, err := s.qr.GetProjectGoalCompletion(ctx, gen.GetProjectGoalCompletionParams{ProjectID: string(projectID), GoalVersion: goalVersion})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectGoalCompletion{}, ports.ErrGoalNotFound
	}
	if err != nil {
		return domain.ProjectGoalCompletion{}, err
	}
	return projectGoalCompletionFromRow(row)
}

// ListProjectGoalCompletions pages retained completions by id keyset.
func (s *Store) ListProjectGoalCompletions(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.ProjectGoalCompletion, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: completion page limit must be between 1 and 100", ports.ErrGoalInvalid)
	}
	rows, err := s.qr.ListProjectGoalCompletions(ctx, gen.ListProjectGoalCompletionsParams{ProjectID: string(projectID), ID: afterID, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.ProjectGoalCompletion, 0, len(rows))
	for _, row := range rows {
		completion, err := projectGoalCompletionFromRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, completion)
	}
	return result, nil
}

func orchestratorPlanReceiptFromRow(row gen.AdaptiveOrchestratorPlanReceipt) (domain.OrchestratorPlanReceipt, error) {
	var outcome domain.OrchestratorPlanOutcome
	if err := json.Unmarshal([]byte(row.Outcome), &outcome); err != nil {
		return domain.OrchestratorPlanReceipt{}, err
	}
	return domain.OrchestratorPlanReceipt{Outcome: outcome}, nil
}

// ApplyOrchestratorPlan executes one native planning action and seals its
// receipt in the same transaction. An exact retry returns the original receipt
// without re-running the action; a changed payload under the same key conflicts.
func (s *Store) ApplyOrchestratorPlan(ctx context.Context, submission domain.OrchestratorPlanSubmission) (domain.OrchestratorPlanReceipt, bool, error) {
	if err := submission.Validate(); err != nil {
		return domain.OrchestratorPlanReceipt{}, false, fmt.Errorf("%w: %w", ports.ErrGoalInvalid, err)
	}
	hash, err := domain.OrchestratorPlanRequestHash(submission.ProjectID, submission.SessionID, submission.IdempotencyKey, submission.Action)
	if err != nil {
		return domain.OrchestratorPlanReceipt{}, false, err
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.OrchestratorPlanReceipt{}, false, err
	}
	defer s.writeMu.Unlock()
	var receipt domain.OrchestratorPlanReceipt
	created := false
	err = s.inTx(ctx, "apply orchestrator plan", func(q *gen.Queries) error {
		row, err := q.GetOrchestratorPlanReceiptByKey(ctx, gen.GetOrchestratorPlanReceiptByKeyParams{ProjectID: string(submission.ProjectID), IdempotencyKey: submission.IdempotencyKey})
		switch {
		case err == nil:
			if row.RequestHash != hash {
				return ports.ErrOrchestratorPlanConflict
			}
			receipt, err = orchestratorPlanReceiptFromRow(row)
			return err
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		mutation := domain.TaskMutation{Actor: domain.AdaptiveActor{Kind: "ORCHESTRATOR", ID: string(submission.SessionID), SessionID: submission.SessionID}, Reason: submission.Action.Reason, ExpectedRevision: submission.Action.ExpectedRevision}
		outcome := domain.OrchestratorPlanOutcome{ReceiptID: submission.ReceiptID, ProjectID: string(submission.ProjectID), SessionID: string(submission.SessionID), IdempotencyKey: submission.IdempotencyKey, Action: submission.Action.Action, RequestHash: hash, CreatedAt: submission.Now}
		switch submission.Action.Action {
		case "create_task":
			if _, err := createAdaptiveTaskTx(ctx, q, submission.NewTaskID, submission.ProjectID, *submission.Action.Definition, nil, mutation, submission.Now); err != nil {
				return err
			}
			outcome.TaskID, outcome.Revision = submission.NewTaskID, 1
		case "revise_task":
			revision, err := reviseAdaptiveTaskTx(ctx, q, submission.Action.TaskID, submission.Action.Definition, nil, mutation, submission.Now)
			if err != nil {
				return err
			}
			outcome.TaskID, outcome.Revision, outcome.CriteriaVersion = submission.Action.TaskID, revision.Number, revision.CriteriaVersion
		case "freeze_criteria":
			revision, err := reviseAdaptiveTaskTx(ctx, q, submission.Action.TaskID, nil, submission.Action.Criteria, mutation, submission.Now)
			if err != nil {
				return err
			}
			outcome.TaskID, outcome.Revision, outcome.CriteriaVersion = submission.Action.TaskID, revision.Number, revision.CriteriaVersion
		default:
			return fmt.Errorf("%w: unknown plan action", ports.ErrGoalInvalid)
		}
		if outcome.CriteriaVersion == 0 {
			row, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: outcome.TaskID, Number: outcome.Revision})
			if err != nil {
				return err
			}
			outcome.CriteriaVersion = row.CriteriaVersion.Int64
		}
		encoded, err := json.Marshal(outcome)
		if err != nil {
			return err
		}
		if err := q.InsertOrchestratorPlanReceipt(ctx, gen.InsertOrchestratorPlanReceiptParams{ID: submission.ReceiptID, ProjectID: string(submission.ProjectID), SessionID: string(submission.SessionID), IdempotencyKey: submission.IdempotencyKey, ActionKind: submission.Action.Action, RequestHash: hash, Outcome: string(encoded), CreatedAt: submission.Now}); err != nil {
			if isSQLiteUnique(err) {
				return ports.ErrOrchestratorPlanConflict
			}
			return err
		}
		receipt = domain.OrchestratorPlanReceipt{Outcome: outcome}
		created = true
		return nil
	})
	if err != nil {
		return domain.OrchestratorPlanReceipt{}, false, err
	}
	return receipt, created, nil
}

// GetOrchestratorPlanReceipt reads one retained planning receipt.
func (s *Store) GetOrchestratorPlanReceipt(ctx context.Context, projectID domain.ProjectID, id string) (domain.OrchestratorPlanReceipt, error) {
	row, err := s.qr.GetOrchestratorPlanReceiptByID(ctx, gen.GetOrchestratorPlanReceiptByIDParams{ProjectID: string(projectID), ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.OrchestratorPlanReceipt{}, ports.ErrOrchestratorPlanNotFound
	}
	if err != nil {
		return domain.OrchestratorPlanReceipt{}, err
	}
	return orchestratorPlanReceiptFromRow(row)
}

// ListOrchestratorPlanReceipts pages retained planning receipts by id keyset.
func (s *Store) ListOrchestratorPlanReceipts(ctx context.Context, projectID domain.ProjectID, afterID string, limit int) ([]domain.OrchestratorPlanReceipt, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: receipt page limit must be between 1 and 100", ports.ErrGoalInvalid)
	}
	rows, err := s.qr.ListOrchestratorPlanReceipts(ctx, gen.ListOrchestratorPlanReceiptsParams{ProjectID: string(projectID), ID: afterID, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	result := make([]domain.OrchestratorPlanReceipt, 0, len(rows))
	for _, row := range rows {
		receipt, err := orchestratorPlanReceiptFromRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, receipt)
	}
	return result, nil
}
