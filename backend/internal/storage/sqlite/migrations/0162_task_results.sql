-- +goose Up
CREATE TABLE adaptive_task_results (
    id TEXT PRIMARY KEY NOT NULL,
    attempt_id TEXT NOT NULL REFERENCES adaptive_task_contexts(attempt_id),
    task_id TEXT NOT NULL REFERENCES adaptive_tasks(id),
    number INTEGER NOT NULL CHECK(number BETWEEN 1 AND 16),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    native_generation TEXT NOT NULL CHECK(length(native_generation)>0),
    source_owner TEXT NOT NULL CHECK(json_valid(source_owner)),
    task_revision INTEGER NOT NULL,
    criteria_version INTEGER NOT NULL,
    configuration_hash TEXT NOT NULL CHECK(length(configuration_hash)=64),
    configuration_sequence INTEGER NOT NULL CHECK(configuration_sequence>=0),
    context_hash TEXT NOT NULL CHECK(length(context_hash)=64),
    idempotency_key TEXT NOT NULL CHECK(length(idempotency_key) BETWEEN 1 AND 200),
    definition TEXT NOT NULL CHECK(json_valid(definition) AND length(definition)<=262144),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    UNIQUE(attempt_id,number),
    UNIQUE(attempt_id,idempotency_key),
    FOREIGN KEY(task_id,task_revision) REFERENCES adaptive_task_revisions(task_id,number),
    FOREIGN KEY(task_id,criteria_version) REFERENCES adaptive_task_criteria(task_id,number)
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_result_immutable BEFORE UPDATE ON adaptive_task_results
BEGIN SELECT RAISE(ABORT,'worker result history is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_result_no_delete BEFORE DELETE ON adaptive_task_results
WHEN EXISTS(SELECT 1 FROM adaptive_task_attempts WHERE id=OLD.attempt_id)
BEGIN SELECT RAISE(ABORT,'result belongs to retained attempt'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER adaptive_task_result_no_delete;
DROP TABLE adaptive_task_results;
