// Package orchestratorfeed delivers the orchestrator loop's push half: derived
// terminal task facts pushed to the project's live orchestrator session through
// the native ownership boundary. The pull half (native feedback reads) stays
// authoritative; a notice reports facts and grants no authority.
package orchestratorfeed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Orchestrators resolves live orchestrator sessions and derives per-task loop
// facts. Both operations are reads; this consumer never spawns or plans.
type Orchestrators interface {
	LiveOrchestrator(ctx context.Context, project domain.ProjectID) (domain.SessionRecord, bool, error)
	Feedback(ctx context.Context, project domain.ProjectID, afterTaskID string, limit int) ([]domain.ProjectFeedbackItem, error)
}

// Store is the durable notice journal.
type Store interface {
	ports.OrchestratorNoticeStore
}

// Dispatcher is one daemon-owned, bounded push consumer.
type Dispatcher struct {
	store         Store
	orchestrators Orchestrators
	transport     ports.OrchestratorNoticeTransport
	newID         func() string
	now           func() time.Time
	// projectCursor is the in-memory fairness point across cycles; durable
	// state lives only in the notice journal itself.
	projectCursor string
	log           *slog.Logger
}

// New binds the journal, the orchestrator reads and the native transport.
func New(store Store, orchestrators Orchestrators, transport ports.OrchestratorNoticeTransport, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{store: store, orchestrators: orchestrators, transport: transport,
		newID: uuid.NewString, now: func() time.Time { return time.Now().UTC() }, log: logger}
}

// Run first settles notices a previous daemon run never resolved, then scans
// candidate projects each cycle. Each fact is journalled and delivered at most
// once; an unresolved delivery is retained as uncertainty, never resent.
func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	recovered := false
	for {
		if ctx.Err() != nil {
			return
		}
		if !recovered {
			if err := d.reconcile(ctx); err != nil {
				d.log.Warn("orchestrator notice recovery deferred", "error", err)
			} else {
				recovered = true
			}
		}
		if recovered {
			if err := d.dispatch(ctx); err != nil && ctx.Err() == nil {
				d.log.Warn("orchestrator notice dispatch deferred", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) reconcile(ctx context.Context) error {
	for {
		items, err := d.store.ListPendingOrchestratorNotices(ctx, 100)
		if err != nil {
			return err
		}
		for _, item := range items {
			// A restart interrupts delivery after the native write may have
			// happened; the ambiguity is retained rather than probed or retried.
			if err := d.store.ResolveOrchestratorNotice(ctx, domain.OrchestratorNoticeResolution{ID: item.ID,
				State: "uncertain", Reason: "Daemon restarted before the orchestrator notice outcome was recorded",
				ResolvedAt: d.now()}); err != nil {
				return err
			}
		}
		if len(items) < 100 {
			return nil
		}
	}
}

// dispatch scans one bounded page of candidate projects per cycle, advancing an
// in-memory cursor so a blocked project cannot starve the rest. Only projects
// with a live orchestrator derive feedback, and at most 300 task facts per
// project per cycle are inspected.
func (d *Dispatcher) dispatch(ctx context.Context) error {
	projects, err := d.store.ListOrchestratorNoticeProjects(ctx, d.projectCursor, 16)
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		d.projectCursor = ""
		return nil
	}
	for _, project := range projects {
		if err := d.dispatchProject(ctx, domain.ProjectID(project)); err != nil && ctx.Err() == nil {
			d.log.Warn("orchestrator notice project deferred", "projectID", project, "error", err)
		}
	}
	d.projectCursor = projects[len(projects)-1]
	if len(projects) < 16 {
		d.projectCursor = ""
	}
	return nil
}

func (d *Dispatcher) dispatchProject(ctx context.Context, project domain.ProjectID) error {
	live, found, err := d.orchestrators.LiveOrchestrator(ctx, project)
	if err != nil || !found {
		return err
	}
	afterTask := ""
	for page := 0; page < 3; page++ {
		items, err := d.orchestrators.Feedback(ctx, project, afterTask, 100)
		if err != nil {
			return err
		}
		for _, item := range items {
			afterTask = item.TaskID
			fact, anchor, ok := noticeAnchor(item)
			if !ok {
				continue
			}
			if err := d.notify(ctx, project, live, item, fact, anchor); err != nil {
				return err
			}
		}
		if len(items) < 100 {
			return nil
		}
	}
	return nil
}

// notify journals the fact and, only when this run created its row, delivers
// it exactly once through the native boundary.
func (d *Dispatcher) notify(ctx context.Context, project domain.ProjectID, live domain.SessionRecord, item domain.ProjectFeedbackItem, fact, anchor string) error {
	notice := domain.OrchestratorNotice{ID: d.newID(), ProjectID: project, TaskID: item.TaskID, Fact: fact,
		Anchor: anchor, Revision: item.Revision, Detail: noticeDetail(item), CreatedAt: d.now()}
	_, created, err := d.store.BeginOrchestratorNotice(ctx, notice)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	observed := d.transport.DeliverOrchestratorNotice(ctx, live.ID, notice)
	resolution := domain.OrchestratorNoticeResolution{ID: notice.ID, State: observed.State, Reason: observed.Reason, ResolvedAt: d.now()}
	if err := resolution.Validate(); err != nil {
		resolution.State, resolution.Reason = "uncertain", "Native transport returned an invalid notice observation"
	}
	// Persist even if the native call exhausted its context; a failed commit
	// leaves the pending row to startup reconciliation, never a blind resend.
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return d.store.ResolveOrchestratorNotice(commitCtx, resolution)
}

// noticeAnchor names the durable fact identity that dedups a push. Completion
// anchors on the sealed result, exhaustion and cancellation on the revision
// they became terminal for.
func noticeAnchor(item domain.ProjectFeedbackItem) (string, string, bool) {
	switch item.State {
	case "completed":
		if item.ResultID == "" {
			return "", "", false
		}
		return "completed", "result:" + item.ResultID, true
	case "failed":
		return "failed", fmt.Sprintf("exhausted:%d", item.Revision), true
	case "cancelled":
		return "cancelled", fmt.Sprintf("cancelled:%d", item.Revision), true
	default:
		return "", "", false
	}
}

func noticeDetail(item domain.ProjectFeedbackItem) string {
	detail := fmt.Sprintf("%s %q revision %d: %s", item.State, item.Title, item.Revision, item.Reason)
	if len(detail) > 4096 {
		detail = detail[:4096]
	}
	if detail == "" {
		detail = item.State
	}
	return detail
}
