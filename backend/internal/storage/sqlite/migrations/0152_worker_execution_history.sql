-- +goose Up
CREATE TABLE adaptive_worker_executions (
    id TEXT PRIMARY KEY NOT NULL,
    session_id TEXT NOT NULL REFERENCES adaptive_worker_configurations(session_id) ON DELETE CASCADE,
    source_kind TEXT NOT NULL CHECK(source_kind IN ('interface_transition','agent_switch','conversation_settings')),
    source_id TEXT NOT NULL,
    previous_activation INTEGER NOT NULL CHECK(previous_activation >= 0),
    configuration TEXT NOT NULL CHECK(json_valid(configuration) AND length(configuration) <= 1048576),
    content_hash TEXT NOT NULL CHECK(length(content_hash) = 64),
    origin TEXT NOT NULL CHECK(origin IN ('USER','AGENT_MANAGER','SYSTEM')),
    actor_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    UNIQUE(session_id,source_kind,source_id)
);
CREATE TABLE adaptive_worker_execution_activations (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES adaptive_worker_configurations(session_id) ON DELETE CASCADE,
    execution_id TEXT REFERENCES adaptive_worker_executions(id),
    operation_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('applied','rolled_back')),
    created_at DATETIME NOT NULL,
    UNIQUE(session_id,operation_id,action)
);
CREATE INDEX adaptive_worker_execution_current ON adaptive_worker_execution_activations(session_id,seq DESC);
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_execution_immutable BEFORE UPDATE ON adaptive_worker_executions
BEGIN SELECT RAISE(ABORT, 'worker execution is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_execution_no_delete BEFORE DELETE ON adaptive_worker_executions
WHEN EXISTS(SELECT 1 FROM sessions WHERE id=OLD.session_id)
BEGIN SELECT RAISE(ABORT, 'worker execution belongs to retained session'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_activation_immutable BEFORE UPDATE ON adaptive_worker_execution_activations
BEGIN SELECT RAISE(ABORT, 'worker activation is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_activation_no_delete BEFORE DELETE ON adaptive_worker_execution_activations
WHEN EXISTS(SELECT 1 FROM sessions WHERE id=OLD.session_id)
BEGIN SELECT RAISE(ABORT, 'worker activation belongs to retained session'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_activation_cdc AFTER INSERT ON adaptive_worker_execution_activations
BEGIN
    INSERT INTO change_log(project_id,session_id,event_type,payload,created_at)
    SELECT NULLIF(project_id,''),id,'session_updated',json_object('workerExecutionSequence',NEW.seq),NEW.created_at
    FROM sessions WHERE id=NEW.session_id;
END;
-- +goose StatementEnd

-- +goose Down
DELETE FROM adaptive_worker_execution_activations;
DELETE FROM adaptive_worker_executions;
DROP TRIGGER adaptive_worker_activation_cdc;
DROP TRIGGER adaptive_worker_activation_no_delete;
DROP TRIGGER adaptive_worker_activation_immutable;
DROP TRIGGER adaptive_worker_execution_no_delete;
DROP TRIGGER adaptive_worker_execution_immutable;
DROP TABLE adaptive_worker_execution_activations;
DROP TABLE adaptive_worker_executions;
