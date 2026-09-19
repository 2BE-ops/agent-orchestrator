-- +goose Up
CREATE TABLE adaptive_task_evaluations (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    task_id TEXT NOT NULL REFERENCES adaptive_tasks(id),
    attempt_id TEXT NOT NULL REFERENCES adaptive_task_attempts(id),
    result_id TEXT NOT NULL REFERENCES adaptive_task_results(id),
    number INTEGER NOT NULL CHECK(number BETWEEN 1 AND 64),
    task_revision INTEGER NOT NULL,
    criteria_version INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL CHECK(length(idempotency_key) BETWEEN 1 AND 200),
    request_hash TEXT NOT NULL CHECK(length(request_hash)=64),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(snapshot)<=524288),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    UNIQUE(attempt_id,number),
    UNIQUE(attempt_id,idempotency_key),
    FOREIGN KEY(task_id,task_revision) REFERENCES adaptive_task_revisions(task_id,number),
    FOREIGN KEY(task_id,criteria_version) REFERENCES adaptive_task_criteria(task_id,number)
);
CREATE INDEX adaptive_task_evaluations_project ON adaptive_task_evaluations(project_id,created_at,id);
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_evaluation_immutable BEFORE UPDATE ON adaptive_task_evaluations
BEGIN SELECT RAISE(ABORT,'task evaluations are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_evaluation_no_delete BEFORE DELETE ON adaptive_task_evaluations
WHEN EXISTS(SELECT 1 FROM adaptive_task_attempts WHERE id=OLD.attempt_id)
BEGIN SELECT RAISE(ABORT,'evaluation belongs to retained attempt'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER adaptive_task_evaluation_no_delete;
DROP TABLE adaptive_task_evaluations;
