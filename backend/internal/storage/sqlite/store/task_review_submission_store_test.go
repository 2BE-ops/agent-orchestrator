package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/review"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestTaskReviewSubmissionGenerationAndExactRetry(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	_, run, frozen := taskReviewFixture(t, s)
	if err := s.InsertTaskReviewRun(ctx, run, frozen); err != nil {
		t.Fatal(err)
	}
	input := domain.TaskReviewSubmission{RunID: run.ID, SessionID: run.SessionID, SourceGeneration: frozen.LaunchID, Verdict: domain.VerdictApproved, Body: "No blocking findings", GithubReviewID: "provider-review"}
	if _, _, err := s.SubmitTaskReviewResult(ctx, input); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("unclaimed native generation accepted: %v", err)
	}
	claim := domain.Review{ID: run.ReviewID, SessionID: run.SessionID, ProjectID: "project", Harness: run.Harness, ReviewerLaunchID: frozen.LaunchID, CreatedAt: run.CreatedAt, UpdatedAt: time.Now().UTC()}
	if err := s.UpsertReview(ctx, claim); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*domain.TaskReviewSubmission)
		want   error
	}{
		{"missing generation", func(v *domain.TaskReviewSubmission) { v.SourceGeneration = "" }, ports.ErrTaskInvalid},
		{"stale generation", func(v *domain.TaskReviewSubmission) { v.SourceGeneration = "stale" }, ports.ErrTaskLeaseFenced},
		{"wrong worker", func(v *domain.TaskReviewSubmission) { v.SessionID = "other" }, ports.ErrTaskForbidden},
		{"oversized body", func(v *domain.TaskReviewSubmission) { v.Body = strings.Repeat("x", (64<<10)+1) }, ports.ErrTaskInvalid},
		{"empty change request", func(v *domain.TaskReviewSubmission) { v.Verdict, v.Body = domain.VerdictChangesRequested, "" }, ports.ErrTaskInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := input
			tc.change(&bad)
			if _, created, err := s.SubmitTaskReviewResult(ctx, bad); created || !errors.Is(err, tc.want) {
				t.Fatalf("submission = %v %v, want %v", created, err, tc.want)
			}
		})
	}
	for _, status := range []domain.ReviewRunStatus{domain.ReviewRunComplete, domain.ReviewRunFailed} {
		if changed, err := s.UpdateReviewRunResult(ctx, run.ID, status, domain.VerdictApproved, input.Body, "", false); err != nil || changed {
			t.Fatalf("generic result bypassed generation fence: %v %v", changed, err)
		}
	}
	claim.ReviewerLaunchID = "replacement"
	if err := s.UpsertReview(ctx, claim); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitTaskReviewResult(ctx, input); !errors.Is(err, ports.ErrTaskLeaseFenced) {
		t.Fatalf("old native generation accepted after ownership changed: %v", err)
	}
	claim.ReviewerLaunchID = frozen.LaunchID
	if err := s.UpsertReview(ctx, claim); err != nil {
		t.Fatal(err)
	}
	// A fast native reply can precede handle persistence. It is retained, but
	// independent evaluation still requires the separate launch witness.
	var wg sync.WaitGroup
	created := make(chan bool, 8)
	for range 8 {
		wg.Go(func() {
			got, fresh, err := s.SubmitTaskReviewResult(ctx, input)
			if err != nil || got.Verdict != input.Verdict {
				t.Errorf("concurrent submission: %+v %v", got, err)
			}
			created <- fresh
		})
	}
	wg.Wait()
	close(created)
	count := 0
	for fresh := range created {
		if fresh {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("result transitions = %d, want 1", count)
	}
	retained, _, err := s.GetTaskReviewContext(ctx, run.ID)
	if err != nil || retained.StartedAt != nil {
		t.Fatalf("invented launch witness: %+v %v", retained, err)
	}
	if _, err := s.MarkReviewRunDelivered(ctx, run.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	claim.ReviewerLaunchID = "replacement"
	if err := s.UpsertReview(ctx, claim); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got, fresh, err := reopened.SubmitTaskReviewResult(ctx, input); err != nil || fresh || got.Status != domain.ReviewRunDelivered {
		t.Fatalf("historical exact retry after reopen: %+v %v %v", got, fresh, err)
	}
	for _, change := range []func(*domain.TaskReviewSubmission){
		func(v *domain.TaskReviewSubmission) { v.Body = "" },
		func(v *domain.TaskReviewSubmission) { v.GithubReviewID = "" },
		func(v *domain.TaskReviewSubmission) { v.Verdict = domain.VerdictChangesRequested },
	} {
		bad := input
		change(&bad)
		if _, _, err := reopened.SubmitTaskReviewResult(ctx, bad); !errors.Is(err, ports.ErrTaskConflict) {
			t.Fatalf("changed delivered result accepted: %v", err)
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM adaptive_task_audit WHERE action='review_submitted'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("submission audits = %d %v", count, err)
	}
}

func TestTaskReviewSubmissionAtomicAuditAndServiceBoundary(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := sqlitetest.MustOpenAt(t, dir)
	_, run, frozen := taskReviewFixture(t, s)
	if err := s.InsertTaskReviewRun(ctx, run, frozen); err != nil {
		t.Fatal(err)
	}
	claim := domain.Review{ID: run.ReviewID, SessionID: run.SessionID, ProjectID: "project", Harness: run.Harness, ReviewerLaunchID: frozen.LaunchID, CreatedAt: run.CreatedAt, UpdatedAt: time.Now().UTC()}
	if err := s.UpsertReview(ctx, claim); err != nil {
		t.Fatal(err)
	}
	svc := reviewsvc.New(nil, s)
	input := reviewsvc.SubmittedReview{RunID: run.ID, Verdict: domain.VerdictApproved, Body: "Independent findings"}
	if _, err := svc.SubmitMany(ctx, run.SessionID, []reviewsvc.SubmittedReview{input}); !errors.Is(err, reviewsvc.ErrInvalid) {
		t.Fatalf("unfenced service submission: %v", err)
	}
	input.SourceGeneration = frozen.LaunchID
	db, err := sql.Open("sqlite", filepath.Join(dir, "ao.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE TRIGGER reject_review_submission BEFORE INSERT ON adaptive_task_audit WHEN NEW.action='review_submitted' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitMany(ctx, run.SessionID, []reviewsvc.SubmittedReview{input}); err == nil {
		t.Fatal("missing audit accepted")
	}
	got, _, err := s.GetReviewRun(ctx, run.ID)
	if err != nil || got.Status != domain.ReviewRunRunning || got.Body != "" {
		t.Fatalf("partial result survived rollback: %+v %v", got, err)
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_review_submission"); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.SubmitMany(ctx, run.SessionID, []reviewsvc.SubmittedReview{input}); err != nil || len(got) != 1 || got[0].Status != domain.ReviewRunComplete {
		t.Fatalf("native result rejected: %+v %v", got, err)
	}
	input.Body = "changed"
	if _, err := svc.SubmitMany(ctx, run.SessionID, []reviewsvc.SubmittedReview{input}); !errors.Is(err, reviewsvc.ErrInvalid) {
		t.Fatalf("changed result accepted: %v", err)
	}
}
