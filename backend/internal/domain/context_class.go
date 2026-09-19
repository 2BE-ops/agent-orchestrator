package domain

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ContextClass is an ordered sensitivity label. Empty historical values mean
// technical without rewriting immutable definitions or their content hashes.
type ContextClass string

// TaskContextPolicy is resolved from the attempt's immutable task revision and
// the exact Agent Type version in its original dispatch configuration.
type TaskContextPolicy struct {
	MaxContextClass ContextClass
	Classification  ContextClass
	EngagementID    string
}

// Context sensitivity increases from technical through engagement to mission.
const (
	ContextTechnical  ContextClass = "technical"
	ContextEngagement ContextClass = "engagement"
	ContextMission    ContextClass = "mission"
)

// Effective resolves the legacy default; unknown values remain invalid.
func (c ContextClass) Effective() ContextClass {
	if c == "" {
		return ContextTechnical
	}
	return c
}

// Validate rejects unknown labels instead of guessing their relative authority.
func (c ContextClass) Validate() error {
	switch c.Effective() {
	case ContextTechnical, ContextEngagement, ContextMission:
		return nil
	default:
		return fmt.Errorf("unknown context classification")
	}
}

// Allows tests the lattice only. Engagement isolation is an additional check.
func (c ContextClass) Allows(material ContextClass) bool {
	if c.Validate() != nil || material.Validate() != nil {
		return false
	}
	return c.Effective() == ContextMission || material.Effective() == ContextTechnical || c.Effective() == material.Effective()
}

// ValidateContextScope bounds a project-local engagement identity. A technical
// task may name its receiving engagement without making its public facts secret.
// Mission material can retain an engagement when derived from scoped inputs.
func ValidateContextScope(class ContextClass, engagementID string) error {
	if err := class.Validate(); err != nil {
		return err
	}
	if len(engagementID) > 200 || !utf8.ValidString(engagementID) || strings.TrimSpace(engagementID) != engagementID || strings.IndexFunc(engagementID, unicode.IsControl) >= 0 {
		return fmt.Errorf("invalid context engagement identity")
	}
	if class.Effective() == ContextEngagement && engagementID == "" {
		return fmt.Errorf("engagement context requires an engagement identity")
	}
	return nil
}

// CanEmbedContext is deterministic need-to-know enforcement. Higher clearance
// does not authorize a different engagement. Technical results always flow up.
func CanEmbedContext(clearance ContextClass, receivingEngagement string, class ContextClass, sourceEngagement string) bool {
	if ValidateContextScope(class, sourceEngagement) != nil || ValidateContextScope(ContextTechnical, receivingEngagement) != nil || !clearance.Allows(class) {
		return false
	}
	return class.Effective() == ContextTechnical || sourceEngagement == "" || sourceEngagement == receivingEngagement
}
