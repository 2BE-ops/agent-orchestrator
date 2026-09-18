-- Widen the CDC vocabulary without rebuilding the heavily referenced log.
-- Follow 0140's exact schema-edit/reload pattern; preserve all triggers and rows.
-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema=ON;
UPDATE sqlite_schema
SET sql = replace(sql, '''review_run_updated''', '''review_run_updated'', ''registry_changed''')
WHERE type = 'table' AND name = 'change_log' AND sql NOT LIKE '%''registry_changed''%';
PRAGMA writable_schema=RESET;
CREATE TEMP TABLE registry_cdc_guard (valid INTEGER CHECK (valid = 1));
INSERT INTO registry_cdc_guard
SELECT COUNT(*) FROM sqlite_schema
WHERE type = 'table' AND name = 'change_log' AND sql LIKE '%''registry_changed''%';
DROP TABLE registry_cdc_guard;
-- +goose StatementEnd

-- +goose Down
-- Keep the additive event vocabulary on downgrade; retained CDC rows can still
-- contain registry_changed until normal retention removes them.
SELECT 1;
