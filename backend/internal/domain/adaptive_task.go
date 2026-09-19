package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AdaptiveActor is trusted action context, never an imported role declaration.
type AdaptiveActor struct {
	Kind      string    `json:"kind" enum:"USER,ORCHESTRATOR,AGENT_MANAGER,WORKER,SYSTEM"`
	ID        string    `json:"id"`
	SessionID SessionID `json:"sessionId,omitempty"`
}

// TaskMutation attributes a planning change and fences its expected revision.
type TaskMutation struct {
	Actor            AdaptiveActor `json:"actor"`
	Reason           string        `json:"reason"`
	ExpectedRevision int64         `json:"expectedRevision"`
}

// ValidatePlanning denies implementing workers and selectors authority to
// rewrite a task's definition of success, including before the first lease.
func (m TaskMutation) ValidatePlanning() error {
	if m.Actor.Kind != "USER" && m.Actor.Kind != "ORCHESTRATOR" && m.Actor.Kind != "SYSTEM" {
		return fmt.Errorf("only the user, orchestrator or system can change task planning")
	}
	if strings.TrimSpace(m.Actor.ID) == "" || len(m.Actor.ID) > 200 || strings.ContainsRune(m.Actor.ID, 0) || len(m.Actor.SessionID) > 200 || strings.ContainsRune(string(m.Actor.SessionID), 0) || strings.TrimSpace(m.Reason) == "" || len(m.Reason) > 2000 || m.ExpectedRevision < 0 {
		return fmt.Errorf("invalid task mutation provenance")
	}
	if m.Actor.Kind == "ORCHESTRATOR" && m.Actor.SessionID == "" {
		return fmt.Errorf("orchestrator planning requires its session identity")
	}
	return nil
}

// AcceptanceCriterion gives evaluation an immutable requirement and evidence
// expectation. Commands are argument vectors; this record does not execute them.
type AcceptanceCriterion struct {
	ID             string   `json:"id"`
	Requirement    string   `json:"requirement"`
	EvidenceKind   string   `json:"evidenceKind" enum:"test,build,lint,ci,mergeability,review,artifact,manual"`
	Command        []string `json:"command,omitempty"`
	ArtifactPath   string   `json:"artifactPath,omitempty"`
	ArtifactSHA256 string   `json:"artifactSha256,omitempty"`
	CheckNames     []string `json:"checkNames,omitempty"`
}

// AcceptanceCriteria is versioned independently of task planning revisions.
type AcceptanceCriteria struct {
	Criteria []AcceptanceCriterion `json:"criteria"`
}

// Validate bounds review and command expectations before they can be pinned.
func (c AcceptanceCriteria) Validate() error {
	if len(c.Criteria) < 1 || len(c.Criteria) > 64 {
		return fmt.Errorf("acceptance criteria must contain 1 to 64 requirements")
	}
	seen := map[string]bool{}
	for _, item := range c.Criteria {
		if strings.TrimSpace(item.ID) == "" || len(item.ID) > 100 || seen[item.ID] || strings.TrimSpace(item.Requirement) == "" || len(item.Requirement) > 4000 {
			return fmt.Errorf("invalid or duplicate acceptance criterion")
		}
		seen[item.ID] = true
		switch item.EvidenceKind {
		case "test", "build", "lint", "ci", "mergeability", "review", "artifact", "manual":
		default:
			return fmt.Errorf("unknown acceptance evidence kind")
		}
		if len(item.Command) > 64 || len(item.ArtifactPath) > 1000 {
			return fmt.Errorf("acceptance verification exceeds its bounds")
		}
		for _, arg := range item.Command {
			if len(arg) > 2000 || strings.ContainsRune(arg, 0) {
				return fmt.Errorf("invalid verification argument")
			}
		}
		if len(item.Command) > 0 && strings.TrimSpace(item.Command[0]) == "" {
			return fmt.Errorf("verification command must name an executable")
		}
		if item.EvidenceKind == "artifact" && strings.TrimSpace(item.ArtifactPath) == "" {
			return fmt.Errorf("artifact verification requires a path")
		}
		if item.EvidenceKind == "ci" || len(item.CheckNames) > 0 {
			if !ciEvidenceKind(item.EvidenceKind) {
				return fmt.Errorf("check names require CI, test, build or lint evidence")
			}
			if len(item.CheckNames) < 1 || len(item.CheckNames) > 16 || len(item.Command) != 0 || item.ArtifactPath != "" {
				return fmt.Errorf("CI verification requires 1 to 16 exact check names and no command or artifact path")
			}
			names := map[string]bool{}
			for _, name := range item.CheckNames {
				if !resultText(name, 300, true) || names[name] {
					return fmt.Errorf("invalid or duplicate CI check name")
				}
				names[name] = true
			}
		}
		if item.EvidenceKind == "mergeability" && (len(item.Command) != 0 || item.ArtifactPath != "") {
			return fmt.Errorf("mergeability uses observed PR facts, not commands or artifacts")
		}
		if item.ArtifactSHA256 != "" && (item.EvidenceKind != "artifact" || !ValidContextFilePath(item.ArtifactPath) || !validArtifactHash(item.ArtifactSHA256) || len(item.Command) != 0) {
			return fmt.Errorf("artifact hash verification requires a portable path, SHA-256 and no command")
		}
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(encoded) > 65536 {
		return fmt.Errorf("acceptance criteria exceed 64 KiB")
	}
	return nil
}

func ciEvidenceKind(kind string) bool {
	return kind == "ci" || kind == "test" || kind == "build" || kind == "lint"
}

// TaskDefinition is work intent, independent of any session display status.
type TaskDefinition struct {
	Title                string           `json:"title"`
	Brief                string           `json:"brief"`
	Category             string           `json:"category"`
	Priority             int              `json:"priority"`
	ParentID             string           `json:"parentId,omitempty"`
	Dependencies         []string         `json:"dependencies" nullable:"true"`
	RequiredCapabilities []string         `json:"requiredCapabilities" nullable:"true"`
	RequestedWorker      *WorkerSelection `json:"requestedWorker,omitempty"`
	MaxAttempts          int              `json:"maxAttempts"`
	ContextFiles         []string         `json:"contextFiles,omitempty"`
}

// Validate bounds one revision; graph validity belongs to the store transaction.
func (d TaskDefinition) Validate() error {
	if strings.TrimSpace(d.Title) == "" || len(d.Title) > 300 || strings.TrimSpace(d.Brief) == "" || len(d.Brief) > 32000 || len(d.Category) > 100 || d.Priority < -100 || d.Priority > 100 || len(d.ParentID) > 200 || d.MaxAttempts < 1 || d.MaxAttempts > 10 {
		return fmt.Errorf("invalid task definition")
	}
	if len(d.Dependencies) > 32 || len(d.RequiredCapabilities) > 32 {
		return fmt.Errorf("task exceeds dependency or capability bounds")
	}
	if len(d.ContextFiles) > 16 {
		return fmt.Errorf("task context is limited to 16 explicit files")
	}
	files := map[string]bool{}
	for _, file := range d.ContextFiles {
		if !ValidContextFilePath(file) || files[file] {
			return fmt.Errorf("invalid or duplicate context file path")
		}
		files[file] = true
	}
	for _, values := range [][]string{d.Dependencies, d.RequiredCapabilities} {
		seen := map[string]bool{}
		for _, value := range values {
			if strings.TrimSpace(value) == "" || len(value) > 200 || strings.ContainsRune(value, 0) || seen[value] {
				return fmt.Errorf("invalid or duplicate task reference")
			}
			seen[value] = true
		}
	}
	if d.RequestedWorker != nil && (strings.TrimSpace(d.RequestedWorker.AgentTypeID) == "" || len(d.RequestedWorker.AgentTypeID) > 200 || strings.ContainsRune(d.RequestedWorker.AgentTypeID, 0) || d.RequestedWorker.Version < 0) {
		return fmt.Errorf("invalid requested worker selection")
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(encoded) > 65536 {
		return fmt.Errorf("task definition exceeds 64 KiB")
	}
	return nil
}

// AdaptiveTask is the stable project-scoped identity and current revision pointer.
type AdaptiveTask struct {
	ID        string        `json:"id"`
	ProjectID ProjectID     `json:"projectId"`
	Revision  int64         `json:"revision"`
	CreatedBy AdaptiveActor `json:"createdBy"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// TaskRevision is immutable, including its exact criteria version and graph.
type TaskRevision struct {
	TaskID          string         `json:"taskId"`
	Number          int64          `json:"number"`
	CriteriaVersion int64          `json:"criteriaVersion"`
	Definition      TaskDefinition `json:"definition"`
	ContentHash     string         `json:"contentHash"`
	Actor           AdaptiveActor  `json:"actor"`
	Reason          string         `json:"reason"`
	CreatedAt       time.Time      `json:"createdAt"`
}

// AcceptanceCriteriaVersion retains the definition of success for every attempt.
type AcceptanceCriteriaVersion struct {
	TaskID          string             `json:"taskId"`
	Number          int64              `json:"number"`
	PreviousVersion int64              `json:"previousVersion"`
	Definition      AcceptanceCriteria `json:"definition"`
	ContentHash     string             `json:"contentHash"`
	Actor           AdaptiveActor      `json:"actor"`
	Reason          string             `json:"reason"`
	CreatedAt       time.Time          `json:"createdAt"`
}

// TaskAudit retains ordered planning actions independently of CDC retention.
type TaskAudit struct {
	Sequence  int64         `json:"sequence"`
	TaskID    string        `json:"taskId"`
	Revision  int64         `json:"revision"`
	Action    string        `json:"action"`
	Actor     AdaptiveActor `json:"actor"`
	Reason    string        `json:"reason"`
	CreatedAt time.Time     `json:"createdAt"`
}

// TaskContent hashes a typed definition for immutable storage and context pins.
func TaskContent(value any) ([]byte, string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}
