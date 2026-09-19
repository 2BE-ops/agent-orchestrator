package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// dryRunSyntheticPrefix namespaces simulated create_task nodes inside the
// graph overlay. It can never collide with minted task identity.
const dryRunSyntheticPrefix = "\x00dry-run-"

var _ ports.DryRunStore = (*Store)(nil)

// DryRunPlan simulates one autonomous plan against current durable state.
// Every validation, selection and admission check execution would apply runs
// here with writes and process launches disabled: the reader never takes the
// write lock, no worktree is created, no harness is installed, the repository
// is untouched, and registry/task state never changes.
func (s *Store) DryRunPlan(ctx context.Context, projectID domain.ProjectID, request domain.DryRunRequest) (domain.DryRunVerdict, error) {
	if err := request.Validate(); err != nil {
		return domain.DryRunVerdict{}, fmt.Errorf("%w: %w", ports.ErrDryRunInvalid, err)
	}
	if _, err := s.qr.GetProject(ctx, projectID); err != nil {
		return domain.DryRunVerdict{}, projectControlReadError(err)
	}
	// One read transaction holds a single consistent snapshot for the whole
	// simulation; a concurrent writer cannot split the rehearsal in half.
	var verdict domain.DryRunVerdict
	err := s.readTx(ctx, "dry run plan", func(q *gen.Queries) error {
		nodes, err := q.AdaptiveTaskGraphState(ctx, string(projectID))
		if err != nil {
			return err
		}
		parents := make(map[string]string, len(nodes)+len(request.Actions))
		revisions := make(map[string]int64, len(nodes))
		for _, node := range nodes {
			parents[node.ID] = node.ParentID.String
			revisions[node.ID] = node.Revision
		}
		edges, err := q.AdaptiveTaskGraphEdges(ctx, string(projectID))
		if err != nil {
			return err
		}
		dependencies := make(map[string][]string, len(parents))
		for _, edge := range edges {
			dependencies[edge.TaskID] = append(dependencies[edge.TaskID], edge.DependencyID)
		}
		verdict = domain.DryRunVerdict{ProjectID: projectID, CostEstimate: domain.DryRunCostUnknown, SimulatedAt: time.Now().UTC()}
		verdict.Actions = make([]domain.DryRunActionVerdict, 0, len(request.Actions))
		if err := applyDryRunOverlay(ctx, q, request, parents, dependencies, revisions, &verdict); err != nil {
			return err
		}
		verdict.GraphValid = true
		if err := validateTaskGraphMaps(parents, dependencies); err != nil {
			// The combined effect must validate: a plan whose overlay cycles
			// or deepens the hierarchy past its bounds would be refused at
			// execution, and every action inherits that refusal.
			verdict.GraphValid = false
			for i := range verdict.Actions {
				verdict.Actions[i].Findings = append(verdict.Actions[i].Findings, "combined plan graph is invalid: "+err.Error())
			}
		}
		for i := range verdict.Actions {
			verdict.Actions[i].WouldApply = verdict.GraphValid && len(verdict.Actions[i].Findings) == 0
		}
		return collectDryRunAdmission(ctx, q, projectID, &verdict)
	})
	if err != nil {
		return domain.DryRunVerdict{}, err
	}
	return verdict, nil
}

// applyDryRunOverlay walks the actions sequentially, mutating the in-memory
// graph exactly as execution would and recording per-action findings (which
// block the action) and notes (which never do).
func applyDryRunOverlay(ctx context.Context, q *gen.Queries, request domain.DryRunRequest, parents map[string]string, dependencies map[string][]string, revisions map[string]int64, verdict *domain.DryRunVerdict) error {
	criteriaFrozen := map[string]int64{}
	for index, action := range request.Actions {
		item := domain.DryRunActionVerdict{Index: index, Action: action.Action, Findings: []string{}, Notes: []string{}}
		switch action.Action {
		case "create_task":
			synthetic := fmt.Sprintf("%s%d", dryRunSyntheticPrefix, index)
			item.TaskID = "(minted on execution)"
			item.Notes = append(item.Notes, "task identity is minted by AO at execution")
			if len(parents) >= 1000 {
				item.Findings = append(item.Findings, "project task limit is 1000")
			}
			parents[synthetic] = action.Definition.ParentID
			dependencies[synthetic] = action.Definition.Dependencies
			revisions[synthetic] = 1
		case "revise_task":
			item.TaskID = action.TaskID
			current, exists := revisions[action.TaskID]
			if !exists {
				item.Findings = append(item.Findings, "task does not exist in this project")
			} else {
				if current != action.ExpectedRevision {
					item.Findings = append(item.Findings, fmt.Sprintf("expected revision %d but the task is at revision %d", action.ExpectedRevision, current))
				} else {
					// Sequential semantics: a clean revise advances the
					// revision the next action in this plan would fence on.
					revisions[action.TaskID] = current + 1
				}
				parents[action.TaskID] = action.Definition.ParentID
				dependencies[action.TaskID] = action.Definition.Dependencies
			}
		case "freeze_criteria":
			item.TaskID = action.TaskID
			current, exists := revisions[action.TaskID]
			if !exists {
				item.Findings = append(item.Findings, "task does not exist in this project")
				break
			}
			if current != action.ExpectedRevision {
				item.Findings = append(item.Findings, fmt.Sprintf("expected revision %d but the task is at revision %d", action.ExpectedRevision, current))
			}
			if version := criteriaFrozen[action.TaskID]; version > 0 {
				item.Notes = append(item.Notes, fmt.Sprintf("criteria already frozen at version %d; execution would freeze another version", version))
			}
			row, err := q.GetAdaptiveTaskRevision(ctx, gen.GetAdaptiveTaskRevisionParams{TaskID: action.TaskID, Number: current})
			switch {
			case errors.Is(err, sql.ErrNoRows):
				// The plan's own earlier revise advanced the overlay past the
				// durable tip; execution would carry the criteria reference
				// forward, so no durable freeze state applies here.
			case err != nil:
				return err
			default:
				criteriaFrozen[action.TaskID] = row.CriteriaVersion.Int64
			}
		}
		verdict.Actions = append(verdict.Actions, item)
		if action.Definition != nil {
			selections := len(verdict.Selections)
			if err := collectDryRunSelection(ctx, q, index, *action.Definition, verdict); err != nil {
				return err
			}
			// A requested worker that does not resolve blocks the action's
			// clean applicability, not just the selection report.
			if selections < len(verdict.Selections) {
				selection := verdict.Selections[selections]
				if !selection.Resolves {
					verdict.Actions[index].Findings = append(verdict.Actions[index].Findings, "requested worker "+selection.AgentTypeID+" does not resolve: "+strings.Join(selection.Findings, "; "))
				}
			}
		}
	}
	return nil
}

// collectDryRunSelection resolves one proposed task's requested worker
// against the immutable registry: the entry must exist and be enabled, and
// the pinned version must exist. Version zero reports the active version.
func collectDryRunSelection(ctx context.Context, q *gen.Queries, index int, definition domain.TaskDefinition, verdict *domain.DryRunVerdict) error {
	if definition.RequestedWorker == nil {
		return nil
	}
	ref := definition.RequestedWorker
	item := domain.DryRunSelectionVerdict{ActionIndex: index, AgentTypeID: ref.AgentTypeID, RequestedVersion: ref.Version, Findings: []string{}}
	entry, err := q.GetRegistryEntry(ctx, ref.AgentTypeID)
	if errors.Is(err, sql.ErrNoRows) {
		item.Findings = append(item.Findings, "agent type does not exist in the registry")
		verdict.Selections = append(verdict.Selections, item)
		return nil
	}
	if err != nil {
		return err
	}
	item.ActiveVersion = entry.ActiveVersion
	if entry.Enabled == 0 {
		item.Findings = append(item.Findings, "agent type is disabled")
	}
	if ref.Version == 0 {
		item.RequestedVersion = entry.ActiveVersion
		item.Notes = append(item.Notes, fmt.Sprintf("version zero resolves to the active version %d at execution time", entry.ActiveVersion))
	}
	if _, err := q.GetRegistryVersion(ctx, gen.GetRegistryVersionParams{EntryID: ref.AgentTypeID, Number: item.RequestedVersion}); errors.Is(err, sql.ErrNoRows) {
		item.Findings = append(item.Findings, fmt.Sprintf("version %d does not exist", item.RequestedVersion))
		verdict.Selections = append(verdict.Selections, item)
		return nil
	} else if err != nil {
		return err
	}
	if item.RequestedVersion != entry.ActiveVersion {
		item.Findings = append(item.Findings, fmt.Sprintf("pins an inactive version (active is %d)", entry.ActiveVersion))
	} else {
		item.PinsActive = true
	}
	item.Resolves = entry.Enabled != 0
	verdict.Selections = append(verdict.Selections, item)
	return nil
}

// collectDryRunAdmission reports the deterministic admission facts the plan
// would face: control state, live worker occupancy and daemon-wide headroom.
func collectDryRunAdmission(ctx context.Context, q *gen.Queries, projectID domain.ProjectID, verdict *domain.DryRunVerdict) error {
	admission := domain.DryRunAdmission{ControlState: string(domain.ProjectRunning)}
	control, err := q.GetProjectControl(ctx, string(projectID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		admission.ControlState = control.State
	}
	limit, err := schedulerWorkerCap(ctx, q)
	if err != nil {
		return err
	}
	active, err := q.CountActiveWorkerSessions(ctx)
	if err != nil {
		return err
	}
	admission.MaxConcurrentWorkers = limit
	admission.ActiveWorkers = active
	admission.Headroom = int64(limit) - active
	admission.WouldAdmitDispatch = admission.Headroom > 0 && admission.ControlState == string(domain.ProjectRunning)
	verdict.Admission = admission
	return nil
}
