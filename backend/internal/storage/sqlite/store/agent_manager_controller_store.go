package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.AgentManagerControllerStore = (*Store)(nil)

func managerControllerFromRow(row gen.AdaptiveAgentManagerController) (domain.AgentManagerController, error) {
	c := domain.AgentManagerController{AgentManagerControllerToken: domain.AgentManagerControllerToken{ID: row.ID, ProjectID: domain.ProjectID(row.ProjectID), ConfigurationVersion: row.ConfigurationVersion}, Reason: row.Reason, CreatedAt: row.CreatedAt, ReleaseReason: row.ReleaseReason}
	if row.ReleasedAt.Valid {
		c.ReleasedAt = &row.ReleasedAt.Time
	}
	err := json.Unmarshal([]byte(row.Actor), &c.Actor)
	return c, err
}

func managerControllerFenced(row gen.AdaptiveAgentManagerController, token domain.AgentManagerControllerToken) bool {
	return row.ID != token.ID || row.ProjectID != string(token.ProjectID) || row.ConfigurationVersion != token.ConfigurationVersion || row.ReleasedAt.Valid
}

func enabledManagerConfiguration(ctx context.Context, q *gen.Queries, projectID domain.ProjectID) (domain.AgentManagerConfiguration, error) {
	project, err := q.GetProject(ctx, projectID)
	if err != nil {
		return domain.AgentManagerConfiguration{}, agentManagerReadError(err)
	}
	if project.ArchivedAt.Valid {
		return domain.AgentManagerConfiguration{}, ports.ErrAgentManagerFenced
	}
	row, err := q.GetAgentManager(ctx, string(projectID))
	if err != nil {
		return domain.AgentManagerConfiguration{}, agentManagerReadError(err)
	}
	configuration, err := agentManagerConfigurationFromRow(row)
	if err != nil {
		return domain.AgentManagerConfiguration{}, err
	}
	if !configuration.Definition.Enabled {
		return domain.AgentManagerConfiguration{}, ports.ErrAgentManagerFenced
	}
	return configuration, nil
}

func insertManagerControllerAudit(ctx context.Context, q *gen.Queries, controller gen.AdaptiveAgentManagerController, action, reason string, now time.Time) error {
	actor, err := json.Marshal(domain.AdaptiveActor{Kind: "SYSTEM", ID: controller.ID})
	if err != nil {
		return err
	}
	return q.InsertAgentManagerAudit(ctx, gen.InsertAgentManagerAuditParams{ProjectID: controller.ProjectID, ConfigurationVersion: controller.ConfigurationVersion, Action: action, Actor: string(actor), Reason: reason, CreatedAt: now})
}

// ReserveAgentManagerController retains admission before any native process or
// workspace effects. No timeout frees it; exact retries only inspect the record.
func (s *Store) ReserveAgentManagerController(ctx context.Context, request domain.AgentManagerControllerReservation) (domain.AgentManagerController, bool, error) {
	if err := request.Validate(); err != nil {
		return domain.AgentManagerController{}, false, ports.ErrAgentManagerInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.AgentManagerController{}, false, err
	}
	defer s.writeMu.Unlock()
	var result domain.AgentManagerController
	created := false
	err := s.inTx(ctx, "reserve Manager controller", func(q *gen.Queries) error {
		row, err := q.GetAgentManagerController(ctx, request.ID)
		if err == nil {
			result, err = managerControllerFromRow(row)
			if err != nil {
				return err
			}
			if result.AgentManagerControllerToken != request.AgentManagerControllerToken || result.Actor != request.Actor || result.Reason != request.Reason {
				return ports.ErrAgentManagerConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		configuration, err := enabledManagerConfiguration(ctx, q, request.ProjectID)
		if err != nil {
			return err
		}
		if configuration.Number != request.ConfigurationVersion {
			return ports.ErrAgentManagerConflict
		}
		if request.Now.Before(configuration.CreatedAt) {
			return ports.ErrAgentManagerInvalid
		}
		if _, err := q.ActiveAgentManagerController(ctx, string(request.ProjectID)); err == nil {
			return ports.ErrAgentManagerFenced
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		count, err := q.CountAgentManagerControllers(ctx, string(request.ProjectID))
		if err != nil {
			return err
		}
		if count >= 1000 {
			return ports.ErrAgentManagerInvalid
		}
		if err := validateWorkerReference(ctx, q, configuration.ControllerType, domain.RegistryAgentType, domain.RegistryUser); err != nil {
			return err
		}
		actor, err := json.Marshal(request.Actor)
		if err != nil {
			return err
		}
		if err := q.InsertAgentManagerController(ctx, gen.InsertAgentManagerControllerParams{ID: request.ID, ProjectID: string(request.ProjectID), ConfigurationVersion: request.ConfigurationVersion, Actor: string(actor), Reason: request.Reason, CreatedAt: request.Now}); err != nil {
			return err
		}
		row, err = q.GetAgentManagerController(ctx, request.ID)
		if err != nil {
			return err
		}
		if err := insertManagerControllerAudit(ctx, q, row, "controller_reserved", request.Reason, request.Now); err != nil {
			return err
		}
		result, err = managerControllerFromRow(row)
		created = err == nil
		return err
	})
	if err != nil {
		return domain.AgentManagerController{}, false, err
	}
	return result, created, nil
}

// GetAgentManagerController reads retained admission independently of live policy.
func (s *Store) GetAgentManagerController(ctx context.Context, id string) (domain.AgentManagerController, error) {
	row, err := s.qr.GetAgentManagerController(ctx, id)
	if err != nil {
		return domain.AgentManagerController{}, agentManagerReadError(err)
	}
	return managerControllerFromRow(row)
}

// ActiveAgentManagerController reports reserved ownership, not inferred liveness.
func (s *Store) ActiveAgentManagerController(ctx context.Context, project domain.ProjectID) (domain.AgentManagerController, bool, error) {
	if err := validateAgentManagerProject(project); err != nil {
		return domain.AgentManagerController{}, false, err
	}
	row, err := s.qr.ActiveAgentManagerController(ctx, string(project))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentManagerController{}, false, nil
	}
	if err != nil {
		return domain.AgentManagerController{}, false, err
	}
	c, err := managerControllerFromRow(row)
	return c, err == nil, err
}

// ListAgentManagerControllers pages retained admissions, including released ones.
func (s *Store) ListAgentManagerControllers(ctx context.Context, project domain.ProjectID, after string, limit int) ([]domain.AgentManagerController, error) {
	if err := s.validateAgentManagerPage(ctx, project, 0, limit); err != nil {
		return nil, err
	}
	if len(after) > 200 || strings.IndexFunc(after, unicode.IsControl) >= 0 {
		return nil, ports.ErrAgentManagerInvalid
	}
	rows, err := s.qr.ListAgentManagerControllers(ctx, gen.ListAgentManagerControllersParams{ProjectID: string(project), ID: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]domain.AgentManagerController, 0, len(rows))
	for _, row := range rows {
		item, err := managerControllerFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func managerDispatchFromRow(row gen.AdaptiveAgentManagerDispatch) domain.AgentManagerControllerDispatch {
	return domain.AgentManagerControllerDispatch{ControllerID: row.ControllerID, SessionID: domain.SessionID(row.SessionID), ConfigurationHash: row.ConfigurationHash, CreatedAt: row.CreatedAt}
}

// GetAgentManagerDispatch reads the durable pre-process association.
func (s *Store) GetAgentManagerDispatch(ctx context.Context, id string) (domain.AgentManagerControllerDispatch, bool, error) {
	row, err := s.qr.GetAgentManagerDispatch(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentManagerControllerDispatch{}, false, nil
	}
	if err != nil {
		return domain.AgentManagerControllerDispatch{}, false, err
	}
	return managerDispatchFromRow(row), true, nil
}

// GetAgentManagerDispatchBySession distinguishes reserved controllers on restore.
func (s *Store) GetAgentManagerDispatchBySession(ctx context.Context, id domain.SessionID) (domain.AgentManagerControllerDispatch, bool, error) {
	row, err := s.qr.GetAgentManagerDispatchBySession(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentManagerControllerDispatch{}, false, nil
	}
	if err != nil {
		return domain.AgentManagerControllerDispatch{}, false, err
	}
	return managerDispatchFromRow(row), true, nil
}

// CreateAgentManagerSession seals Type/Skills and binds one existing AO session
// seed atomically. created=false is inspection, never another launch permission.
func (s *Store) CreateAgentManagerSession(ctx context.Context, token domain.AgentManagerControllerToken, rec domain.SessionRecord, snapshot domain.WorkerConfiguration, now time.Time) (domain.SessionRecord, bool, error) {
	if now.IsZero() || rec.ProjectID != token.ProjectID || rec.Kind != domain.KindAgentManager {
		return domain.SessionRecord{}, false, ports.ErrAgentManagerInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return domain.SessionRecord{}, false, err
	}
	defer s.writeMu.Unlock()
	var result domain.SessionRecord
	created := false
	err := s.inTx(ctx, "seed Manager controller", func(q *gen.Queries) error {
		controller, err := q.GetAgentManagerController(ctx, token.ID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if managerControllerFenced(controller, token) || now.Before(controller.CreatedAt) {
			return ports.ErrAgentManagerFenced
		}
		prior, err := q.GetAgentManagerDispatch(ctx, token.ID)
		if err == nil {
			if prior.ConfigurationHash != snapshot.ContentHash {
				return ports.ErrAgentManagerConflict
			}
			row, err := q.GetSession(ctx, domain.SessionID(prior.SessionID))
			if err != nil {
				return err
			}
			result = rowToRecord(row)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		configuration, err := enabledManagerConfiguration(ctx, q, token.ProjectID)
		if err != nil {
			return err
		}
		if configuration.Number != token.ConfigurationVersion || snapshot.AgentType.ID != configuration.ControllerType.ID || snapshot.AgentType.Version != configuration.ControllerType.Version || snapshot.AgentType.ContentHash != configuration.ControllerType.ContentHash || snapshot.Selection.Version != configuration.Definition.AgentTypeVersion || snapshot.Selection.Overrides != (domain.WorkerOverrides{}) || snapshot.Origin != domain.RegistryUser || snapshot.ActorID != configuration.Actor.ID {
			return ports.ErrAgentManagerConflict
		}
		result, err = createConfiguredSessionForRole(ctx, q, rec, snapshot, domain.KindAgentManager)
		if err != nil {
			return err
		}
		if err := q.InsertAgentManagerDispatch(ctx, gen.InsertAgentManagerDispatchParams{ControllerID: token.ID, SessionID: string(result.ID), ConfigurationHash: snapshot.ContentHash, CreatedAt: now}); err != nil {
			return err
		}
		if err := insertManagerControllerAudit(ctx, q, controller, "controller_seeded", "Associated exact configuration and native session seed", now); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return domain.SessionRecord{}, false, err
	}
	return result, created, nil
}

// ReleaseAgentManagerController requires confirmed termination and no unresolved
// native side effects. An unseeded reservation can safely be cancelled directly.
func (s *Store) ReleaseAgentManagerController(ctx context.Context, request domain.AgentManagerControllerRelease) error {
	if request.Now.IsZero() || strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 2000 {
		return ports.ErrAgentManagerInvalid
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "release Manager controller", func(q *gen.Queries) error {
		controller, err := q.GetAgentManagerController(ctx, request.Token.ID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if managerControllerFenced(controller, request.Token) || request.Now.Before(controller.CreatedAt) {
			return ports.ErrAgentManagerFenced
		}
		if _, err := q.PendingAgentManagerControllerExecution(ctx, controller.ID); err == nil {
			return ports.ErrAgentManagerFenced
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		dispatch, err := q.GetAgentManagerDispatch(ctx, controller.ID)
		if err == nil {
			row, err := q.GetSession(ctx, domain.SessionID(dispatch.SessionID))
			if err != nil {
				return err
			}
			rec := rowToRecord(row)
			if !rec.IsTerminated || request.ObservedOwner == nil || *request.ObservedOwner != rec.ControllerOwner() {
				return ports.ErrAgentManagerFenced
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		} else if request.ObservedOwner != nil {
			return ports.ErrAgentManagerFenced
		}
		changed, err := q.ReleaseAgentManagerController(ctx, gen.ReleaseAgentManagerControllerParams{ID: controller.ID, ReleasedAt: sql.NullTime{Time: request.Now, Valid: true}, ReleaseReason: request.Reason})
		if err != nil {
			return err
		}
		if changed != 1 {
			return ports.ErrAgentManagerFenced
		}
		return insertManagerControllerAudit(ctx, q, controller, "controller_released", request.Reason, request.Now)
	})
}
