package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// managerRequestNativeOwner is the shared transaction guard for incoming native
// proposals and outgoing context seals. Unknown ownership never grants access.
func managerRequestNativeOwner(ctx context.Context, q *gen.Queries, request domain.AgentManagerRequest, sessionID domain.SessionID, sourceOwner domain.SessionControllerOwner, now time.Time) (domain.AgentManagerConfiguration, gen.AdaptiveAgentManagerController, domain.WorkerConfiguration, error) {
	var configuration domain.AgentManagerConfiguration
	var controller gen.AdaptiveAgentManagerController
	var snapshot domain.WorkerConfiguration
	var err error
	if _, err := q.GetAgentManagerRequestResolution(ctx, request.ID); err == nil {
		return configuration, controller, snapshot, ports.ErrAgentManagerFenced
	} else if !errors.Is(err, sql.ErrNoRows) {
		return configuration, controller, snapshot, err
	}
	configuration, err = enabledManagerConfiguration(ctx, q, request.ProjectID)
	if err != nil {
		return configuration, controller, snapshot, err
	}
	if configuration.Number != request.ConfigurationVersion || configuration.ContentHash != request.ConfigurationHash {
		return configuration, controller, snapshot, ports.ErrAgentManagerFenced
	}
	task, err := q.GetAdaptiveTask(ctx, request.TaskID)
	if err != nil {
		return configuration, controller, snapshot, err
	}
	if task.Revision != request.TaskRevision {
		return configuration, controller, snapshot, ports.ErrAgentManagerFenced
	}
	if err := requireTaskRunIntent(ctx, q, task.ID); err != nil {
		return configuration, controller, snapshot, err
	}
	dispatch, err := q.GetAgentManagerDispatchBySession(ctx, string(sessionID))
	if err != nil {
		return configuration, controller, snapshot, agentManagerReadError(err)
	}
	controller, err = q.GetAgentManagerController(ctx, dispatch.ControllerID)
	if err != nil {
		return configuration, controller, snapshot, err
	}
	if controller.ProjectID != string(request.ProjectID) || controller.ConfigurationVersion != request.ConfigurationVersion || controller.ReleasedAt.Valid {
		return configuration, controller, snapshot, ports.ErrAgentManagerFenced
	}
	if _, err := q.PendingAgentManagerControllerExecution(ctx, controller.ID); err == nil {
		return configuration, controller, snapshot, ports.ErrAgentManagerFenced
	} else if !errors.Is(err, sql.ErrNoRows) {
		return configuration, controller, snapshot, err
	}
	row, err := q.GetSession(ctx, sessionID)
	if err != nil {
		return configuration, controller, snapshot, err
	}
	rec := rowToRecord(row)
	generation := resultGeneration(sourceOwner)
	if rec.Kind != domain.KindAgentManager || rec.ProjectID != request.ProjectID || rec.ControllerOwner() != sourceOwner || rec.IsTerminated {
		return configuration, controller, snapshot, ports.ErrAgentManagerFenced
	}
	connectedAt, err := q.AgentManagerConnectedGeneration(ctx, gen.AgentManagerConnectedGenerationParams{ControllerID: controller.ID, SessionID: string(sessionID), Mode: string(sourceOwner.Mode), Harness: string(sourceOwner.Harness), Generation: generation})
	if errors.Is(err, sql.ErrNoRows) {
		return configuration, controller, snapshot, ports.ErrAgentManagerFenced
	}
	if err != nil {
		return configuration, controller, snapshot, err
	}
	if now.Before(request.CreatedAt) || now.Before(connectedAt) {
		return configuration, controller, snapshot, ports.ErrAgentManagerInvalid
	}
	snapshotRow, err := q.GetWorkerConfiguration(ctx, string(sessionID))
	if err != nil {
		return configuration, controller, snapshot, err
	}
	snapshot, err = workerConfigurationFromRow(snapshotRow)
	if err != nil {
		return configuration, controller, snapshot, err
	}
	if snapshot.ContentHash != dispatch.ConfigurationHash || snapshot.AgentType.ID != configuration.ControllerType.ID || snapshot.AgentType.Version != configuration.ControllerType.Version || snapshot.AgentType.ContentHash != configuration.ControllerType.ContentHash {
		return configuration, controller, snapshot, ports.ErrAgentManagerFenced
	}

	return configuration, controller, snapshot, nil
}

// managerReceivedContext ties incoming native actions to an actual delivery and
// preserves the cumulative conversation class, including across generations.
func managerReceivedContext(ctx context.Context, q *gen.Queries, request domain.AgentManagerRequest, controllerID string, sessionID domain.SessionID, generation, configurationHash string, now time.Time) (domain.AgentManagerContext, domain.AgentManagerContext, error) {
	delivery, err := q.LatestAgentManagerRequestDelivery(ctx, request.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentManagerContext{}, domain.AgentManagerContext{}, ports.ErrAgentManagerFenced
	}
	if err != nil {
		return domain.AgentManagerContext{}, domain.AgentManagerContext{}, err
	}
	if delivery.State == "not_sent" || delivery.ControllerID != controllerID || delivery.SessionID != string(sessionID) {
		return domain.AgentManagerContext{}, domain.AgentManagerContext{}, ports.ErrAgentManagerFenced
	}
	contextRow, err := q.GetAgentManagerContext(ctx, delivery.ContextID)
	if err != nil {
		return domain.AgentManagerContext{}, domain.AgentManagerContext{}, err
	}
	sealed, err := managerContextFromRow(contextRow)
	if err != nil {
		return domain.AgentManagerContext{}, domain.AgentManagerContext{}, err
	}
	if sealed.NativeGeneration != generation || sealed.ConfigurationHash != configurationHash || sealed.RequestHash != request.ContentHash || now.Before(sealed.CreatedAt) {
		return domain.AgentManagerContext{}, domain.AgentManagerContext{}, ports.ErrAgentManagerFenced
	}
	conversationRow, err := q.LatestAgentManagerContext(ctx, controllerID)
	if err != nil {
		return domain.AgentManagerContext{}, domain.AgentManagerContext{}, err
	}
	conversation, err := managerContextFromRow(conversationRow)
	if err != nil {
		return domain.AgentManagerContext{}, domain.AgentManagerContext{}, err
	}
	if now.Before(conversation.CreatedAt) {
		return domain.AgentManagerContext{}, domain.AgentManagerContext{}, ports.ErrAgentManagerInvalid
	}
	return sealed, conversation, nil
}
