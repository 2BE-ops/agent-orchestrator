package store

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// Validate and replace edges inside the revision transaction, so concurrent
// individually-valid edits cannot commit a cycle together.
func validateAdaptiveTaskGraph(ctx context.Context, q *gen.Queries, projectID, taskID string, definition domain.TaskDefinition) error {
	nodes, err := q.AdaptiveTaskGraphNodes(ctx, projectID)
	if err != nil {
		return err
	}
	parents := make(map[string]string, len(nodes)+1)
	for _, node := range nodes {
		parents[node.ID] = node.ParentID.String
	}
	if _, exists := parents[taskID]; !exists && len(parents) >= 1000 {
		return fmt.Errorf("%w: project task limit is 1000", ports.ErrTaskInvalid)
	}
	parents[taskID] = definition.ParentID
	for id := range parents {
		seen := map[string]bool{}
		for ancestor := id; ancestor != ""; ancestor = parents[ancestor] {
			if _, exists := parents[ancestor]; !exists {
				return fmt.Errorf("%w: parent must belong to the same project", ports.ErrTaskInvalid)
			}
			if seen[ancestor] {
				return fmt.Errorf("%w: parent hierarchy contains a cycle", ports.ErrTaskInvalid)
			}
			seen[ancestor] = true
			if len(seen) > 8 {
				return fmt.Errorf("%w: task hierarchy exceeds 8 levels", ports.ErrTaskInvalid)
			}
		}
	}
	edges, err := q.AdaptiveTaskGraphEdges(ctx, projectID)
	if err != nil {
		return err
	}
	dependencies := make(map[string][]string, len(parents))
	for _, edge := range edges {
		if edge.TaskID != taskID {
			dependencies[edge.TaskID] = append(dependencies[edge.TaskID], edge.DependencyID)
		}
	}
	for _, dependency := range definition.Dependencies {
		if _, exists := parents[dependency]; !exists || dependency == taskID {
			return fmt.Errorf("%w: dependencies must name other tasks in the same project", ports.ErrTaskInvalid)
		}
	}
	dependencies[taskID] = definition.Dependencies
	color := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		if color[id] == 1 {
			return false
		}
		if color[id] == 2 {
			return true
		}
		color[id] = 1
		for _, dependency := range dependencies[id] {
			if !visit(dependency) {
				return false
			}
		}
		color[id] = 2
		return true
	}
	for id := range parents {
		if !visit(id) {
			return fmt.Errorf("%w: dependency graph contains a cycle", ports.ErrTaskInvalid)
		}
	}
	return nil
}
