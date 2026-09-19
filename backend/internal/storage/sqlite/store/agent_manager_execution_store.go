package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func managerExecutionFromRow(row gen.AdaptiveAgentManagerExecutionOperation) (domain.AgentManagerExecutionOperation, error) {
	op := domain.AgentManagerExecutionOperation{ID: row.ID, ControllerID: row.ControllerID, SessionID: domain.SessionID(row.SessionID), Kind: row.Kind, CreatedAt: row.CreatedAt}
	err := json.Unmarshal([]byte(row.SourceOwner), &op.SourceOwner)
	return op, err
}

// BeginAgentManagerExecution reserves a generation before native side effects.
// Repeating it only acknowledges retained intent and never authorizes a replay.
func (s *Store) BeginAgentManagerExecution(ctx context.Context, op domain.AgentManagerExecutionOperation) (bool, error) {
	if strings.TrimSpace(op.ID) == "" || len(op.ID) > 200 || strings.IndexFunc(op.ID, unicode.IsControl) >= 0 || op.SessionID == "" || op.ControllerID == "" || op.CreatedAt.IsZero() || (op.Kind != "dispatch" && op.Kind != "restore") {
		return false, ports.ErrAgentManagerInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return false, err
	}
	defer s.writeMu.Unlock()
	created := false
	err := s.inTx(ctx, "reserve Manager native execution", func(q *gen.Queries) error {
		previous, err := q.GetAgentManagerExecution(ctx, op.ID)
		if err == nil {
			prior, err := managerExecutionFromRow(previous)
			if err != nil {
				return err
			}
			if prior.ControllerID != op.ControllerID || prior.SessionID != op.SessionID || prior.Kind != op.Kind || prior.SourceOwner != op.SourceOwner {
				return ports.ErrAgentManagerConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		controller, err := q.GetAgentManagerController(ctx, op.ControllerID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if controller.ReleasedAt.Valid || op.CreatedAt.Before(controller.CreatedAt) {
			return ports.ErrAgentManagerFenced
		}
		configuration, err := enabledManagerConfiguration(ctx, q, domain.ProjectID(controller.ProjectID))
		if err != nil {
			return err
		}
		if op.Kind == "dispatch" && configuration.Number != controller.ConfigurationVersion {
			return ports.ErrAgentManagerFenced
		}
		dispatch, err := q.GetAgentManagerDispatch(ctx, controller.ID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if dispatch.SessionID != string(op.SessionID) {
			return ports.ErrAgentManagerFenced
		}
		if op.CreatedAt.Before(dispatch.CreatedAt) {
			return ports.ErrAgentManagerInvalid
		}
		if op.Kind == "dispatch" {
			count, err := q.CountAgentManagerExecutions(ctx, controller.ID)
			if err != nil {
				return err
			}
			if count != 0 {
				return ports.ErrAgentManagerFenced
			}
		}
		if _, err := q.PendingAgentManagerControllerExecution(ctx, controller.ID); err == nil {
			return ports.ErrAgentManagerFenced
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		row, err := q.GetSession(ctx, op.SessionID)
		if err != nil {
			return err
		}
		rec := rowToRecord(row)
		if rec.Kind != domain.KindAgentManager || string(rec.ProjectID) != controller.ProjectID || rec.ControllerOwner() != op.SourceOwner || (op.Kind == "dispatch" && rec.IsTerminated) {
			return ports.ErrAgentManagerFenced
		}
		owner, err := json.Marshal(op.SourceOwner)
		if err != nil {
			return err
		}
		if err := q.InsertAgentManagerExecution(ctx, gen.InsertAgentManagerExecutionParams{ID: op.ID, ControllerID: controller.ID, SessionID: string(op.SessionID), SourceOwner: string(owner), Kind: op.Kind, CreatedAt: op.CreatedAt}); err != nil {
			return err
		}
		if err := insertManagerControllerAudit(ctx, q, controller, "controller_execution_reserved", "Reserved native "+op.Kind+" generation "+op.ID, op.CreatedAt); err != nil {
			return err
		}
		created = true
		return nil
	})
	return created && err == nil, err
}

// PendingAgentManagerExecution survives daemon failure without treating expiry as death.
func (s *Store) PendingAgentManagerExecution(ctx context.Context, session domain.SessionID) (domain.AgentManagerExecutionOperation, bool, error) {
	row, err := s.qr.PendingAgentManagerExecution(ctx, string(session))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentManagerExecutionOperation{}, false, nil
	}
	if err != nil {
		return domain.AgentManagerExecutionOperation{}, false, err
	}
	op, err := managerExecutionFromRow(row)
	return op, err == nil, err
}

// ResolveAgentManagerExecution records independently confirmed native evidence,
// fencing the exact observed owner and target generation for connected processes.
func (s *Store) ResolveAgentManagerExecution(ctx context.Context, resolution domain.AgentManagerExecutionResolution) error {
	if resolution.CreatedAt.IsZero() || strings.TrimSpace(resolution.Reason) == "" || len(resolution.Reason) > 2000 || (resolution.Outcome != "connected" && resolution.Outcome != "terminated") {
		return ports.ErrAgentManagerInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "resolve Manager native execution", func(q *gen.Queries) error {
		owner, err := json.Marshal(resolution.ObservedOwner)
		if err != nil {
			return err
		}
		previous, err := q.GetAgentManagerExecutionResolution(ctx, resolution.OperationID)
		if err == nil {
			if previous.ObservedOwner != string(owner) || previous.Outcome != resolution.Outcome || previous.Reason != resolution.Reason {
				return ports.ErrAgentManagerConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		row, err := q.GetAgentManagerExecution(ctx, resolution.OperationID)
		if err != nil {
			return agentManagerReadError(err)
		}
		op, err := managerExecutionFromRow(row)
		if err != nil {
			return err
		}
		if resolution.CreatedAt.Before(op.CreatedAt) {
			return ports.ErrAgentManagerInvalid
		}
		controller, err := q.GetAgentManagerController(ctx, op.ControllerID)
		if err != nil {
			return err
		}
		if controller.ReleasedAt.Valid {
			return ports.ErrAgentManagerFenced
		}
		session, err := q.GetSession(ctx, op.SessionID)
		if err != nil {
			return err
		}
		rec := rowToRecord(session)
		if rec.ControllerOwner() != resolution.ObservedOwner || rec.IsTerminated != (resolution.Outcome == "terminated") {
			return ports.ErrAgentManagerFenced
		}
		if resolution.Outcome == "connected" {
			generation := resolution.ObservedOwner.RuntimeLaunchID
			if resolution.ObservedOwner.Mode == domain.SessionModeChat {
				generation = resolution.ObservedOwner.ControllerGeneration
			}
			adopted := resolution.ObservedOwner.Mode == domain.SessionModeChat && generation != "" && resolution.ObservedOwner == op.SourceOwner
			if generation != op.ID && !adopted {
				return ports.ErrAgentManagerFenced
			}
		}
		if err := q.InsertAgentManagerExecutionResolution(ctx, gen.InsertAgentManagerExecutionResolutionParams{OperationID: op.ID, ObservedOwner: string(owner), Outcome: resolution.Outcome, Reason: resolution.Reason, CreatedAt: resolution.CreatedAt}); err != nil {
			return err
		}
		return insertManagerControllerAudit(ctx, q, controller, "controller_execution_"+resolution.Outcome, resolution.Reason, resolution.CreatedAt)
	})
}
