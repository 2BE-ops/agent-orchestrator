package domain

import "fmt"

// TaskReviewSubmission carries the native reviewer's generation along with its
// qualitative result. It cannot choose or change any configuration attribution.
type TaskReviewSubmission struct {
	RunID            string
	SessionID        SessionID
	SourceGeneration string
	Verdict          ReviewVerdict
	Body             string
	GithubReviewID   string
}

// Validate bounds native review output and requires an exact source generation.
func (s TaskReviewSubmission) Validate() error {
	if !resultText(s.RunID, 200, true) || !resultText(string(s.SessionID), 200, true) || !resultText(s.SourceGeneration, 200, true) || !s.Verdict.Valid() || !resultText(s.Body, 64<<10, s.Verdict == VerdictChangesRequested) || !resultText(s.GithubReviewID, 200, false) {
		return fmt.Errorf("invalid bounded task review submission")
	}
	return nil
}
