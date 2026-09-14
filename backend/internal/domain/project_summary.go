package domain

import "time"

// ProjectSummary is the replaceable, daemon-owned briefing for one project.
// SourceWatermark belongs to this consumer and never changes report delivery state.
type ProjectSummary struct {
	ProjectID        ProjectID              `json:"projectId"`
	Narrative        string                 `json:"narrative"`
	ActiveWorkers    int                    `json:"activeWorkers"`
	CompletedWorkers int                    `json:"completedWorkers"`
	NeedsAttention   []ProjectAttentionItem `json:"needsAttention"`
	Outputs          []ProjectSummaryOutput `json:"outputs"`
	SourceWatermark  string                 `json:"sourceWatermark"`
	GeneratedAt      time.Time              `json:"generatedAt"`
}

type ProjectAttentionItem struct {
	SessionID   SessionID `json:"sessionId"`
	SessionName string    `json:"sessionName"`
	Question    string    `json:"question"`
}

type ProjectSummaryOutput struct {
	SessionID   SessionID `json:"sessionId"`
	SessionName string    `json:"sessionName"`
	Kind        string    `json:"kind" enum:"pull_request"`
	URL         string    `json:"url"`
	Number      int       `json:"number"`
	State       string    `json:"state"`
}
