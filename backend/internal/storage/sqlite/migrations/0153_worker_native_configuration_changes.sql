-- +goose Up
CREATE TABLE adaptive_worker_native_changes (
    id TEXT PRIMARY KEY NOT NULL,
    session_id TEXT NOT NULL REFERENCES adaptive_worker_configurations(session_id) ON DELETE CASCADE,
    conversation_id TEXT NOT NULL REFERENCES conversations(id),
    owner TEXT NOT NULL CHECK(json_valid(owner)),
    previous_activation INTEGER NOT NULL CHECK(previous_activation >= 0),
    previous_options TEXT NOT NULL CHECK(json_valid(previous_options) AND length(previous_options) <= 262144),
    requested TEXT NOT NULL CHECK(json_valid(requested) AND length(requested) <= 8192),
    created_at DATETIME NOT NULL
);
CREATE TABLE adaptive_worker_native_resolutions (
    change_id TEXT PRIMARY KEY NOT NULL REFERENCES adaptive_worker_native_changes(id) ON DELETE CASCADE,
    outcome TEXT NOT NULL CHECK(outcome IN ('applied','reverted')),
    execution_id TEXT REFERENCES adaptive_worker_executions(id),
    reason TEXT NOT NULL CHECK(length(reason) <= 2000),
    created_at DATETIME NOT NULL
);
CREATE INDEX adaptive_worker_native_session ON adaptive_worker_native_changes(session_id);
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_native_one_pending BEFORE INSERT ON adaptive_worker_native_changes
WHEN EXISTS(SELECT 1 FROM adaptive_worker_native_changes c WHERE c.session_id=NEW.session_id AND NOT EXISTS(SELECT 1 FROM adaptive_worker_native_resolutions r WHERE r.change_id=c.id))
BEGIN SELECT RAISE(ABORT, 'native configuration change is unresolved'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_native_change_immutable BEFORE UPDATE ON adaptive_worker_native_changes
BEGIN SELECT RAISE(ABORT, 'native configuration intent is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_native_resolution_immutable BEFORE UPDATE ON adaptive_worker_native_resolutions
BEGIN SELECT RAISE(ABORT, 'native configuration resolution is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_native_change_no_delete BEFORE DELETE ON adaptive_worker_native_changes
WHEN EXISTS(SELECT 1 FROM sessions WHERE id=OLD.session_id)
BEGIN SELECT RAISE(ABORT, 'native configuration intent belongs to retained session'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_native_resolution_no_delete BEFORE DELETE ON adaptive_worker_native_resolutions
WHEN EXISTS(SELECT 1 FROM adaptive_worker_native_changes c JOIN sessions s ON s.id=c.session_id WHERE c.id=OLD.change_id)
BEGIN SELECT RAISE(ABORT, 'native configuration resolution belongs to retained session'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_native_change_cdc AFTER INSERT ON adaptive_worker_native_changes
BEGIN
    INSERT INTO change_log(project_id,session_id,event_type,payload,created_at)
    SELECT NULLIF(project_id,''),id,'session_updated',json_object('workerNativeChangeId',NEW.id),NEW.created_at FROM sessions WHERE id=NEW.session_id;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_native_resolution_cdc AFTER INSERT ON adaptive_worker_native_resolutions
BEGIN
    INSERT INTO change_log(project_id,session_id,event_type,payload,created_at)
    SELECT NULLIF(s.project_id,''),s.id,'session_updated',json_object('workerNativeChangeId',NEW.change_id),NEW.created_at FROM sessions s JOIN adaptive_worker_native_changes c ON c.session_id=s.id WHERE c.id=NEW.change_id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE adaptive_worker_native_resolutions;
DROP TABLE adaptive_worker_native_changes;
