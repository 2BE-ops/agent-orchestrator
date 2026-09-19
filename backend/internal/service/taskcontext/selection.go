package taskcontext

import (
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type sourceSelection struct {
	snapshot   *domain.TaskContextSnapshot
	skipped    int
	restricted int
}

func (s *sourceSelection) fits(source domain.ContextSource) bool {
	if len(source.Content) > s.snapshot.Budget.MaxSourceBytes {
		return false
	}
	sources := append(append([]domain.ContextSource(nil), s.snapshot.Sources...), source)
	prompt, err := domain.RenderTaskContextPrompt(s.snapshot.BasePrompt, sources)
	bytes := len(prompt) + s.snapshot.SystemPromptBytes
	return err == nil && bytes <= s.snapshot.Budget.MaxBytes && (bytes+3)/4 <= s.snapshot.Budget.MaxEstimatedTokens
}

func (s *sourceSelection) required(source domain.ContextSource) error {
	source.Classification = source.Classification.Effective()
	if !domain.CanEmbedContext(s.snapshot.MaxContextClass, s.snapshot.EngagementID, source.Classification, source.EngagementID) {
		return fmt.Errorf("required context exceeds pinned clearance or engagement")
	}
	if len(s.snapshot.Sources) >= s.snapshot.Budget.MaxSources || !s.fits(source) {
		return fmt.Errorf("task context budget cannot include required task, criteria and configured instructions")
	}
	s.snapshot.Sources = append(s.snapshot.Sources, source)
	return nil
}

func (s *sourceSelection) room() bool {
	// Reserve one slot for a count when a custom source budget is small.
	return len(s.snapshot.Sources) < s.snapshot.Budget.MaxSources-1
}

func (s *sourceSelection) optional(source domain.ContextSource) {
	source.Classification = source.Classification.Effective()
	if !domain.CanEmbedContext(s.snapshot.MaxContextClass, s.snapshot.EngagementID, source.Classification, source.EngagementID) {
		// Do not retain a prohibited source's identity, hash, title or explanation.
		s.restricted++
		return
	}
	if !s.room() {
		s.skipped++
		return
	}
	if !s.fits(source) {
		source.Content, source.ContentHash, source.Disposition = "", "", "omitted"
		source.Reason += "; omitted by source or rendered prompt budget"
	}
	s.snapshot.Sources = append(s.snapshot.Sources, source)
	if !s.snapshot.Classification.Allows(source.Classification) {
		s.snapshot.Classification = source.Classification
	}
}

func (s *sourceSelection) finish() error {
	if s.restricted > 0 {
		s.optional(domain.ContextSource{Kind: "selection", ID: "context-classification-filter", Disposition: "omitted", Reason: "Sources outside pinned clearance or engagement were excluded"})
	}
	if s.skipped > 0 {
		if len(s.snapshot.Sources) >= s.snapshot.Budget.MaxSources {
			return fmt.Errorf("task context source budget cannot retain omission provenance")
		}
		s.snapshot.Sources = append(s.snapshot.Sources, domain.ContextSource{Classification: domain.ContextTechnical, Kind: "selection", ID: "context-source-limit", Disposition: "omitted", Reason: fmt.Sprintf("Source-count budget omitted %d additional candidate records", s.skipped)})
	}
	var err error
	s.snapshot.Prompt, err = domain.RenderTaskContextPrompt(s.snapshot.BasePrompt, s.snapshot.Sources)
	s.snapshot.TotalBytes = s.snapshot.SystemPromptBytes + len(s.snapshot.Prompt)
	s.snapshot.EstimatedTokens = (s.snapshot.TotalBytes + 3) / 4
	return err
}
