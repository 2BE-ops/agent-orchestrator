package ports

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// WorkerConfigurationStore commits a frozen configuration with its session seed.
// Reading a legacy session returns ok=false; a failed read never means legacy.
type WorkerConfigurationStore interface {
	CreateConfiguredSession(context.Context, domain.SessionRecord, domain.WorkerConfiguration) (domain.SessionRecord, error)
	GetWorkerConfiguration(context.Context, domain.SessionID) (domain.WorkerConfiguration, bool, error)
}

// WorkerConfigurationResolver supplies launch facts through the shared registry
// service, with a separate check for fresh restoration of historical content.
type WorkerConfigurationResolver interface {
	ResolveWorker(context.Context, domain.WorkerSelection, domain.ProjectRecord, domain.SessionMode, domain.RegistryActor) (domain.WorkerConfiguration, error)
	ResolveWorkerChange(context.Context, domain.WorkerConfiguration, domain.WorkerOverrides, domain.ProjectRecord) (domain.WorkerConfiguration, error)
	ValidateWorkerRestore(context.Context, domain.WorkerConfiguration, string) error
}

// WorkerExecutionStore separates immutable preparation from atomic ownership
// activation. A zero sequence denotes the original launch configuration.
type WorkerExecutionStore interface {
	PrepareWorkerExecution(context.Context, domain.SessionControllerOwner, domain.WorkerExecution) (domain.WorkerExecution, error)
	GetEffectiveWorkerConfiguration(context.Context, domain.SessionID) (domain.WorkerConfiguration, int64, bool, error)
	ListWorkerExecutions(context.Context, domain.SessionID, int64, int) ([]domain.WorkerExecutionActivation, error)
	GetWorkerExecution(context.Context, domain.SessionID, string) (domain.WorkerExecution, error)
}

// WorkerConversationSettingsStore commits native turn preferences and their
// execution attribution together, fencing both controller and configuration.
type WorkerConversationSettingsStore interface {
	CommitWorkerConversationSettings(context.Context, domain.SessionControllerOwner, string, domain.ConversationSettings, domain.WorkerExecution) error
}

// WorkerNativeChangeStore fences provider side effects with durable intent and
// immutable resolutions. Applied resolution shares the settings transaction.
type WorkerNativeChangeStore interface {
	BeginWorkerNativeChange(context.Context, domain.WorkerNativeChange) error
	PendingWorkerNativeChange(context.Context, domain.SessionID) (domain.WorkerNativeChange, bool, error)
	RevertWorkerNativeChange(context.Context, domain.SessionControllerOwner, string, string) error
}
