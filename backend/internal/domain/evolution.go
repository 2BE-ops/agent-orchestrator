package domain

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// EvolutionCohortLimit bounds one complete comparable-cohort read. Larger
// admission windows must be narrowed, never silently partially compared.
const EvolutionCohortLimit = 1000

// EvolutionDiffFieldLimit bounds one inspectable definition diff.
const EvolutionDiffFieldLimit = 64

// EvolutionExperiment pins one controlled comparison before any evidence is
// read. Control and candidate are exact immutable versions of the same entry,
// so a cohort can never silently mix definitions.
type EvolutionExperiment struct {
	ID               string               `json:"id"`
	ProjectID        ProjectID            `json:"projectId"`
	Kind             RegistryKind         `json:"kind" enum:"agent_type,skill"`
	EntryID          string               `json:"entryId"`
	ControlVersion   int64                `json:"controlVersion"`
	CandidateVersion int64                `json:"candidateVersion"`
	Hypothesis       string               `json:"hypothesis"`
	MinimumSamples   int64                `json:"minimumSamples"`
	Status           string               `json:"status" enum:"running,concluded"`
	CreatedAt        time.Time            `json:"createdAt"`
	Conclusion       *EvolutionConclusion `json:"conclusion,omitempty"`
}

// Validate pins the comparison identity and comparison bounds.
func (e EvolutionExperiment) Validate() error {
	if !registryText(e.ID, 200, true) || !registryText(string(e.ProjectID), 200, true) || !registryText(e.EntryID, 200, true) {
		return fmt.Errorf("evolution experiment requires bounded identities")
	}
	if e.Kind != RegistryAgentType && e.Kind != RegistrySkill {
		return fmt.Errorf("evolution experiment kind must be agent_type or skill")
	}
	if e.ControlVersion < 1 || e.CandidateVersion < 1 || e.ControlVersion == e.CandidateVersion {
		return fmt.Errorf("evolution experiment requires two distinct existing versions of one entry")
	}
	if !registryText(e.Hypothesis, 2000, true) {
		return fmt.Errorf("evolution hypothesis is required and bounded")
	}
	if e.MinimumSamples < 1 || e.MinimumSamples > EvolutionCohortLimit {
		return fmt.Errorf("evolution minimum samples must be between 1 and %d", EvolutionCohortLimit)
	}
	if e.Status != "running" && e.Status != "concluded" {
		return fmt.Errorf("evolution experiment status is running or concluded")
	}
	if e.CreatedAt.IsZero() {
		return fmt.Errorf("evolution experiment requires a creation timestamp")
	}
	if e.Status == "concluded" && e.Conclusion == nil {
		return fmt.Errorf("concluded evolution experiment requires its sealed conclusion")
	}
	if e.Status == "running" && e.Conclusion != nil {
		return fmt.Errorf("running evolution experiment cannot carry a conclusion")
	}
	return nil
}

// EvolutionCohortMetrics keeps comparison-relevant counters with their own
// denominators. They are observations, not calibrated quality scores.
type EvolutionCohortMetrics struct {
	Attempts           int64 `json:"attempts"`
	AssessedPassed     int64 `json:"assessedPassed"`
	AssessedFailed     int64 `json:"assessedFailed"`
	Inconclusive       int64 `json:"inconclusive"`
	Unassessed         int64 `json:"unassessed"`
	Superseded         int64 `json:"superseded"`
	FirstPassCompleted int64 `json:"firstPassCompleted"`
	ResultRevisions    int64 `json:"resultRevisions"`
	RetryAttempts      int64 `json:"retryAttempts"`
	Ongoing            int64 `json:"ongoing"`
}

// EvolutionCohort is one side of the comparison with its comparable-attempt
// denominator. ComparableAttempts excludes mixed and unseeded attribution.
type EvolutionCohort struct {
	Version            int64                  `json:"version"`
	ComparableAttempts int64                  `json:"comparableAttempts"`
	Metrics            EvolutionCohortMetrics `json:"metrics"`
}

// EvolutionEvidence is a complete admission-window observation of both cohorts.
// ConfoundedTasks counts distinct tasks that contributed attempts to both
// cohorts, which blocks a causal reading.
type EvolutionEvidence struct {
	From             time.Time       `json:"from"`
	To               time.Time       `json:"to"`
	ObservedAt       time.Time       `json:"observedAt"`
	Control          EvolutionCohort `json:"control"`
	Candidate        EvolutionCohort `json:"candidate"`
	ExcludedMixed    int64           `json:"excludedMixedAttempts"`
	ExcludedUnseeded int64           `json:"excludedUnseededAttempts"`
	ConfoundedTasks  int64           `json:"confoundedTasks"`
}

// Validate bounds the sealed evidence window and both pinned versions.
func (e EvolutionEvidence) Validate() error {
	if e.From.IsZero() || e.To.IsZero() || !e.From.Before(e.To) || e.To.Sub(e.From) > 366*24*time.Hour {
		return fmt.Errorf("evolution evidence requires an admission window up to 366 days")
	}
	if e.Control.Version < 1 || e.Candidate.Version < 1 || e.Control.Version == e.Candidate.Version {
		return fmt.Errorf("evolution evidence requires both pinned versions")
	}
	return nil
}

// EvolutionVerdict reports the promotion gates over sealed evidence. Findings
// name every failed gate so nobody has to guess why promotion is blocked.
type EvolutionVerdict struct {
	Eligible bool     `json:"eligible"`
	Findings []string `json:"findings"`
}

// EvaluateEvolutionEvidence applies the promotion gates: both cohorts must
// reach the experiment's minimum comparable samples, and no task may have fed
// both cohorts. One successful task is never sufficient evidence by itself
// because MinimumSamples is at least one per cohort and confounds block
// causal claims.
func EvaluateEvolutionEvidence(experiment EvolutionExperiment, evidence EvolutionEvidence) EvolutionVerdict {
	verdict := EvolutionVerdict{Findings: []string{}}
	if experiment.ControlVersion != evidence.Control.Version || experiment.CandidateVersion != evidence.Candidate.Version {
		verdict.Findings = append(verdict.Findings, "evidence does not match the experiment's pinned versions")
	}
	for _, cohort := range []struct {
		side  string
		value int64
	}{{"control", evidence.Control.ComparableAttempts}, {"candidate", evidence.Candidate.ComparableAttempts}} {
		if cohort.value < experiment.MinimumSamples {
			verdict.Findings = append(verdict.Findings, fmt.Sprintf("%s cohort has %d comparable attempts, minimum %d", cohort.side, cohort.value, experiment.MinimumSamples))
		}
	}
	if evidence.ConfoundedTasks > 0 {
		verdict.Findings = append(verdict.Findings, fmt.Sprintf("%d tasks contributed attempts to both cohorts", evidence.ConfoundedTasks))
	}
	verdict.Eligible = len(verdict.Findings) == 0
	return verdict
}

// EvolutionConclusion seals one terminal decision with the evidence observed
// at decision time. Promotion names the policy path: a manager may version
// only when the entry policy allows it, otherwise a recommendation is
// required; keep/insufficient outcomes promote nothing.
type EvolutionConclusion struct {
	Outcome   string            `json:"outcome" enum:"promote_candidate,keep_control,insufficient_evidence"`
	Promotion string            `json:"promotion" enum:"version_permitted,recommendation_required,none"`
	Reason    string            `json:"reason"`
	Actor     RegistryActor     `json:"actor"`
	DecidedAt time.Time         `json:"decidedAt"`
	Evidence  EvolutionEvidence `json:"evidence"`
	Verdict   EvolutionVerdict  `json:"verdict"`
}

// Validate refuses conclusions that misstate their gates or seals.
func (c EvolutionConclusion) Validate() error {
	if !registryText(c.Reason, 2000, true) || c.DecidedAt.IsZero() {
		return fmt.Errorf("evolution conclusion requires a bounded reason and timestamp")
	}
	switch c.Actor.Origin {
	case RegistryUser, RegistryManager, RegistrySystem:
	default:
		return fmt.Errorf("evolution conclusion requires a known actor")
	}
	if !registryText(c.Actor.ID, 200, true) {
		return fmt.Errorf("evolution conclusion actor identity is bounded")
	}
	if err := c.Evidence.Validate(); err != nil {
		return err
	}
	switch c.Outcome {
	case "promote_candidate":
		if c.Promotion != "version_permitted" && c.Promotion != "recommendation_required" {
			return fmt.Errorf("promoting a candidate requires an explicit policy path")
		}
		if !c.Verdict.Eligible {
			return fmt.Errorf("promotion requires eligible comparable cohorts")
		}
	case "keep_control", "insufficient_evidence":
		if c.Promotion != "none" {
			return fmt.Errorf("non-promoting outcomes promote nothing")
		}
	default:
		return fmt.Errorf("unknown evolution outcome")
	}
	return nil
}

// EvolutionRecommendation persists one improvement proposal with its evidence
// sample size and inspectable proposed definition. It never edits versions;
// adoption happens through normal registry authoring.
type EvolutionRecommendation struct {
	ID          string             `json:"id"`
	ProjectID   ProjectID          `json:"projectId"`
	Kind        RegistryKind       `json:"kind" enum:"agent_type,skill"`
	EntryID     string             `json:"entryId"`
	FromVersion int64              `json:"fromVersion"`
	Observation string             `json:"observation"`
	SampleSize  int64              `json:"sampleSize"`
	Proposed    RegistryDefinition `json:"proposed"`
	Status      string             `json:"status" enum:"pending,dismissed,adopted"`
	CreatedAt   time.Time          `json:"createdAt"`
	Decision    *EvolutionDecision `json:"decision,omitempty"`
}

// Validate keeps the recommendation inspectable and bounded.
func (r EvolutionRecommendation) Validate() error {
	if !registryText(r.ID, 200, true) || !registryText(string(r.ProjectID), 200, true) || !registryText(r.EntryID, 200, true) {
		return fmt.Errorf("evolution recommendation requires bounded identities")
	}
	if r.Kind != RegistryAgentType && r.Kind != RegistrySkill {
		return fmt.Errorf("evolution recommendation kind must be agent_type or skill")
	}
	if r.FromVersion < 1 {
		return fmt.Errorf("evolution recommendation pins the version it improves")
	}
	if !registryText(r.Observation, 2000, true) {
		return fmt.Errorf("evolution recommendation observation is required and bounded")
	}
	if r.SampleSize < 1 || r.SampleSize > EvolutionCohortLimit {
		return fmt.Errorf("evolution recommendation sample size must be between 1 and %d", EvolutionCohortLimit)
	}
	if err := r.Proposed.Validate(r.Kind); err != nil {
		return err
	}
	switch r.Status {
	case "pending":
		if r.Decision != nil {
			return fmt.Errorf("pending recommendation carries no decision")
		}
	case "dismissed", "adopted":
		if r.Decision == nil {
			return fmt.Errorf("decided recommendation seals its decision")
		}
	default:
		return fmt.Errorf("evolution recommendation status is pending, dismissed or adopted")
	}
	if r.CreatedAt.IsZero() {
		return fmt.Errorf("evolution recommendation requires a creation timestamp")
	}
	return nil
}

// EvolutionDecision seals the one-time disposition change with its actor.
type EvolutionDecision struct {
	Disposition string        `json:"disposition" enum:"dismissed,adopted"`
	Reason      string        `json:"reason"`
	Actor       RegistryActor `json:"actor"`
	DecidedAt   time.Time     `json:"decidedAt"`
}

// Validate bounds the decision content and actor.
func (d EvolutionDecision) Validate() error {
	if d.Disposition != "dismissed" && d.Disposition != "adopted" {
		return fmt.Errorf("evolution decision dismisses or adopts")
	}
	if !registryText(d.Reason, 2000, true) || d.DecidedAt.IsZero() {
		return fmt.Errorf("evolution decision requires a bounded reason and timestamp")
	}
	switch d.Actor.Origin {
	case RegistryUser, RegistryManager, RegistrySystem:
	default:
		return fmt.Errorf("evolution decision requires a known actor")
	}
	if !registryText(d.Actor.ID, 200, true) {
		return fmt.Errorf("evolution decision actor identity is bounded")
	}
	return nil
}

// EvolutionEvidenceQuery selects one experiment's admission cohort. The
// window bounds match every other cohort read: up to 366 days, from before to.
type EvolutionEvidenceQuery struct {
	ProjectID    ProjectID
	ExperimentID string
	From         time.Time
	To           time.Time
}

// Validate bounds the evidence window and identities.
func (q EvolutionEvidenceQuery) Validate() error {
	if !registryText(string(q.ProjectID), 200, true) || !registryText(q.ExperimentID, 200, true) {
		return fmt.Errorf("evolution evidence requires bounded identities")
	}
	if q.From.IsZero() || q.To.IsZero() || !q.From.Before(q.To) || q.To.Sub(q.From) > 366*24*time.Hour {
		return fmt.Errorf("evolution evidence requires an admission window up to 366 days")
	}
	return nil
}

// EvolutionConclusionRequest is the caller's terminal decision. The evidence
// itself is never trusted from the caller: the store recomputes and seals it.
type EvolutionConclusionRequest struct {
	Outcome string
	Reason  string
	Actor   RegistryActor
	From    time.Time
	To      time.Time
}

// Validate checks the request shape before any evidence is read.
func (r EvolutionConclusionRequest) Validate() error {
	switch r.Outcome {
	case "promote_candidate", "keep_control", "insufficient_evidence":
	default:
		return fmt.Errorf("unknown evolution outcome")
	}
	if !registryText(r.Reason, 2000, true) {
		return fmt.Errorf("evolution conclusion requires a bounded reason")
	}
	switch r.Actor.Origin {
	case RegistryUser, RegistryManager, RegistrySystem:
	default:
		return fmt.Errorf("evolution conclusion requires a known actor")
	}
	if !registryText(r.Actor.ID, 200, true) {
		return fmt.Errorf("evolution conclusion actor identity is bounded")
	}
	if r.From.IsZero() || r.To.IsZero() || !r.From.Before(r.To) || r.To.Sub(r.From) > 366*24*time.Hour {
		return fmt.Errorf("evolution conclusion requires an admission window up to 366 days")
	}
	return nil
}

// RegistryDiffField is one inspectable change between two definitions.
type RegistryDiffField struct {
	Path   string `json:"path"`
	Change string `json:"change" enum:"added,removed,changed"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

// RegistryDefinitionDiff is the complete bounded field diff between two
// immutable versions of one entry. It is descriptive, never a verdict.
type RegistryDefinitionDiff struct {
	Kind       RegistryKind        `json:"kind" enum:"agent_type,skill"`
	EntryID    string              `json:"entryId"`
	FromNumber int64               `json:"fromVersion"`
	ToNumber   int64               `json:"toVersion"`
	Fields     []RegistryDiffField `json:"fields"`
}

// DiffRegistryDefinitions compares two validated definitions of one kind. The
// diff is complete or refused: more than EvolutionDiffFieldLimit changed
// fields is an error instead of a silently truncated list.
func DiffRegistryDefinitions(kind RegistryKind, entryID string, fromNumber, toNumber int64, from, to RegistryDefinition) (RegistryDefinitionDiff, error) {
	diff := RegistryDefinitionDiff{Kind: kind, EntryID: entryID, FromNumber: fromNumber, ToNumber: toNumber, Fields: []RegistryDiffField{}}
	if err := from.Validate(kind); err != nil {
		return diff, err
	}
	if err := to.Validate(kind); err != nil {
		return diff, err
	}
	render := func(value any) string {
		encoded, err := json.Marshal(value)
		if err != nil {
			return "unrenderable"
		}
		text := string(encoded)
		if len(text) > 4096 {
			return text[:4096] + "…(truncated)"
		}
		return text
	}
	add := func(path, change string, fromValue, toValue any) {
		fromText, toText := "", ""
		if change != "added" {
			fromText = render(fromValue)
		}
		if change != "removed" {
			toText = render(toValue)
		}
		diff.Fields = append(diff.Fields, RegistryDiffField{Path: path, Change: change, From: fromText, To: toText})
	}
	scalar := func(path string, fromValue, toValue any) {
		if render(fromValue) != render(toValue) {
			add(path, "changed", fromValue, toValue)
		}
	}
	list := func(path string, fromItems, toItems []string) {
		for _, item := range fromItems {
			if !slices.Contains(toItems, item) {
				add(path+"."+item, "removed", item, nil)
			}
		}
		for _, item := range toItems {
			if !slices.Contains(fromItems, item) {
				add(path+"."+item, "added", nil, item)
			}
		}
	}
	if kind == RegistryAgentType && from.AgentType != nil && to.AgentType != nil {
		fromType, toType := from.AgentType, to.AgentType
		scalar("maxContextClass", string(fromType.MaxContextClass), string(toType.MaxContextClass))
		scalar("harness", string(fromType.Harness), string(toType.Harness))
		scalar("sessionMode", string(fromType.SessionMode), string(toType.SessionMode))
		scalar("config", fromType.Config, toType.Config)
		scalar("providerBindingId", fromType.ProviderBindingID, toType.ProviderBindingID)
		scalar("providerBindingRequired", fromType.ProviderBindingRequired, toType.ProviderBindingRequired)
		scalar("instructions", fromType.Instructions, toType.Instructions)
		scalar("maxParallelWorkers", fromType.MaxParallelWorkers, toType.MaxParallelWorkers)
		list("capabilities", fromType.Capabilities, toType.Capabilities)
		fromSkills, toSkills := []string{}, []string{}
		for _, ref := range fromType.Skills {
			fromSkills = append(fromSkills, fmt.Sprintf("%s@%d", ref.ID, ref.Version))
		}
		for _, ref := range toType.Skills {
			toSkills = append(toSkills, fmt.Sprintf("%s@%d", ref.ID, ref.Version))
		}
		list("skills", fromSkills, toSkills)
	}
	if kind == RegistrySkill && from.Skill != nil && to.Skill != nil {
		fromSkill, toSkill := from.Skill, to.Skill
		scalar("instructions", fromSkill.Instructions, toSkill.Instructions)
		list("capabilities", fromSkill.Capabilities, toSkill.Capabilities)
		list("requiredTools", fromSkill.RequiredTools, toSkill.RequiredTools)
		list("requiredMcpServers", fromSkill.RequiredMCPServers, toSkill.RequiredMCPServers)
		fromResources, toResources := map[string]string{}, map[string]string{}
		for _, resource := range fromSkill.Resources {
			fromResources[resource.Path] = resource.Content
		}
		for _, resource := range toSkill.Resources {
			toResources[resource.Path] = resource.Content
		}
		paths := []string{}
		for path := range fromResources {
			paths = append(paths, path)
		}
		for path := range toResources {
			if !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
		slices.Sort(paths)
		for _, path := range paths {
			fromContent, fromOK := fromResources[path]
			toContent, toOK := toResources[path]
			switch {
			case fromOK && !toOK:
				add("resources."+path, "removed", fromContent, nil)
			case !fromOK && toOK:
				add("resources."+path, "added", nil, toContent)
			case fromContent != toContent:
				add("resources."+path, "changed", fromContent, toContent)
			}
		}
	}
	if len(diff.Fields) > EvolutionDiffFieldLimit {
		return RegistryDefinitionDiff{}, fmt.Errorf("definition diff exceeds %d fields", EvolutionDiffFieldLimit)
	}
	slices.SortFunc(diff.Fields, func(a, b RegistryDiffField) int {
		return strings.Compare(a.Path, b.Path)
	})
	return diff, nil
}
