package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var _ ports.AgentManagerStore = (*Store)(nil)

func agentManagerReadError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrAgentManagerNotFound
	}
	return err
}

func validateAgentManagerProject(projectID domain.ProjectID) error {
	if strings.TrimSpace(string(projectID)) == "" || len(projectID) > 200 || strings.IndexFunc(string(projectID), unicode.IsControl) >= 0 {
		return ports.ErrAgentManagerInvalid
	}
	return nil
}

func agentManagerConfigurationFromRow(row gen.AdaptiveAgentManagerConfiguration) (domain.AgentManagerConfiguration, error) {
	var configuration domain.AgentManagerConfiguration
	if err := json.Unmarshal([]byte(row.Snapshot), &configuration); err != nil {
		return configuration, err
	}
	if err := configuration.Validate(); err != nil {
		return configuration, err
	}
	if string(configuration.ProjectID) != row.ProjectID || configuration.Number != row.Number || configuration.ContentHash != row.ContentHash || configuration.ControllerType.ID != row.AgentTypeID || configuration.ControllerType.Version != row.AgentTypeVersion || !configuration.CreatedAt.Equal(row.CreatedAt) {
		return domain.AgentManagerConfiguration{}, fmt.Errorf("manager configuration identity mismatch")
	}
	return configuration, nil
}

// ConfigureAgentManager atomically versions user governance and records audit/CDC.
// It has no process effects and cannot grant a manager authority over its policy.
func (s *Store) ConfigureAgentManager(ctx context.Context, projectID domain.ProjectID, definition domain.AgentManagerDefinition, mutation domain.TaskMutation) (domain.AgentManagerConfiguration, error) {
	var configuration domain.AgentManagerConfiguration
	if mutation.Actor.Kind != "USER" || mutation.Actor.SessionID != "" {
		return configuration, ports.ErrAgentManagerForbidden
	}
	if err := mutation.ValidatePlanning(); err != nil {
		return configuration, fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
	}
	if mutation.ExpectedRevision >= 1000 {
		return configuration, ports.ErrAgentManagerInvalid
	}
	if err := validateAgentManagerProject(projectID); err != nil {
		return configuration, err
	}
	if err := definition.Validate(); err != nil {
		return configuration, fmt.Errorf("%w: %w", ports.ErrAgentManagerInvalid, err)
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return configuration, err
	}
	defer s.writeMu.Unlock()
	err := s.inTx(ctx, "configure agent manager", func(q *gen.Queries) error {
		project, err := q.GetProject(ctx, projectID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if project.ArchivedAt.Valid {
			return ports.ErrAgentManagerInvalid
		}
		current, err := q.GetAgentManagerRecord(ctx, string(projectID))
		missing := errors.Is(err, sql.ErrNoRows)
		if err != nil && !missing {
			return err
		}
		if (missing && mutation.ExpectedRevision != 0) || (!missing && current.Revision != mutation.ExpectedRevision) {
			return ports.ErrAgentManagerConflict
		}
		entry, err := q.GetRegistryEntry(ctx, definition.AgentTypeID)
		if err != nil {
			return agentManagerReadError(err)
		}
		if entry.Kind != string(domain.RegistryAgentType) || (definition.Enabled && entry.Enabled == 0) {
			return ports.ErrAgentManagerInvalid
		}
		versionRow, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: entry.ID, Number: definition.AgentTypeVersion})
		if err != nil {
			return agentManagerReadError(err)
		}
		version, err := registryVersionFromGen(versionRow)
		if err != nil {
			return err
		}
		if err := version.Definition.Validate(domain.RegistryAgentType); err != nil {
			return err
		}
		now := time.Now().UTC()
		configuration = domain.AgentManagerConfiguration{ProjectID: projectID, Number: mutation.ExpectedRevision + 1, Definition: definition, ControllerType: domain.WorkerDefinitionRef{ID: entry.ID, Version: version.Number, Name: entry.Name, ContentHash: version.ContentHash}, Actor: mutation.Actor, Reason: mutation.Reason, CreatedAt: now}
		configuration.ContentHash = configuration.Hash()
		if err := configuration.Validate(); err != nil {
			return err
		}
		if missing {
			if err := q.InsertAgentManager(ctx, gen.InsertAgentManagerParams{ProjectID: string(projectID), CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
		} else {
			changed, err := q.AdvanceAgentManagerConfiguration(ctx, gen.AdvanceAgentManagerConfigurationParams{ProjectID: string(projectID), ExpectedRevision: mutation.ExpectedRevision, UpdatedAt: now})
			if err != nil {
				return err
			}
			if changed != 1 {
				return ports.ErrAgentManagerConflict
			}
		}
		encoded, err := json.Marshal(configuration)
		if err != nil {
			return err
		}
		if err := q.InsertAgentManagerConfiguration(ctx, gen.InsertAgentManagerConfigurationParams{ProjectID: string(projectID), Number: configuration.Number, AgentTypeID: entry.ID, AgentTypeVersion: version.Number, Snapshot: string(encoded), ContentHash: configuration.ContentHash, CreatedAt: now}); err != nil {
			return err
		}
		actor, err := json.Marshal(mutation.Actor)
		if err != nil {
			return err
		}
		action := "reconfigured"
		if missing {
			action = "configured"
		}
		return q.InsertAgentManagerAudit(ctx, gen.InsertAgentManagerAuditParams{ProjectID: string(projectID), ConfigurationVersion: configuration.Number, Action: action, Actor: string(actor), Reason: mutation.Reason, CreatedAt: now})
	})
	if err != nil {
		return domain.AgentManagerConfiguration{}, err
	}
	return configuration, nil
}

// GetAgentManager reads the exact current desired version without spawning.
func (s *Store) GetAgentManager(ctx context.Context, projectID domain.ProjectID) (domain.AgentManagerConfiguration, error) {
	if err := validateAgentManagerProject(projectID); err != nil {
		return domain.AgentManagerConfiguration{}, err
	}
	row, err := s.qr.GetAgentManager(ctx, string(projectID))
	if err != nil {
		return domain.AgentManagerConfiguration{}, agentManagerReadError(err)
	}
	return agentManagerConfigurationFromRow(row)
}

// GetAgentManagerConfiguration reads retained history, even after Type disable.
func (s *Store) GetAgentManagerConfiguration(ctx context.Context, projectID domain.ProjectID, number int64) (domain.AgentManagerConfiguration, error) {
	if err := validateAgentManagerProject(projectID); err != nil {
		return domain.AgentManagerConfiguration{}, err
	}
	if number < 1 || number > 1000 {
		return domain.AgentManagerConfiguration{}, ports.ErrAgentManagerInvalid
	}
	row, err := s.qr.GetAgentManagerConfiguration(ctx, gen.GetAgentManagerConfigurationParams{ProjectID: string(projectID), Number: number})
	if err != nil {
		return domain.AgentManagerConfiguration{}, agentManagerReadError(err)
	}
	return agentManagerConfigurationFromRow(row)
}

func (s *Store) validateAgentManagerPage(ctx context.Context, projectID domain.ProjectID, after int64, limit int) error {
	if err := validateAgentManagerProject(projectID); err != nil {
		return err
	}
	if after < 0 || limit < 1 || limit > 100 {
		return ports.ErrAgentManagerInvalid
	}
	_, err := s.qr.GetAgentManagerRecord(ctx, string(projectID))
	return agentManagerReadError(err)
}

// ListAgentManagerConfigurations pages immutable user governance history.
func (s *Store) ListAgentManagerConfigurations(ctx context.Context, projectID domain.ProjectID, after int64, limit int) ([]domain.AgentManagerConfiguration, error) {
	if err := s.validateAgentManagerPage(ctx, projectID, after, limit); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListAgentManagerConfigurations(ctx, gen.ListAgentManagerConfigurationsParams{ProjectID: string(projectID), Number: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]domain.AgentManagerConfiguration, 0, len(rows))
	for _, row := range rows {
		item, err := agentManagerConfigurationFromRow(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// ListAgentManagerAudit retains policy provenance independently of CDC pruning.
func (s *Store) ListAgentManagerAudit(ctx context.Context, projectID domain.ProjectID, after int64, limit int) ([]domain.AgentManagerAudit, error) {
	if err := s.validateAgentManagerPage(ctx, projectID, after, limit); err != nil {
		return nil, err
	}
	rows, err := s.qr.ListAgentManagerAudit(ctx, gen.ListAgentManagerAuditParams{ProjectID: string(projectID), Seq: after, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]domain.AgentManagerAudit, 0, len(rows))
	for _, row := range rows {
		item := domain.AgentManagerAudit{Sequence: row.Seq, ProjectID: domain.ProjectID(row.ProjectID), ConfigurationVersion: row.ConfigurationVersion, Action: row.Action, Reason: row.Reason, CreatedAt: row.CreatedAt}
		if err := json.Unmarshal([]byte(row.Actor), &item.Actor); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
