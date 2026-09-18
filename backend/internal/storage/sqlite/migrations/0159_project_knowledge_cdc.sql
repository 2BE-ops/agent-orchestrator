-- Add a vocabulary value without rebuilding the existing CDC log or triggers.
-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema=ON;
UPDATE sqlite_schema
SET sql = replace(sql, '''adaptive_task_changed''', '''adaptive_task_changed'', ''project_knowledge_changed''')
WHERE type='table' AND name='change_log' AND sql NOT LIKE '%''project_knowledge_changed''%';
PRAGMA writable_schema=RESET;
CREATE TEMP TABLE project_knowledge_cdc_guard(valid INTEGER CHECK(valid=1));
INSERT INTO project_knowledge_cdc_guard SELECT COUNT(*) FROM sqlite_schema
WHERE type='table' AND name='change_log' AND sql LIKE '%''project_knowledge_changed''%';
DROP TABLE project_knowledge_cdc_guard;
-- +goose StatementEnd

-- +goose Down
-- Retained events continue to use the additive vocabulary until retention.
SELECT 1;
