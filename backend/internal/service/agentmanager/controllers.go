package agentmanager

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// NativeRuntime is the existing session manager's launch boundary. Manager
// services do not create processes, conversations or workspaces themselves.
type NativeRuntime interface {
	Spawn(context.Context, ports.SpawnConfig) (domain.SessionRecord, int, int, error)
}

// ControllerStartInput contains a durable retry identity and exact governance
// version. It has no role, Type override, actor, native mode or launch authority.
type ControllerStartInput struct {
	ID                   string `json:"id"`
	ConfigurationVersion int64  `json:"configurationVersion"`
	Reason               string `json:"reason"`
}

// ControllerState reports retained admission/dispatch facts. Pending operation
// absence is not a claim that the native process is alive, stopped or ready.
type ControllerState struct {
	Controller       domain.AgentManagerController          `json:"controller"`
	Dispatch         *domain.AgentManagerControllerDispatch `json:"dispatch" nullable:"true"`
	PendingOperation *domain.AgentManagerExecutionOperation `json:"pendingOperation" nullable:"true"`
}

// ControllerStartReceipt distinguishes a new admission from inspection on retry.
type ControllerStartReceipt struct {
	State   ControllerState `json:"state"`
	Created bool            `json:"created"`
}

func mapControllerError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrAgentManagerNotFound):
		return apierr.NotFound("AGENT_MANAGER_CONTROLLER_NOT_FOUND", "Manager controller or governing configuration was not found in this project")
	case errors.Is(err, ports.ErrAgentManagerConflict):
		return apierr.Conflict("AGENT_MANAGER_CONTROLLER_CONFLICT", "Manager governance or retry identity changed; inspect retained controller admission", nil)
	case errors.Is(err, ports.ErrAgentManagerFenced):
		return apierr.Conflict("AGENT_MANAGER_CONTROLLER_FENCED", "Manager ownership or governing policy prevents another native launch; inspect the current controller", nil)
	case errors.Is(err, ports.ErrAgentManagerForbidden), errors.Is(err, ports.ErrRegistryForbidden):
		return apierr.Forbidden("AGENT_MANAGER_CONTROLLER_FORBIDDEN", "Manager admission requires trusted user or daemon authority under user governance")
	case errors.Is(err, ports.ErrAgentManagerInvalid), errors.Is(err, ports.ErrRegistryInvalid):
		return apierr.Invalid("INVALID_AGENT_MANAGER_CONTROLLER", err.Error(), nil)
	default:
		return err
	}
}

func (m *Manager) controllerStore() (ports.AgentManagerControllerStore, error) {
	store, ok := m.store.(ports.AgentManagerControllerStore)
	if !ok {
		return nil, apierr.NotImplemented("AGENT_MANAGER_CONTROLLERS_UNAVAILABLE", "Manager controller persistence is unavailable")
	}
	return store, nil
}

// StartController reserves once, then calls the shared native engine once. Every
// retry inspects retained facts, including an unseeded or uncertain admission.
// Recovery must reconcile those facts before another launch is authorized.
func (m *Manager) StartController(ctx context.Context, actor domain.AdaptiveActor, project domain.ProjectID, input ControllerStartInput) (ControllerStartReceipt, error) {
	var empty ControllerStartReceipt
	if (actor.Kind != "USER" && actor.Kind != "SYSTEM") || actor.SessionID != "" {
		return empty, mapControllerError(ports.ErrAgentManagerForbidden)
	}
	reservation := domain.AgentManagerControllerReservation{AgentManagerControllerToken: domain.AgentManagerControllerToken{ID: input.ID, ProjectID: project, ConfigurationVersion: input.ConfigurationVersion}, Actor: actor, Reason: input.Reason, Now: time.Now().UTC()}
	if err := reservation.Validate(); err != nil {
		return empty, mapControllerError(ports.ErrAgentManagerInvalid)
	}
	store, err := m.controllerStore()
	if err != nil {
		return empty, err
	}
	if m.native == nil {
		return empty, apierr.NotImplemented("AGENT_MANAGER_NATIVE_UNAVAILABLE", "Native Manager execution is unavailable")
	}
	configuration, err := m.store.GetAgentManagerConfiguration(ctx, project, input.ConfigurationVersion)
	if err != nil {
		return empty, mapControllerError(err)
	}
	controller, created, err := store.ReserveAgentManagerController(ctx, reservation)
	if err != nil {
		return empty, mapControllerError(err)
	}
	if created {
		_, _, _, err = m.native.Spawn(ctx, ports.SpawnConfig{ProjectID: project, Kind: domain.KindAgentManager,
			Prompt:            "Wait for durable Agent Manager inbox work and use its versioned routing proposal protocol.",
			ManagerController: &controller.AgentManagerControllerToken,
			WorkerSelection:   &domain.WorkerSelection{AgentTypeID: configuration.ControllerType.ID, Version: configuration.ControllerType.Version},
			WorkerActor:       domain.RegistryActor{Origin: domain.RegistryUser, ID: configuration.Actor.ID}})
		if err != nil {
			return empty, mapControllerError(err)
		}
	}
	state, err := m.controllerState(ctx, store, controller)
	if err != nil {
		return empty, err
	}
	return ControllerStartReceipt{State: state, Created: created}, nil
}

func (m *Manager) controllerState(ctx context.Context, store ports.AgentManagerControllerStore, controller domain.AgentManagerController) (ControllerState, error) {
	state := ControllerState{Controller: controller}
	dispatch, found, err := store.GetAgentManagerDispatch(ctx, controller.ID)
	if err != nil {
		return state, mapControllerError(err)
	}
	if !found {
		return state, nil
	}
	state.Dispatch = &dispatch
	operation, pending, err := store.PendingAgentManagerExecution(ctx, dispatch.SessionID)
	if err != nil {
		return state, mapControllerError(err)
	}
	if pending {
		state.PendingOperation = &operation
	}
	return state, nil
}

// Controller is a project-scoped historical read, independent of live policy.
func (m *Manager) Controller(ctx context.Context, project domain.ProjectID, id string) (ControllerState, error) {
	if err := validateProject(project); err != nil {
		return ControllerState{}, err
	}
	if err := validateProject(domain.ProjectID(id)); err != nil {
		return ControllerState{}, err
	}
	store, err := m.controllerStore()
	if err != nil {
		return ControllerState{}, err
	}
	controller, err := store.GetAgentManagerController(ctx, id)
	if err != nil {
		return ControllerState{}, mapControllerError(err)
	}
	if controller.ProjectID != project {
		return ControllerState{}, mapControllerError(ports.ErrAgentManagerNotFound)
	}
	return m.controllerState(ctx, store, controller)
}

// CurrentController returns reserved ownership even when execution is uncertain.
func (m *Manager) CurrentController(ctx context.Context, project domain.ProjectID) (*ControllerState, error) {
	if err := validateProject(project); err != nil {
		return nil, err
	}
	store, err := m.controllerStore()
	if err != nil {
		return nil, err
	}
	controller, found, err := store.ActiveAgentManagerController(ctx, project)
	if err != nil {
		return nil, mapControllerError(err)
	}
	if !found {
		return nil, nil
	}
	state, err := m.controllerState(ctx, store, controller)
	if err != nil {
		return nil, err
	}
	return &state, nil
}
