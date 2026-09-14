-- Summary: derive the active Codex account from the device credential only.
-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA foreign_keys=OFF;

DROP TRIGGER IF EXISTS codex_active_account_cdc_insert;
DROP TRIGGER IF EXISTS codex_active_account_cdc_update;
DROP TRIGGER IF EXISTS codex_active_account_cdc_delete;
DROP TABLE IF EXISTS codex_active_account;

DROP TRIGGER IF EXISTS codex_account_switches_cdc_update;
DROP TRIGGER IF EXISTS codex_account_switches_cdc_insert;
DROP INDEX IF EXISTS idx_codex_account_switches_one_active;

CREATE TABLE codex_account_switches_next (
    id TEXT PRIMARY KEY,
    source_account_id TEXT NOT NULL,
    target_account_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    request_fingerprint TEXT NOT NULL,
    phase TEXT NOT NULL CHECK (phase IN (
        'requested', 'checkpointing_source', 'activating_target',
        'recovery_required', 'completed', 'failed'
    )),
    failure_code TEXT NOT NULL DEFAULT '',
    credentials_committed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    completed_at TIMESTAMP,
    source_kind TEXT NOT NULL DEFAULT 'managed'
        CHECK (source_kind IN ('managed', 'device', 'none'))
);

INSERT INTO codex_account_switches_next (
    id, source_account_id, target_account_id, idempotency_key,
    request_fingerprint, phase, failure_code, credentials_committed_at,
    created_at, updated_at, completed_at, source_kind
)
SELECT
    id, source_account_id, target_account_id, idempotency_key,
    request_fingerprint, phase, failure_code, credentials_committed_at,
    created_at, updated_at, completed_at, source_kind
FROM codex_account_switches;

DROP TABLE codex_account_switches;
ALTER TABLE codex_account_switches_next RENAME TO codex_account_switches;

CREATE UNIQUE INDEX idx_codex_account_switches_one_active
ON codex_account_switches((1))
WHERE phase NOT IN ('completed', 'failed');

PRAGMA foreign_keys=ON;
PRAGMA foreign_key_check;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- The removed active pointer and revision admission are intentionally not
-- restored. Device auth.json remains the only active-account authority.
SELECT 1;
-- +goose StatementEnd
