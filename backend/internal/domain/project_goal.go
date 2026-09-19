package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ProjectGoalVersion is one immutable wording of the project's high-level goal.
// The user authors goals; the orchestrator consumes, plans against and completes
// them but never rewrites user intent.
type ProjectGoalVersion struct {
	ProjectID   ProjectID     `json:"projectId"`
	Number      int64         `json:"number"`
	Goal        string        `json:"goal"`
	Actor       AdaptiveActor `json:"actor"`
	Reason      string        `json:"reason"`
	ContentHash string        `json:"contentHash"`
	CreatedAt   time.Time     `json:"createdAt"`
}

// ProjectGoalEvidenceTask seals the terminal fact one completion relied on.
type ProjectGoalEvidenceTask struct {
	TaskID       string `json:"taskId"`
	Revision     int64  `json:"revision"`
	State        string `json:"state" enum:"completed,cancelled"`
	ResultID     string `json:"resultId,omitempty"`
	EvaluationID string `json:"evaluationId,omitempty"`
	FactHash     string `json:"factHash"`
}

// ProjectGoalEvidence is the sealed fact set a completion was verified against.
// Tasks that later change do not rewrite this retained history.
type ProjectGoalEvidence struct {
	ProjectID     ProjectID                 `json:"projectId"`
	GoalVersion   int64                     `json:"goalVersion"`
	GoalHash      string                    `json:"goalHash"`
	VerifiedTasks []ProjectGoalEvidenceTask `json:"verifiedTasks"`
	Blockers      []ProjectGoalBlocker      `json:"blockers,omitempty"`
}

// ProjectGoalBlocker names one durable reason completion was refused.
type ProjectGoalBlocker struct {
	TaskID   string `json:"taskId"`
	Revision int64  `json:"revision"`
	State    string `json:"state" enum:"pending,working,blocked,failed,cancelling"`
	Reason   string `json:"reason"`
}

// ProjectGoalCompletion is a retained decision that one goal version was
// satisfied against the project's durable task facts.
type ProjectGoalCompletion struct {
	ID          string              `json:"id"`
	ProjectID   ProjectID           `json:"projectId"`
	GoalVersion int64               `json:"goalVersion"`
	Summary     string              `json:"summary"`
	Evidence    ProjectGoalEvidence `json:"evidence"`
	Actor       AdaptiveActor       `json:"actor"`
	Reason      string              `json:"reason"`
	CreatedAt   time.Time           `json:"createdAt"`
}

// goalText bounds the human-authored goal wording.
func goalText(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(value) > 32768 || strings.ContainsRune(value, 0) {
		return fmt.Errorf("goal text must be 1 to 32768 bytes without NUL")
	}
	return nil
}

func goalReason(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > 2000 || strings.ContainsRune(value, 0) {
		return fmt.Errorf("a bounded non-empty reason is required")
	}
	return nil
}

// ValidateGoalAuthoring restricts goal wording to user or system authority.
func ValidateGoalAuthoring(actor AdaptiveActor, goal, reason string) error {
	if actor.Kind != "USER" && actor.Kind != "SYSTEM" {
		return fmt.Errorf("only the user or system can set the project goal")
	}
	if strings.TrimSpace(actor.ID) == "" || len(actor.ID) > 200 || len(actor.SessionID) > 200 {
		return fmt.Errorf("invalid goal authoring provenance")
	}
	if err := goalText(goal); err != nil {
		return err
	}
	return goalReason(reason)
}

// ValidateGoalCompletion accepts user, system or the orchestrator session that
// assessed the work; the deterministic fact check remains the real gate.
func ValidateGoalCompletion(actor AdaptiveActor, summary, reason string) error {
	if actor.Kind != "USER" && actor.Kind != "SYSTEM" && actor.Kind != "ORCHESTRATOR" {
		return fmt.Errorf("only the user, system or orchestrator can assess goal completion")
	}
	if strings.TrimSpace(actor.ID) == "" || len(actor.ID) > 200 || len(actor.SessionID) > 200 {
		return fmt.Errorf("invalid goal completion provenance")
	}
	if actor.Kind == "ORCHESTRATOR" && actor.SessionID == "" {
		return fmt.Errorf("orchestrator completion requires its session identity")
	}
	if strings.TrimSpace(summary) == "" || len(summary) > 8000 || strings.ContainsRune(summary, 0) {
		return fmt.Errorf("a bounded non-empty completion summary is required")
	}
	return goalReason(reason)
}

// GoalContent hashes a goal version the way it is stored.
func GoalContent(goal ProjectGoalVersion) (string, error) {
	data, err := json.Marshal(struct {
		ProjectID ProjectID     `json:"projectId"`
		Number    int64         `json:"number"`
		Goal      string        `json:"goal"`
		Actor     AdaptiveActor `json:"actor"`
		Reason    string        `json:"reason"`
	}{goal.ProjectID, goal.Number, goal.Goal, goal.Actor, goal.Reason})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Validate seals one evidence snapshot: a completion retains only verified or
// cancelled tasks and never carries blockers.
func (e ProjectGoalEvidence) Validate(project ProjectID, goalVersion int64, goalHash string) error {
	if e.ProjectID != project || e.GoalVersion != goalVersion || e.GoalHash != goalHash || len(e.GoalHash) != 64 {
		return fmt.Errorf("evidence must match the completed goal version")
	}
	if len(e.Blockers) > 0 {
		return fmt.Errorf("retained completion evidence cannot contain blockers")
	}
	if len(e.VerifiedTasks) > 10000 {
		return fmt.Errorf("evidence exceeds the task enumeration bound")
	}
	seen := map[string]bool{}
	for _, task := range e.VerifiedTasks {
		if strings.TrimSpace(task.TaskID) == "" || len(task.TaskID) > 200 || seen[task.TaskID] || task.Revision < 1 || len(task.FactHash) != 64 {
			return fmt.Errorf("invalid or duplicate evidence task")
		}
		seen[task.TaskID] = true
		if task.State != "completed" && task.State != "cancelled" {
			return fmt.Errorf("evidence task state must be terminal")
		}
		if task.State == "cancelled" && (task.ResultID != "" || task.EvaluationID != "") {
			return fmt.Errorf("cancelled evidence carries no result or evaluation")
		}
	}
	return nil
}

// ProjectGoalBlockers reports why completion was refused. It is derived at
// decision time and never retained as completion evidence.
type ProjectGoalBlockers struct {
	ProjectID   ProjectID            `json:"projectId"`
	GoalVersion int64                `json:"goalVersion"`
	Items       []ProjectGoalBlocker `json:"blockers"`
}

func (b ProjectGoalBlockers) Error() string {
	if len(b.Items) == 0 {
		return "project goal is not satisfied"
	}
	return fmt.Sprintf("project goal is not satisfied: %s %s", b.Items[0].TaskID, b.Items[0].Reason)
}

// ProjectGoalCompletionRequest asks the store to verify and retain completion
// of the current goal version against the project's durable task facts.
type ProjectGoalCompletionRequest struct {
	ID          string
	ProjectID   ProjectID
	GoalVersion int64
	Summary     string
	Actor       AdaptiveActor
	Reason      string
	Now         time.Time
}

// Validate bounds a completion submission before any fact verification runs.
func (r ProjectGoalCompletionRequest) Validate() error {
	if r.Now.IsZero() {
		return fmt.Errorf("completion requires a timestamp")
	}
	if strings.TrimSpace(r.ID) == "" || len(r.ID) > 200 || strings.ContainsRune(r.ID, 0) || r.ProjectID == "" || r.GoalVersion < 1 {
		return fmt.Errorf("invalid completion identity")
	}
	if err := ValidateGoalCompletion(r.Actor, r.Summary, r.Reason); err != nil {
		return err
	}
	return nil
}
