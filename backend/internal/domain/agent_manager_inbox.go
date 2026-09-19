package domain

import (
	"fmt"
	"time"
)

// AgentManagerRequest pins routing intent, not a delegation or permission to run.
// Content is referenced rather than embedded; the classified Context Builder
// supplies material to the native controller through a separately sealed artifact.
type AgentManagerRequest struct {
	ID                   string        `json:"id"`
	Sequence             int64         `json:"sequence"`
	SchemaVersion        int           `json:"schemaVersion"`
	ProjectID            ProjectID     `json:"projectId"`
	Kind                 string        `json:"kind" enum:"select_worker"`
	TaskID               string        `json:"taskId"`
	TaskRevision         int64         `json:"taskRevision"`
	TaskContentHash      string        `json:"taskContentHash"`
	CriteriaVersion      int64         `json:"criteriaVersion"`
	CriteriaContentHash  string        `json:"criteriaContentHash"`
	ConfigurationVersion int64         `json:"configurationVersion"`
	ConfigurationHash    string        `json:"configurationHash"`
	Actor                AdaptiveActor `json:"actor"`
	Reason               string        `json:"reason"`
	CreatedAt            time.Time     `json:"createdAt"`
	ContentHash          string        `json:"contentHash"`
}

// Hash excludes only the database paging cursor and the hash itself.
func (r AgentManagerRequest) Hash() string {
	r.Sequence, r.ContentHash = 0, ""
	_, hash, _ := TaskContent(r)
	return hash
}

// Validate verifies immutable references and request provenance.
func (r AgentManagerRequest) Validate() error {
	input := AgentManagerEnqueue{ID: r.ID, ProjectID: r.ProjectID, TaskID: r.TaskID, TaskRevision: r.TaskRevision, ConfigurationVersion: r.ConfigurationVersion, Actor: r.Actor, Reason: r.Reason, Now: r.CreatedAt}
	if err := input.Validate(); err != nil {
		return err
	}
	if r.SchemaVersion != 1 || r.Kind != "select_worker" || r.Sequence < 0 || r.CriteriaVersion < 1 || !validArtifactHash(r.TaskContentHash) || !validArtifactHash(r.CriteriaContentHash) || !validArtifactHash(r.ConfigurationHash) || r.ContentHash != r.Hash() {
		return fmt.Errorf("invalid sealed Manager request")
	}
	return nil
}

// AgentManagerEnqueue comes from trusted user/orchestrator/system context.
// A Manager or implementation worker cannot silently rewrite routing intent.
type AgentManagerEnqueue struct {
	ID                   string
	ProjectID            ProjectID
	TaskID               string
	TaskRevision         int64
	ConfigurationVersion int64
	Actor                AdaptiveActor
	Reason               string
	Now                  time.Time
}

// Validate requires bounded identities and trusted planning authority.
func (e AgentManagerEnqueue) Validate() error {
	if !messageIdentity(e.ID) || !messageIdentity(string(e.ProjectID)) || !messageIdentity(e.TaskID) || e.TaskRevision < 1 || e.ConfigurationVersion < 1 || e.ConfigurationVersion > 1000 || e.Now.IsZero() {
		return fmt.Errorf("manager request requires exact task and configuration revisions")
	}
	return validateManagerInboxActor(e.Actor, e.Reason)
}

func validateManagerInboxActor(actor AdaptiveActor, reason string) error {
	if !messageIdentity(actor.ID) || (actor.Kind != "ORCHESTRATOR" && actor.SessionID != "") || (actor.Kind == "ORCHESTRATOR" && !messageIdentity(string(actor.SessionID))) {
		return fmt.Errorf("invalid Manager inbox authority")
	}
	return (TaskMutation{Actor: actor, Reason: reason}).ValidatePlanning()
}

// AgentManagerRequestResolution is append-only terminal routing state. This
// foundation permits cancellation/escalation, never a claimed successful launch.
// Applied selections will require a separately validated decision transaction.
type AgentManagerRequestResolution struct {
	RequestID string        `json:"requestId"`
	Outcome   string        `json:"outcome" enum:"cancelled,superseded,needs_human"`
	Actor     AdaptiveActor `json:"actor"`
	Reason    string        `json:"reason"`
	CreatedAt time.Time     `json:"createdAt"`
}

// Validate rejects unvalidated success and authority supplied by a worker.
func (r AgentManagerRequestResolution) Validate() error {
	if !messageIdentity(r.RequestID) || r.CreatedAt.IsZero() || (r.Outcome != "cancelled" && r.Outcome != "superseded" && r.Outcome != "needs_human") {
		return fmt.Errorf("invalid Manager request resolution")
	}
	return validateManagerInboxActor(r.Actor, r.Reason)
}
