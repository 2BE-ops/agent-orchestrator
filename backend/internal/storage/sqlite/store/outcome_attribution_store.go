package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var (
	_ ports.ManagerRoutingOutcomeStore       = (*Store)(nil)
	_ ports.OrchestratorPlanningOutcomeStore = (*Store)(nil)
)

// ListManagerRoutingOutcomes pages the project's sealed routing decisions — one
// row per request that carries at least one decision — each coupled with the
// read-time fate of its routed task. The cursor is the request's durable arrival
// sequence. Everything is derived from durable rows; nothing is written.
func (s *Store) ListManagerRoutingOutcomes(ctx context.Context, projectID domain.ProjectID, afterSequence int64, limit int) ([]domain.ManagerRoutingOutcome, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: routing outcome page limit must be between 1 and 100", ports.ErrOutcomePageInvalid)
	}
	if afterSequence < 0 {
		return nil, fmt.Errorf("%w: routing outcome cursor must be non-negative", ports.ErrOutcomePageInvalid)
	}
	var outcomes []domain.ManagerRoutingOutcome
	err := s.inTxLocked(ctx, "derive manager routing outcomes", func(q *gen.Queries) error {
		rows, err := q.ListRoutingAttributionRequests(ctx, gen.ListRoutingAttributionRequestsParams{ProjectID: string(projectID), AfterSequence: afterSequence, PageLimit: int64(limit)})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		outcomes = make([]domain.ManagerRoutingOutcome, 0, len(rows))
		for _, id := range rows {
			item, err := managerRoutingOutcome(ctx, q, projectID, id, now)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, item)
		}
		return nil
	})
	return outcomes, err
}

// ManagerRoutingSummary aggregates one complete bounded decision cohort. The
// count and the cohort read run in one locked transaction so the summary can
// never report a silently partial sum.
func (s *Store) ManagerRoutingSummary(ctx context.Context, query domain.OutcomeAttributionQuery) (domain.ManagerRoutingSummary, error) {
	if err := query.Validate(); err != nil {
		return domain.ManagerRoutingSummary{}, fmt.Errorf("%w: %w", ports.ErrOutcomePageInvalid, err)
	}
	var outcomes []domain.ManagerRoutingOutcome
	err := s.inTxLocked(ctx, "summarize manager routing outcomes", func(q *gen.Queries) error {
		count, err := q.CountRoutingAttributionRequestsInWindow(ctx, gen.CountRoutingAttributionRequestsInWindowParams{ProjectID: string(query.ProjectID), FromTime: query.From.UTC(), ToTime: query.To.UTC()})
		if err != nil {
			return err
		}
		if count > domain.OutcomeAttributionLimit {
			return ports.ErrOutcomeCohortTooLarge
		}
		ids, err := q.ListRoutingAttributionRequestIDsInWindow(ctx, gen.ListRoutingAttributionRequestIDsInWindowParams{ProjectID: string(query.ProjectID), FromTime: query.From.UTC(), ToTime: query.To.UTC(), PageLimit: count})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		outcomes = make([]domain.ManagerRoutingOutcome, 0, len(ids))
		for _, id := range ids {
			item, err := managerRoutingOutcome(ctx, q, query.ProjectID, id, now)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, item)
		}
		return nil
	})
	if err != nil {
		return domain.ManagerRoutingSummary{}, err
	}
	return domain.SummarizeManagerRouting(query, outcomes, time.Now().UTC())
}

// managerRoutingOutcome couples one request's chosen decision — the accepted
// selection when present, otherwise its latest rejection — with the derived
// fate of the routed task. Task state comes from the same deterministic
// feedback projection the orchestrator loop reads.
func managerRoutingOutcome(ctx context.Context, q *gen.Queries, projectID domain.ProjectID, requestID string, now time.Time) (domain.ManagerRoutingOutcome, error) {
	row, err := q.GetAgentManagerRequest(ctx, requestID)
	if err != nil {
		return domain.ManagerRoutingOutcome{}, agentManagerReadError(err)
	}
	if row.ProjectID != string(projectID) {
		return domain.ManagerRoutingOutcome{}, ports.ErrAgentManagerNotFound
	}
	decisionRows, err := q.ListAgentManagerDecisions(ctx, requestID)
	if err != nil {
		return domain.ManagerRoutingOutcome{}, err
	}
	if len(decisionRows) == 0 {
		return domain.ManagerRoutingOutcome{}, ports.ErrAgentManagerNotFound
	}
	var chosen domain.AgentManagerDecision
	for _, decisionRow := range decisionRows {
		decision, err := managerDecisionFromRow(decisionRow)
		if err != nil {
			return domain.ManagerRoutingOutcome{}, err
		}
		chosen = decision
		if decision.Outcome == "accepted" {
			break
		}
	}
	outcome := domain.ManagerRoutingOutcome{
		Sequence:     row.Sequence,
		DecisionID:   chosen.ProposalID,
		RequestID:    requestID,
		TaskID:       row.TaskID,
		Outcome:      chosen.Outcome,
		Optimization: chosen.Optimization,
		DecidedAt:    chosen.CreatedAt,
	}
	if chosen.Outcome == "accepted" && len(chosen.Candidates) > 0 {
		selected := chosen.Candidates[0].AgentType
		outcome.AgentType = &selected
	}
	target := taskOutcomeTarget{TaskID: row.TaskID, TaskTitle: &outcome.TaskTitle, Revision: &outcome.Revision, State: &outcome.State, Reason: &outcome.Reason, ResultID: &outcome.ResultID, EvaluationID: &outcome.EvaluationID, Attempts: &outcome.Attempts}
	if err := attributeTaskOutcome(ctx, q, target, now); err != nil {
		return domain.ManagerRoutingOutcome{}, err
	}
	return outcome, nil
}

// taskOutcomeTarget inlays one attributed task's derived fields into either
// attribution row so routing and planning fills stay identical.
type taskOutcomeTarget struct {
	TaskID       string
	TaskTitle    *string
	Revision     *int64
	State        *string
	Reason       *string
	ResultID     *string
	EvaluationID *string
	Attempts     *int64
}

func attributeTaskOutcome(ctx context.Context, q *gen.Queries, target taskOutcomeTarget, now time.Time) error {
	feedback, err := projectFeedbackItem(ctx, q, target.TaskID, now)
	if err != nil {
		return err
	}
	*target.TaskTitle = feedback.Title
	*target.Revision = feedback.Revision
	*target.State = feedback.State
	*target.Reason = feedback.Reason
	*target.ResultID = feedback.ResultID
	*target.EvaluationID = feedback.EvaluationID
	attempts, err := q.ListTaskAttempts(ctx, gen.ListTaskAttemptsParams{TaskID: target.TaskID, Number: 0, Limit: 1000})
	if err != nil {
		return err
	}
	*target.Attempts = int64(len(attempts))
	return nil
}

// ListOrchestratorPlanningOutcomes pages the project's sealed planning
// receipts, each coupled with the read-time fate of the task it created or
// revised. The cursor matches the public receipt history.
func (s *Store) ListOrchestratorPlanningOutcomes(ctx context.Context, projectID domain.ProjectID, afterReceiptID string, limit int) ([]domain.OrchestratorPlanningOutcome, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: planning outcome page limit must be between 1 and 100", ports.ErrOutcomePageInvalid)
	}
	var outcomes []domain.OrchestratorPlanningOutcome
	err := s.inTxLocked(ctx, "derive orchestrator planning outcomes", func(q *gen.Queries) error {
		rows, err := q.ListOrchestratorPlanReceipts(ctx, gen.ListOrchestratorPlanReceiptsParams{ProjectID: string(projectID), ID: afterReceiptID, Limit: int64(limit)})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		outcomes = make([]domain.OrchestratorPlanningOutcome, 0, len(rows))
		for _, row := range rows {
			item, err := orchestratorPlanningOutcome(ctx, q, row, now)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, item)
		}
		return nil
	})
	return outcomes, err
}

// OrchestratorPlanningSummary aggregates one complete bounded receipt cohort.
func (s *Store) OrchestratorPlanningSummary(ctx context.Context, query domain.OutcomeAttributionQuery) (domain.OrchestratorPlanningSummary, error) {
	if err := query.Validate(); err != nil {
		return domain.OrchestratorPlanningSummary{}, fmt.Errorf("%w: %w", ports.ErrOutcomePageInvalid, err)
	}
	var outcomes []domain.OrchestratorPlanningOutcome
	err := s.inTxLocked(ctx, "summarize orchestrator planning outcomes", func(q *gen.Queries) error {
		count, err := q.CountOrchestratorPlanReceiptsInWindow(ctx, gen.CountOrchestratorPlanReceiptsInWindowParams{ProjectID: string(query.ProjectID), FromTime: query.From.UTC(), ToTime: query.To.UTC()})
		if err != nil {
			return err
		}
		if count > domain.OutcomeAttributionLimit {
			return ports.ErrOutcomeCohortTooLarge
		}
		rows, err := q.ListOrchestratorPlanReceiptsInWindow(ctx, gen.ListOrchestratorPlanReceiptsInWindowParams{ProjectID: string(query.ProjectID), FromTime: query.From.UTC(), ToTime: query.To.UTC(), PageLimit: count})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		outcomes = make([]domain.OrchestratorPlanningOutcome, 0, len(rows))
		for _, row := range rows {
			item, err := orchestratorPlanningOutcome(ctx, q, row, now)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, item)
		}
		return nil
	})
	if err != nil {
		return domain.OrchestratorPlanningSummary{}, err
	}
	return domain.SummarizeOrchestratorPlanning(query, outcomes, time.Now().UTC())
}

func orchestratorPlanningOutcome(ctx context.Context, q *gen.Queries, row gen.AdaptiveOrchestratorPlanReceipt, now time.Time) (domain.OrchestratorPlanningOutcome, error) {
	receipt, err := orchestratorPlanReceiptFromRow(row)
	if err != nil {
		return domain.OrchestratorPlanningOutcome{}, err
	}
	outcome := domain.OrchestratorPlanningOutcome{
		ReceiptID: receipt.Outcome.ReceiptID,
		Action:    receipt.Outcome.Action,
		TaskID:    receipt.Outcome.TaskID,
		Revision:  receipt.Outcome.Revision,
		CreatedAt: receipt.Outcome.CreatedAt,
	}
	target := taskOutcomeTarget{TaskID: receipt.Outcome.TaskID, TaskTitle: &outcome.TaskTitle, Revision: &outcome.Revision, State: &outcome.State, Reason: &outcome.Reason, ResultID: &outcome.ResultID, EvaluationID: &outcome.EvaluationID, Attempts: &outcome.Attempts}
	if err := attributeTaskOutcome(ctx, q, target, now); err != nil {
		return domain.OrchestratorPlanningOutcome{}, err
	}
	return outcome, nil
}
