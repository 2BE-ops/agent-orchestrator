package domain

// TaskCompletion is a current read projection, never stored task status. A
// retained passing evaluation is necessary but mutable SCM/review facts are
// checked again before its result can be presented as completed work.
type TaskCompletion struct {
	TaskID       string `json:"taskId"`
	TaskRevision int64  `json:"taskRevision"`
	AttemptID    string `json:"attemptId,omitempty"`
	ResultID     string `json:"resultId,omitempty"`
	EvaluationID string `json:"evaluationId,omitempty"`
	Verified     bool   `json:"verified"`
	Reason       string `json:"reason"`
}
