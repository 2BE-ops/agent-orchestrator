-- +goose Up
CREATE TABLE adaptive_task_intents (
    task_id TEXT NOT NULL REFERENCES adaptive_tasks(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK(version > 0),
    task_revision INTEGER NOT NULL,
    intent TEXT NOT NULL CHECK(intent IN ('run','cancel')),
    actor TEXT NOT NULL CHECK(json_valid(actor)),
    reason TEXT NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 2000),
    created_at DATETIME NOT NULL,
    PRIMARY KEY(task_id,version),
    FOREIGN KEY(task_id,task_revision) REFERENCES adaptive_task_revisions(task_id,number)
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_intent_immutable BEFORE UPDATE ON adaptive_task_intents
BEGIN SELECT RAISE(ABORT,'task intent history is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_intent_no_delete BEFORE DELETE ON adaptive_task_intents
WHEN EXISTS(SELECT 1 FROM adaptive_tasks WHERE id=OLD.task_id)
BEGIN SELECT RAISE(ABORT,'intent belongs to retained task'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER adaptive_task_intent_no_delete;
DROP TABLE adaptive_task_intents;
