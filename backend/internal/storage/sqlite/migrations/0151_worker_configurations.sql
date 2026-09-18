-- +goose Up
CREATE TABLE adaptive_worker_configurations (
    session_id TEXT PRIMARY KEY NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    agent_type_id TEXT NOT NULL,
    agent_type_version INTEGER NOT NULL,
    configuration TEXT NOT NULL CHECK(json_valid(configuration) AND length(configuration) <= 1048576),
    content_hash TEXT NOT NULL CHECK(length(content_hash) = 64),
    created_at DATETIME NOT NULL,
    FOREIGN KEY(agent_type_id, agent_type_version) REFERENCES adaptive_registry_versions(entry_id, number)
);
CREATE INDEX adaptive_worker_type ON adaptive_worker_configurations(agent_type_id,agent_type_version);
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_configuration_immutable BEFORE UPDATE ON adaptive_worker_configurations
BEGIN SELECT RAISE(ABORT, 'worker configuration is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_worker_configuration_no_delete BEFORE DELETE ON adaptive_worker_configurations
WHEN EXISTS(SELECT 1 FROM sessions WHERE id=OLD.session_id)
BEGIN SELECT RAISE(ABORT, 'worker configuration belongs to retained session'); END;
-- +goose StatementEnd

-- +goose Down
-- Refuse to discard retained worker history.
DELETE FROM adaptive_worker_configurations;
DROP TRIGGER adaptive_worker_configuration_no_delete;
DROP TRIGGER adaptive_worker_configuration_immutable;
DROP TABLE adaptive_worker_configurations;
