package domain

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// KnowledgeSource is provenance, not executable instructions or an automatic
// remote fetch. Content/commit hashes identify evidence without copying secrets.
type KnowledgeSource struct {
	Kind        string    `json:"kind" enum:"user,worker,file,artifact,external"`
	Reference   string    `json:"reference"`
	TaskID      string    `json:"taskId,omitempty"`
	AttemptID   string    `json:"attemptId,omitempty"`
	SessionID   SessionID `json:"sessionId,omitempty"`
	Commit      string    `json:"commit,omitempty"`
	ContentHash string    `json:"contentHash,omitempty"`
}

// KnowledgeVersionRef links exact historical content rather than a mutable name.
type KnowledgeVersionRef struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

// KnowledgeDefinition keeps claims, provenance and review disposition together.
// Deletion withdraws future selection; immutable historical content is retained.
type KnowledgeDefinition struct {
	Title        string               `json:"title"`
	Kind         string               `json:"kind" enum:"architecture,convention,interface,constraint,pitfall,failed_approach,file_relationship,external_behavior,question"`
	Content      string               `json:"content"`
	Status       string               `json:"status" enum:"candidate,accepted,invalidated,superseded,deleted"`
	Confidence   string               `json:"confidence" enum:"low,medium,high"`
	Pinned       bool                 `json:"pinned"`
	Sources      []KnowledgeSource    `json:"sources"`
	TaskIDs      []string             `json:"taskIds,omitempty"`
	Tags         []string             `json:"tags,omitempty"`
	SupersededBy *KnowledgeVersionRef `json:"supersededBy,omitempty"`
}

// Validate bounds inert content before it enters project history or prompts.
func (d KnowledgeDefinition) Validate() error {
	if strings.TrimSpace(d.Title) == "" || len(d.Title) > 200 || strings.TrimSpace(d.Content) == "" || len(d.Content) > 16384 || strings.ContainsRune(d.Content, 0) {
		return fmt.Errorf("knowledge requires a title and content within 16 KiB")
	}
	switch d.Kind {
	case "architecture", "convention", "interface", "constraint", "pitfall", "failed_approach", "file_relationship", "external_behavior", "question":
	default:
		return fmt.Errorf("unknown knowledge kind")
	}
	switch d.Status {
	case "candidate", "accepted", "invalidated", "superseded", "deleted":
	default:
		return fmt.Errorf("unknown knowledge status")
	}
	if d.Confidence != "low" && d.Confidence != "medium" && d.Confidence != "high" {
		return fmt.Errorf("knowledge confidence must be low, medium or high")
	}
	if d.Pinned && d.Status != "accepted" {
		return fmt.Errorf("only accepted knowledge can be pinned")
	}
	if (d.Status == "superseded") != (d.SupersededBy != nil) || (d.SupersededBy != nil && (strings.TrimSpace(d.SupersededBy.ID) == "" || len(d.SupersededBy.ID) > 200 || d.SupersededBy.Version < 1)) {
		return fmt.Errorf("superseded knowledge requires an exact replacement")
	}
	if len(d.Sources) < 1 || len(d.Sources) > 16 || len(d.TaskIDs) > 16 || len(d.Tags) > 16 {
		return fmt.Errorf("knowledge requires 1 to 16 sources and bounded task/tag references")
	}
	for _, source := range d.Sources {
		switch source.Kind {
		case "user", "worker", "file", "artifact", "external":
		default:
			return fmt.Errorf("unknown knowledge source kind")
		}
		if strings.TrimSpace(source.Reference) == "" || len(source.Reference) > 2000 || strings.ContainsRune(source.Reference, 0) {
			return fmt.Errorf("invalid knowledge source reference")
		}
		for _, id := range []string{source.TaskID, source.AttemptID, string(source.SessionID)} {
			if len(id) > 200 || strings.ContainsRune(id, 0) {
				return fmt.Errorf("invalid knowledge source identity")
			}
		}
		if source.Kind == "worker" && (source.AttemptID == "" || source.SessionID == "" || source.TaskID == "") {
			return fmt.Errorf("worker source requires exact task, attempt and session")
		}
		for _, hash := range []string{source.Commit, source.ContentHash} {
			if hash != "" {
				if len(hash) != 40 && len(hash) != 64 {
					return fmt.Errorf("invalid provenance hash")
				}
				if _, err := hex.DecodeString(hash); err != nil {
					return fmt.Errorf("invalid provenance hash")
				}
			}
		}
	}
	for _, values := range [][]string{d.TaskIDs, d.Tags} {
		seen := map[string]bool{}
		for _, value := range values {
			if strings.TrimSpace(value) == "" || len(value) > 200 || strings.ContainsRune(value, 0) || seen[value] {
				return fmt.Errorf("invalid or duplicate knowledge relevance reference")
			}
			seen[value] = true
		}
	}
	content, _, err := TaskContent(d)
	if err != nil {
		return err
	}
	if len(content) > 65536 {
		return fmt.Errorf("knowledge definition exceeds 64 KiB")
	}
	return nil
}

// ProjectKnowledge is stable identity and its active immutable version pointer.
type ProjectKnowledge struct {
	ID        string    `json:"id"`
	ProjectID ProjectID `json:"projectId"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// KnowledgeVersion is the retained claim and author of one explicit revision.
type KnowledgeVersion struct {
	KnowledgeID string              `json:"knowledgeId"`
	Number      int64               `json:"number"`
	Definition  KnowledgeDefinition `json:"definition"`
	ContentHash string              `json:"contentHash"`
	Actor       AdaptiveActor       `json:"actor"`
	Reason      string              `json:"reason"`
	CreatedAt   time.Time           `json:"createdAt"`
}

// KnowledgeMutation receives actor authority from the daemon action boundary.
type KnowledgeMutation struct {
	Actor           AdaptiveActor
	Reason          string
	ExpectedVersion int64
}

// KnowledgeFilter bounds current-version project search. Deleted items remain
// inspectable explicitly, while default lists exclude them.
type KnowledgeFilter struct {
	ProjectID ProjectID
	After     string
	Limit     int
	Status    string
	Kind      string
	Search    string
}
