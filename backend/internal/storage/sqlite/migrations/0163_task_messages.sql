-- +goose Up
CREATE TABLE adaptive_task_messages (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    project_id TEXT NOT NULL REFERENCES projects(id),
    task_id TEXT NOT NULL REFERENCES adaptive_tasks(id),
    attempt_id TEXT NOT NULL REFERENCES adaptive_task_contexts(attempt_id),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    native_generation TEXT NOT NULL CHECK(length(native_generation)>0),
    source_owner TEXT NOT NULL CHECK(json_valid(source_owner)),
    task_revision INTEGER NOT NULL,
    criteria_version INTEGER NOT NULL,
    configuration_hash TEXT NOT NULL CHECK(length(configuration_hash)=64),
    configuration_sequence INTEGER NOT NULL CHECK(configuration_sequence>=0),
    context_hash TEXT NOT NULL CHECK(length(context_hash)=64),
    target_task_id TEXT NOT NULL REFERENCES adaptive_tasks(id),
    correlation_id TEXT NOT NULL,
    reply_to_id TEXT REFERENCES adaptive_task_messages(id),
    result_id TEXT REFERENCES adaptive_task_results(id),
    idempotency_key TEXT NOT NULL CHECK(length(idempotency_key) BETWEEN 1 AND 200),
    definition TEXT NOT NULL CHECK(json_valid(definition) AND length(definition)<=32768),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    UNIQUE(attempt_id,idempotency_key),
    FOREIGN KEY(task_id,task_revision) REFERENCES adaptive_task_revisions(task_id,number),
    FOREIGN KEY(task_id,criteria_version) REFERENCES adaptive_task_criteria(task_id,number)
);
CREATE INDEX adaptive_task_messages_project ON adaptive_task_messages(project_id,sequence);
CREATE INDEX adaptive_task_messages_target ON adaptive_task_messages(target_task_id,sequence);
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_message_immutable BEFORE UPDATE ON adaptive_task_messages
BEGIN SELECT RAISE(ABORT,'task message history is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_message_no_delete BEFORE DELETE ON adaptive_task_messages
WHEN EXISTS(SELECT 1 FROM adaptive_task_attempts WHERE id=OLD.attempt_id)
BEGIN SELECT RAISE(ABORT,'message belongs to retained attempt'); END;
-- +goose StatementEnd

CREATE TABLE adaptive_task_message_deliveries (
    id TEXT PRIMARY KEY NOT NULL,
    message_id TEXT NOT NULL REFERENCES adaptive_task_messages(id),
    number INTEGER NOT NULL CHECK(number BETWEEN 1 AND 4),
    target_attempt_id TEXT NOT NULL REFERENCES adaptive_task_contexts(attempt_id),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    owner TEXT NOT NULL CHECK(json_valid(owner)),
    delivery_key TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('dispatching','handed_off','not_sent','uncertain')),
    reason TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE(message_id,number)
);
CREATE UNIQUE INDEX adaptive_task_message_delivery_exclusive ON adaptive_task_message_deliveries(message_id) WHERE state!='not_sent';
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_message_delivery_guard BEFORE UPDATE ON adaptive_task_message_deliveries
WHEN OLD.state!='dispatching' OR NEW.state='dispatching' OR NEW.id!=OLD.id OR NEW.message_id!=OLD.message_id
 OR NEW.number!=OLD.number OR NEW.target_attempt_id!=OLD.target_attempt_id OR NEW.session_id!=OLD.session_id
 OR NEW.owner!=OLD.owner OR NEW.delivery_key!=OLD.delivery_key OR NEW.created_at!=OLD.created_at
BEGIN SELECT RAISE(ABORT,'delivery attribution and terminal outcomes are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_message_delivery_no_delete BEFORE DELETE ON adaptive_task_message_deliveries
BEGIN SELECT RAISE(ABORT,'message delivery history is retained'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER adaptive_task_message_delivery_no_delete;
DROP TABLE adaptive_task_message_deliveries;
DROP TRIGGER adaptive_task_message_no_delete;
DROP TABLE adaptive_task_messages;
