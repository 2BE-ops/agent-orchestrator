-- Summary: derive the active Codex account from the device credential only.
-- +goose Up
DROP TRIGGER IF EXISTS codex_active_account_cdc_insert;
DROP TRIGGER IF EXISTS codex_active_account_cdc_update;
DROP TRIGGER IF EXISTS codex_active_account_cdc_delete;
DROP TABLE IF EXISTS codex_active_account;

ALTER TABLE codex_account_switches DROP COLUMN expected_account_revision;

-- +goose Down
-- +goose StatementBegin
-- The removed active pointer and revision admission are intentionally not
-- restored. Device auth.json remains the only active-account authority.
SELECT 1;
-- +goose StatementEnd
