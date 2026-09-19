// Package taskcontext builds bounded, reproducible input for reserved workers.
package taskcontext

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Store exposes the durable facts required to construct and atomically seal a
// launch context. Selection does not grant planning or launch authority.
type Store interface {
	ports.TaskContextStore
	ports.ContextKnowledgeStore
	ports.ContextArtifactStore
	GetTaskLease(context.Context, string) (domain.TaskLease, error)
	GetTaskAttempt(context.Context, string) (domain.TaskAttempt, error)
	GetTaskWorkerDispatch(context.Context, string) (domain.TaskWorkerDispatch, bool, error)
	PendingTaskExecution(context.Context, domain.SessionID) (domain.TaskExecutionOperation, bool, error)
	GetWorkerConfiguration(context.Context, domain.SessionID) (domain.WorkerConfiguration, bool, error)
	GetAdaptiveTask(context.Context, string) (domain.AdaptiveTask, error)
	GetTaskRevision(context.Context, string, int64) (domain.TaskRevision, error)
	GetAcceptanceCriteria(context.Context, string, int64) (domain.AcceptanceCriteriaVersion, error)
}

// Builder performs no native execution or network retrieval.
type Builder struct {
	store Store
	now   func() time.Time
}

var _ ports.TaskContextBuilder = (*Builder)(nil)

// New binds the shared context builder to persistent facts.
func New(store Store) *Builder { return &Builder{store: store, now: time.Now} }

// Build returns an existing sealed prompt or selects bounded sources under the
// launch reservation. SaveTaskContext rechecks authority and source acceptance
// transactionally, closing races with cancellation and knowledge invalidation.
func (b *Builder) Build(ctx context.Context, request ports.TaskContextRequest) (domain.TaskContextSnapshot, error) {
	var empty domain.TaskContextSnapshot
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	lease, err := b.store.GetTaskLease(ctx, request.Lease.AttemptID)
	if err != nil {
		return empty, err
	}
	if lease.TaskLeaseToken != request.Lease || lease.ReleasedAt != nil {
		return empty, ports.ErrTaskLeaseFenced
	}
	retained, found, err := b.store.GetTaskContext(ctx, request.Lease.AttemptID)
	if err != nil {
		return empty, err
	}
	if found {
		if retained.SessionID != request.SessionID || retained.ExecutionOperationID != request.ExecutionOperationID {
			return empty, ports.ErrTaskConflict
		}
		return retained, nil
	}
	if lease.NeedsReconciliation(b.now()) {
		return empty, ports.ErrTaskLeaseFenced
	}
	dispatch, found, err := b.store.GetTaskWorkerDispatch(ctx, lease.AttemptID)
	if err != nil {
		return empty, err
	}
	if !found || dispatch.SessionID != request.SessionID {
		return empty, ports.ErrTaskLeaseFenced
	}
	op, found, err := b.store.PendingTaskExecution(ctx, request.SessionID)
	if err != nil {
		return empty, err
	}
	if !found || op.ID != request.ExecutionOperationID || op.Kind != "dispatch" || op.Lease != request.Lease {
		return empty, ports.ErrTaskLeaseFenced
	}
	budget := request.Budget
	if budget == (domain.ContextBudget{}) {
		budget = domain.DefaultContextBudget()
	}
	if err := budget.Validate(); err != nil {
		return empty, err
	}
	if len(request.Prompt) > 64<<10 || len(request.SystemPrompt) > budget.MaxBytes {
		return empty, fmt.Errorf("task context instructions exceed the prompt budget")
	}
	attempt, err := b.store.GetTaskAttempt(ctx, lease.AttemptID)
	if err != nil {
		return empty, err
	}
	task, err := b.store.GetAdaptiveTask(ctx, attempt.TaskID)
	if err != nil {
		return empty, err
	}
	revision, err := b.store.GetTaskRevision(ctx, attempt.TaskID, attempt.TaskRevision)
	if err != nil {
		return empty, err
	}
	criteria, err := b.store.GetAcceptanceCriteria(ctx, attempt.TaskID, attempt.CriteriaVersion)
	if err != nil {
		return empty, err
	}
	config, found, err := b.store.GetWorkerConfiguration(ctx, request.SessionID)
	if err != nil {
		return empty, err
	}
	if !found || config.ContentHash != dispatch.ConfigurationHash {
		return empty, ports.ErrTaskConflict
	}
	snapshot := domain.TaskContextSnapshot{SchemaVersion: 1, AttemptID: attempt.ID, SessionID: request.SessionID,
		Task:            domain.TaskRevisionRef{TaskID: task.ID, Revision: revision.Number, ContentHash: revision.ContentHash},
		CriteriaVersion: criteria.Number, ConfigurationHash: config.ContentHash, ExecutionOperationID: op.ID,
		SystemPromptHash: domain.ContextTextHash(request.SystemPrompt), SystemPromptBytes: len(request.SystemPrompt),
		BasePrompt: request.Prompt, Budget: budget}
	selection := sourceSelection{snapshot: &snapshot}
	for _, source := range []domain.ContextSource{
		definitionSource("task", task.ID, revision.Number, revision.ContentHash, revision.Definition, "Frozen attempt task revision"),
		definitionSource("criteria", task.ID, criteria.Number, criteria.ContentHash, criteria.Definition, "Frozen acceptance criteria"),
		referenceSource("agent_type", config.AgentType),
	} {
		if err := selection.required(source); err != nil {
			return empty, err
		}
	}
	for _, skill := range config.Skills {
		if err := selection.required(referenceSource("skill", skill.Reference)); err != nil {
			return empty, err
		}
	}
	if err := b.relatedSources(ctx, &selection, task, revision, attempt, request.WorkspacePath); err != nil {
		return empty, err
	}
	if err := selection.finish(); err != nil {
		return empty, err
	}
	snapshot.CreatedAt = b.now().UTC()
	snapshot.ContentHash = snapshot.Hash()
	if err := snapshot.Validate(); err != nil {
		return empty, err
	}
	if err := b.store.SaveTaskContext(ctx, request.Lease, snapshot); err != nil {
		return empty, err
	}
	return snapshot, nil
}

func (b *Builder) relatedSources(ctx context.Context, selection *sourceSelection, task domain.AdaptiveTask, revision domain.TaskRevision, attempt domain.TaskAttempt, workspace string) error {
	taskIDs := []string{task.ID}
	if parentID := revision.Definition.ParentID; parentID != "" {
		parent, err := b.store.GetAdaptiveTask(ctx, parentID)
		if err != nil {
			return err
		}
		version, err := b.store.GetTaskRevision(ctx, parentID, parent.Revision)
		if err != nil {
			return err
		}
		selection.optional(definitionSource("parent", parentID, version.Number, version.ContentHash, version.Definition, "Parent planning at context construction"))
		taskIDs = append(taskIDs, parentID)
	}
	for _, dependency := range attempt.Dependencies {
		version, err := b.store.GetTaskRevision(ctx, dependency.TaskID, dependency.Revision)
		if err != nil {
			return err
		}
		if version.ContentHash != dependency.ContentHash {
			return ports.ErrTaskConflict
		}
		selection.optional(definitionSource("dependency", dependency.TaskID, dependency.Revision, dependency.ContentHash, version.Definition, "Dependency planning pinned by the attempt; not completion evidence"))
		taskIDs = append(taskIDs, dependency.TaskID)
	}
	if err := b.workerArtifactSources(ctx, selection, task, attempt); err != nil {
		return err
	}
	for _, path := range revision.Definition.ContextFiles {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !selection.room() {
			selection.skipped++
			continue
		}
		selection.optional(workspaceSource(workspace, path, selection.snapshot.Budget.MaxSourceBytes))
	}
	versions, err := b.store.SelectContextKnowledge(ctx, task.ProjectID, taskIDs, revision.Definition.Category, 33)
	if err != nil {
		return err
	}
	for i, version := range versions {
		if i == 32 {
			selection.optional(domain.ContextSource{Kind: "selection", ID: "knowledge-candidate-limit", Disposition: "omitted", Reason: "Additional accepted relevant knowledge omitted after the first 32 candidates"})
			break
		}
		reason := "Accepted general project knowledge"
		switch {
		case version.Definition.Pinned:
			reason = "Accepted knowledge explicitly pinned for project context"
		case intersects(version.Definition.TaskIDs, taskIDs):
			reason = "Accepted knowledge relevant to the task, parent or dependencies"
		case intersects(version.Definition.Tags, []string{revision.Definition.Category}):
			reason = "Accepted knowledge matching the task category"
		}
		selection.optional(definitionSource("knowledge", version.KnowledgeID, version.Number, version.ContentHash, version.Definition, reason))
	}
	return nil
}

func (b *Builder) workerArtifactSources(ctx context.Context, selection *sourceSelection, task domain.AdaptiveTask, attempt domain.TaskAttempt) error {
	refs := append([]domain.TaskRevisionRef{{TaskID: task.ID}}, attempt.Dependencies...)
	for _, ref := range refs {
		limit := 1
		reason := "Latest worker claims for a pinned dependency revision; not independently verified completion"
		if ref.TaskID == task.ID {
			limit = 4
			reason = "Latest correction from a previous attempt; historical worker findings, not verified evidence"
		}
		results, err := b.store.SelectTaskContextResults(ctx, ref.TaskID, ref.Revision, attempt.ID, limit)
		if err != nil {
			return err
		}
		for i, result := range results {
			if ref.TaskID == task.ID && i == 3 {
				selection.optional(domain.ContextSource{Kind: "selection", ID: "previous-result-candidate-limit", Disposition: "omitted", Reason: "Additional prior attempt findings omitted after the latest three attempts"})
				break
			}
			selection.optional(definitionSource("result", result.ID, result.Number, result.ContentHash, result.ContextFacts(), reason))
		}
	}
	contracts, err := b.store.SelectTaskContextInterfaces(ctx, task.ProjectID, task.ID, 9)
	if err != nil {
		return err
	}
	for i, message := range contracts {
		if i == 8 {
			selection.optional(domain.ContextSource{Kind: "selection", ID: "interface-candidate-limit", Disposition: "omitted", Reason: "Additional incoming interface contracts omitted after the latest eight"})
			break
		}
		selection.optional(definitionSource("interface_contract", message.ID, 1, message.ContentHash, message, "Historical incoming worker interface proposal; not an accepted agreement or delivery acknowledgement"))
	}
	return nil
}

func intersects(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

func definitionSource(kind, id string, version int64, hash string, definition any, reason string) domain.ContextSource {
	// All callers supply validated domain definitions containing only JSON values.
	content, _, _ := domain.TaskContent(definition)
	return domain.ContextSource{Kind: kind, ID: id, Version: version, SourceHash: hash, Content: string(content), ContentHash: domain.ContextTextHash(string(content)), Disposition: "inline", Reason: reason}
}

func referenceSource(kind string, ref domain.WorkerDefinitionRef) domain.ContextSource {
	return domain.ContextSource{Kind: kind, ID: ref.ID, Version: ref.Version, SourceHash: ref.ContentHash, Disposition: "reference", Reason: "Exact configured instructions and materialized resources in worker system context"}
}
