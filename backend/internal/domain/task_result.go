package domain

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// TaskTestClaim is worker-reported evidence, never a daemon verification result.
// Commands are inert provenance; submitting one does not authorize execution.
type TaskTestClaim struct {
	Command []string `json:"command" nullable:"true"`
	Outcome string   `json:"outcome" enum:"passed,failed,not_run,unknown"`
	Details string   `json:"details"`
}

// TaskInterfaceClaim describes an interface asserted by the implementing worker.
type TaskInterfaceClaim struct {
	Name     string   `json:"name"`
	Contract string   `json:"contract"`
	Files    []string `json:"files" nullable:"true"`
}

// Validate bounds an interface claim and its portable workspace file references.
func (c TaskInterfaceClaim) Validate() error {
	if !resultText(c.Name, 200, true) || !resultText(c.Contract, 8000, true) || len(c.Files) > 16 {
		return fmt.Errorf("invalid interface claim")
	}
	seen := map[string]bool{}
	for _, file := range c.Files {
		if !ValidContextFilePath(file) || seen[file] {
			return fmt.Errorf("interface files must be unique portable workspace paths")
		}
		seen[file] = true
	}
	return nil
}

// TaskKnowledgeCandidate remains a proposal attributed to its result owner.
type TaskKnowledgeCandidate struct {
	Title      string   `json:"title"`
	Kind       string   `json:"kind" enum:"architecture,convention,interface,constraint,pitfall,failed_approach,file_relationship,external_behavior,question"`
	Content    string   `json:"content"`
	Confidence string   `json:"confidence" enum:"low,medium,high"`
	Tags       []string `json:"tags" nullable:"true"`
}

// TaskResultDefinition is a bounded schema for claims, separate from objective
// evaluation and task state. It cannot change acceptance criteria or release work.
type TaskResultDefinition struct {
	SchemaVersion       int                      `json:"schemaVersion"`
	ClaimedOutcome      string                   `json:"claimedOutcome" enum:"completed,partial,blocked"`
	ClaimedCommit       string                   `json:"claimedCommit,omitempty"`
	Summary             string                   `json:"summary"`
	Implementation      string                   `json:"implementation"`
	Decisions           []string                 `json:"decisions" nullable:"true"`
	Assumptions         []string                 `json:"assumptions" nullable:"true"`
	Interfaces          []TaskInterfaceClaim     `json:"interfaces" nullable:"true"`
	Tests               []TaskTestClaim          `json:"tests" nullable:"true"`
	Findings            []string                 `json:"findings" nullable:"true"`
	UnresolvedIssues    []string                 `json:"unresolvedIssues" nullable:"true"`
	RecommendedFollowUp []string                 `json:"recommendedFollowUp" nullable:"true"`
	KnowledgeCandidates []TaskKnowledgeCandidate `json:"knowledgeCandidates" nullable:"true"`
}

func resultText(value string, maximum int, required bool) bool {
	return len(value) <= maximum && !strings.ContainsRune(value, 0) && (!required || strings.TrimSpace(value) != "")
}

// Validate bounds every collection before encoding and rejects schema drift.
func (d TaskResultDefinition) Validate() error {
	if d.SchemaVersion != 1 || (d.ClaimedOutcome != "completed" && d.ClaimedOutcome != "partial" && d.ClaimedOutcome != "blocked") || !resultText(d.Summary, 4000, true) || !resultText(d.Implementation, 16000, false) {
		return fmt.Errorf("invalid schema v1 worker result")
	}
	if d.ClaimedCommit != "" {
		if len(d.ClaimedCommit) != 40 && len(d.ClaimedCommit) != 64 {
			return fmt.Errorf("claimed commit must be a full Git object ID")
		}
		if _, err := hex.DecodeString(d.ClaimedCommit); err != nil {
			return fmt.Errorf("claimed commit must be hexadecimal")
		}
	}
	for _, values := range [][]string{d.Decisions, d.Assumptions, d.Findings, d.UnresolvedIssues, d.RecommendedFollowUp} {
		if len(values) > 32 {
			return fmt.Errorf("worker result exceeds claim count bounds")
		}
		for _, value := range values {
			if !resultText(value, 2000, true) {
				return fmt.Errorf("invalid worker result claim")
			}
		}
	}
	if len(d.Interfaces) > 16 || len(d.Tests) > 32 || len(d.KnowledgeCandidates) > 8 {
		return fmt.Errorf("worker result exceeds structured claim bounds")
	}
	for _, contract := range d.Interfaces {
		if err := contract.Validate(); err != nil {
			return err
		}
	}
	for _, test := range d.Tests {
		if len(test.Command) > 32 || !resultText(test.Details, 4000, false) {
			return fmt.Errorf("invalid test claim bounds")
		}
		switch test.Outcome {
		case "passed", "failed", "not_run", "unknown":
		default:
			return fmt.Errorf("unknown test claim outcome")
		}
		for _, arg := range test.Command {
			if !resultText(arg, 2000, true) {
				return fmt.Errorf("invalid test command claim")
			}
		}
	}
	for _, candidate := range d.KnowledgeCandidates {
		definition := KnowledgeDefinition{Title: candidate.Title, Kind: candidate.Kind, Content: candidate.Content, Status: "candidate", Confidence: candidate.Confidence, Tags: candidate.Tags, Sources: []KnowledgeSource{{Kind: "artifact", Reference: "Worker result proposal"}}}
		if err := definition.Validate(); err != nil {
			return fmt.Errorf("invalid knowledge candidate: %w", err)
		}
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(encoded) > 256<<10 {
		return fmt.Errorf("worker result exceeds 256 KiB")
	}
	return nil
}

// TaskResult retains worker authorship and the exact configuration at submission.
// ContentHash hashes the definition; attribution is established transactionally.
type TaskResult struct {
	ID                    string               `json:"id"`
	TaskID                string               `json:"taskId"`
	AttemptID             string               `json:"attemptId"`
	Number                int64                `json:"number"`
	SessionID             SessionID            `json:"sessionId"`
	NativeGeneration      string               `json:"nativeGeneration"`
	TaskRevision          int64                `json:"taskRevision"`
	CriteriaVersion       int64                `json:"criteriaVersion"`
	ConfigurationHash     string               `json:"configurationHash"`
	ConfigurationSequence int64                `json:"configurationSequence"`
	ContextHash           string               `json:"contextHash"`
	Definition            TaskResultDefinition `json:"definition"`
	ContentHash           string               `json:"contentHash"`
	CreatedAt             time.Time            `json:"createdAt"`
}

// TaskResultSubmission carries trusted controller context, not user author tags.
// ExpectedVersion fences concurrent corrections; keys make retries idempotent.
type TaskResultSubmission struct {
	ID                 string
	AttemptID          string
	SessionID          SessionID
	SourceOwner        SessionControllerOwner
	ExpectedActivation int64
	ExpectedVersion    int64
	IdempotencyKey     string
	Definition         TaskResultDefinition
}

// Validate checks request bounds before opening a transaction.
func (s TaskResultSubmission) Validate() error {
	for _, value := range []string{s.ID, s.AttemptID, string(s.SessionID), s.IdempotencyKey} {
		if strings.TrimSpace(value) == "" || len(value) > 200 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return fmt.Errorf("invalid result submission identity")
		}
	}
	if s.ExpectedVersion < 0 || s.ExpectedVersion >= 16 || s.ExpectedActivation < 0 {
		return fmt.Errorf("result version or activation exceeds bounds")
	}
	return s.Definition.Validate()
}
