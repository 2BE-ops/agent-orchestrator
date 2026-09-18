-- Preserve the existing change log and triggers, following 0148's additive
-- vocabulary migration. The guard fails rather than silently omitting events.
-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema=ON;
UPDATE sqlite_schema
SET sql = replace(sql, '''registry_changed''', '''registry_changed'', ''adaptive_task_changed''')
WHERE type = 'table' AND name = 'change_log' AND sql NOT LIKE '%''adaptive_task_changed''%';
PRAGMA writable_schema=RESET;
CREATE TEMP TABLE adaptive_task_cdc_guard (valid INTEGER CHECK (valid = 1));
INSERT INTO adaptive_task_cdc_guard
SELECT COUNT(*) FROM sqlite_schema
WHERE type = 'table' AND name = 'change_log' AND sql LIKE '%''adaptive_task_changed''%';
DROP TABLE adaptive_task_cdc_guard;
-- +goose StatementEnd

-- +goose Down
-- Retained events continue to use the additive vocabulary until retention.
SELECT 1;
