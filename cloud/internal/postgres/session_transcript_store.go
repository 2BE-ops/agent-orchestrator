package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/cloud/internal/domain"
	"github.com/jackc/pgx/v5"
)

// PutSessionTranscript upserts the latest captured harness transcript and
// preserved git ref for a session. It is the worker capture path: the worker
// periodically pushes its transcript blob so a later RESTORE can rehydrate a
// fresh sandbox under the same session_id. Scoped to the worker's own
// organization (withOrg), so row-level security still confines the write to one
// tenant even though there is no user principal on this path.
func (s *Store) PutSessionTranscript(
	ctx context.Context,
	orgID, sessionID, agentSessionID, harness string,
	transcript []byte,
	preservedGitRef string,
) error {
	return s.withOrg(ctx, orgID, func(tx pgx.Tx) error {
		// INSERT ... SELECT FROM ao_sessions gates the write on the session
		// still existing, so a capture for a hard-deleted session reports
		// ErrNotFound instead of tripping the foreign key as an opaque error.
		tag, err := tx.Exec(
			ctx,
			`INSERT INTO ao_session_transcripts (
				org_id, session_id, agent_session_id, harness,
				transcript, preserved_git_ref, captured_at, updated_at
			)
			SELECT $1, $2, $3, $4, $5, $6, now(), now()
			FROM ao_sessions
			WHERE id = $2 AND org_id = $1
			ON CONFLICT (org_id, session_id) DO UPDATE
			SET agent_session_id = EXCLUDED.agent_session_id,
				harness = EXCLUDED.harness,
				transcript = EXCLUDED.transcript,
				preserved_git_ref = EXCLUDED.preserved_git_ref,
				captured_at = now(),
				updated_at = now()`,
			orgID, sessionID, agentSessionID, harness, transcript, preservedGitRef,
		)
		if err != nil {
			return fmt.Errorf("put session transcript: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// GetSessionTranscript returns the most recent captured transcript for a
// session, or ErrNotFound when none has been captured yet.
func (s *Store) GetSessionTranscript(
	ctx context.Context,
	orgID, sessionID string,
) (agentSessionID, harness string, transcript []byte, preservedGitRef string, err error) {
	err = s.withOrg(ctx, orgID, func(tx pgx.Tx) error {
		scanErr := tx.QueryRow(
			ctx,
			`SELECT agent_session_id, harness, transcript, preserved_git_ref
			FROM ao_session_transcripts
			WHERE org_id = $1 AND session_id = $2`,
			orgID, sessionID,
		).Scan(&agentSessionID, &harness, &transcript, &preservedGitRef)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if scanErr != nil {
			return fmt.Errorf("get session transcript: %w", scanErr)
		}
		return nil
	})
	return agentSessionID, harness, transcript, preservedGitRef, err
}

// RestoreSession reverses a cloud delete: it un-terminates the SAME session_id
// and queues a fresh sandbox provision, mirroring SetSandboxDesiredState's
// 'running' intent. Preserving the session_id keeps parent_session_id links
// valid so a restored orchestrator re-discovers its workers. Runs in one tenant
// transaction so a caller never observes a half-restored session.
func (s *Store) RestoreSession(
	ctx context.Context,
	principal domain.Principal,
	orgID, sessionID string,
) error {
	return s.withTenant(ctx, principal, orgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(
			ctx,
			`UPDATE ao_sessions
			SET is_terminated = false,
				activity_state = 'idle',
				updated_at = now()
			WHERE id = $1 AND org_id = $2`,
			sessionID, orgID,
		)
		if err != nil {
			// A live orchestrator already occupies this project (the
			// one-active-orchestrator unique index) surfaces as ErrConflict.
			return normalizeConstraintError(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		// Re-arm the sandbox for reconciliation. A deleted sandbox row survives
		// with observed_state='deleted' and reconcile_after ~100 years out, so
		// resetting desired_state, the startup window, and the deletion/failure
		// artifacts is what lets the reconciler pick it up and provision anew.
		if _, err := tx.Exec(
			ctx,
			`UPDATE ao_sandboxes
			SET desired_state = 'running',
				startup_started_at = now(),
				reconcile_after = now(),
				deletion_requested_at = NULL,
				consecutive_failures = 0,
				startup_attempts = 0,
				last_error = '',
				reconcile_lease_owner = '',
				reconcile_lease_until = NULL,
				updated_at = now()
			WHERE session_id = $1 AND org_id = $2`,
			sessionID, orgID,
		); err != nil {
			return fmt.Errorf("restore session sandbox: %w", err)
		}
		return nil
	})
}

// SessionTranscriptStore adapts *Store to the narrow Put/Get transcript-blob
// abstraction the HTTP layer depends on. Keeping the handlers behind this
// interface lets the Postgres-backed store be swapped for an object-store (S3)
// implementation later without touching the handlers.
type SessionTranscriptStore struct{ store *Store }

// SessionTranscripts returns the Postgres-backed transcript blob store.
func (s *Store) SessionTranscripts() *SessionTranscriptStore {
	return &SessionTranscriptStore{store: s}
}

// Put stores the latest captured transcript for a session.
func (t *SessionTranscriptStore) Put(
	ctx context.Context,
	orgID, sessionID, agentSessionID, harness string,
	transcript []byte,
	preservedGitRef string,
) error {
	return t.store.PutSessionTranscript(
		ctx, orgID, sessionID, agentSessionID, harness, transcript, preservedGitRef,
	)
}

// Get returns the latest captured transcript for a session, or ErrNotFound.
func (t *SessionTranscriptStore) Get(
	ctx context.Context,
	orgID, sessionID string,
) (agentSessionID, harness string, transcript []byte, preservedGitRef string, err error) {
	return t.store.GetSessionTranscript(ctx, orgID, sessionID)
}
