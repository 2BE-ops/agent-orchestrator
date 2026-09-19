package domain

// TaskResultContext selects previous findings and interface claims for a new
// worker. It intentionally carries no executable test command or success vote.
// SourceHash in the enclosing manifest still pins the complete original result.
type TaskResultContext struct {
	ResultID         string               `json:"resultId"`
	TaskID           string               `json:"taskId"`
	AttemptID        string               `json:"attemptId"`
	TaskRevision     int64                `json:"taskRevision"`
	CriteriaVersion  int64                `json:"criteriaVersion"`
	ClaimedOutcome   string               `json:"claimedOutcome"`
	ClaimedCommit    string               `json:"claimedCommit,omitempty"`
	Summary          string               `json:"summary"`
	Findings         []string             `json:"findings" nullable:"true"`
	UnresolvedIssues []string             `json:"unresolvedIssues" nullable:"true"`
	Interfaces       []TaskInterfaceClaim `json:"interfaces" nullable:"true"`
}

// ContextFacts projects immutable claims without treating them as verification.
func (r TaskResult) ContextFacts() TaskResultContext {
	return TaskResultContext{ResultID: r.ID, TaskID: r.TaskID, AttemptID: r.AttemptID, TaskRevision: r.TaskRevision, CriteriaVersion: r.CriteriaVersion, ClaimedOutcome: r.Definition.ClaimedOutcome, ClaimedCommit: r.Definition.ClaimedCommit, Summary: r.Definition.Summary, Findings: r.Definition.Findings, UnresolvedIssues: r.Definition.UnresolvedIssues, Interfaces: r.Definition.Interfaces}
}
