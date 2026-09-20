package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// Validate pins within the planning transaction. Historical reads deliberately
// do not revalidate live metadata: disabling a Type must not rewrite criteria.
func validateTaskReviewPolicy(ctx context.Context, q *gen.Queries, policy *domain.TaskReviewPolicy) error {
	if policy == nil {
		return nil
	}
	entry, err := q.GetRegistryEntry(ctx, policy.AgentTypeID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: reviewer Agent Type does not exist", ports.ErrTaskInvalid)
	}
	if err != nil {
		return err
	}
	if entry.Kind != string(domain.RegistryAgentType) || entry.Enabled == 0 {
		return fmt.Errorf("%w: reviewer must be an enabled Agent Type", ports.ErrTaskInvalid)
	}
	row, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: policy.AgentTypeID, Number: policy.Version})
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: exact reviewer Type version does not exist", ports.ErrTaskInvalid)
	}
	if err != nil {
		return err
	}
	version, err := registryVersionFromGen(row)
	if err != nil {
		return err
	}
	if version.Definition.AgentType == nil {
		return fmt.Errorf("%w: reviewer version is not an Agent Type", ports.ErrTaskInvalid)
	}
	return nil
}
