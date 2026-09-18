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
