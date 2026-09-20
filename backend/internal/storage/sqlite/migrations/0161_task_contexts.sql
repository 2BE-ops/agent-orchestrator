-- +goose Up
CREATE TABLE adaptive_task_contexts (
    attempt_id TEXT PRIMARY KEY NOT NULL REFERENCES adaptive_task_attempts(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL UNIQUE REFERENCES sessions(id),
    execution_operation_id TEXT NOT NULL REFERENCES adaptive_task_execution_operations(id),
    configuration_hash TEXT NOT NULL CHECK(length(configuration_hash)=64),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(snapshot)<=1048576),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_context_immutable BEFORE UPDATE ON adaptive_task_contexts
BEGIN SELECT RAISE(ABORT,'task context is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_context_no_delete BEFORE DELETE ON adaptive_task_contexts
WHEN EXISTS(SELECT 1 FROM adaptive_task_attempts WHERE id=OLD.attempt_id)
BEGIN SELECT RAISE(ABORT,'context belongs to retained attempt'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER adaptive_task_context_no_delete;
DROP TABLE adaptive_task_contexts;
