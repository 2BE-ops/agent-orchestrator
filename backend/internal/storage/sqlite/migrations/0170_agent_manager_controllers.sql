-- +goose Up
CREATE TABLE adaptive_agent_manager_controllers (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES adaptive_agent_managers(project_id) ON DELETE CASCADE,
    configuration_version INTEGER NOT NULL,
    actor TEXT NOT NULL CHECK(json_valid(actor)),
    reason TEXT NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 2000),
    created_at DATETIME NOT NULL,
    released_at DATETIME,
    release_reason TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(project_id,configuration_version) REFERENCES adaptive_agent_manager_configurations(project_id,number),
    CHECK((released_at IS NULL AND release_reason='') OR (released_at IS NOT NULL AND length(trim(release_reason)) BETWEEN 1 AND 2000))
);
CREATE UNIQUE INDEX adaptive_agent_manager_controller_exclusive ON adaptive_agent_manager_controllers(project_id) WHERE released_at IS NULL;
CREATE INDEX adaptive_agent_manager_controller_history ON adaptive_agent_manager_controllers(project_id,id);
CREATE TABLE adaptive_agent_manager_dispatches (
    controller_id TEXT PRIMARY KEY NOT NULL REFERENCES adaptive_agent_manager_controllers(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL UNIQUE REFERENCES sessions(id),
    configuration_hash TEXT NOT NULL CHECK(length(configuration_hash)=64),
    created_at DATETIME NOT NULL
);
CREATE TABLE adaptive_agent_manager_execution_operations (
    id TEXT PRIMARY KEY NOT NULL,
    controller_id TEXT NOT NULL REFERENCES adaptive_agent_manager_controllers(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    source_owner TEXT NOT NULL CHECK(json_valid(source_owner)),
    kind TEXT NOT NULL CHECK(kind IN ('dispatch','restore')),
    created_at DATETIME NOT NULL
);
CREATE INDEX adaptive_agent_manager_execution_session ON adaptive_agent_manager_execution_operations(session_id);
CREATE TABLE adaptive_agent_manager_execution_resolutions (
    operation_id TEXT PRIMARY KEY NOT NULL REFERENCES adaptive_agent_manager_execution_operations(id) ON DELETE CASCADE,
    observed_owner TEXT NOT NULL CHECK(json_valid(observed_owner)),
    outcome TEXT NOT NULL CHECK(outcome IN ('connected','terminated')),
    reason TEXT NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 2000),
    created_at DATETIME NOT NULL
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_agent_manager_controller_immutable BEFORE UPDATE ON adaptive_agent_manager_controllers
WHEN NEW.id<>OLD.id OR NEW.project_id<>OLD.project_id OR NEW.configuration_version<>OLD.configuration_version
 OR NEW.actor<>OLD.actor OR NEW.reason<>OLD.reason OR NEW.created_at<>OLD.created_at
 OR OLD.released_at IS NOT NULL OR NEW.released_at IS NULL
BEGIN SELECT RAISE(ABORT,'Manager admission is immutable except confirmed release'); END;
CREATE TRIGGER adaptive_agent_manager_controller_retained BEFORE DELETE ON adaptive_agent_manager_controllers
WHEN EXISTS(SELECT 1 FROM adaptive_agent_managers WHERE project_id=OLD.project_id)
BEGIN SELECT RAISE(ABORT,'controller belongs to retained Manager'); END;
CREATE TRIGGER adaptive_agent_manager_dispatch_immutable BEFORE UPDATE ON adaptive_agent_manager_dispatches
BEGIN SELECT RAISE(ABORT,'Manager dispatch is immutable'); END;
CREATE TRIGGER adaptive_agent_manager_dispatch_retained BEFORE DELETE ON adaptive_agent_manager_dispatches
WHEN EXISTS(SELECT 1 FROM adaptive_agent_manager_controllers WHERE id=OLD.controller_id)
BEGIN SELECT RAISE(ABORT,'dispatch belongs to retained controller'); END;
CREATE TRIGGER adaptive_agent_manager_execution_immutable BEFORE UPDATE ON adaptive_agent_manager_execution_operations
BEGIN SELECT RAISE(ABORT,'Manager execution is immutable'); END;
CREATE TRIGGER adaptive_agent_manager_execution_retained BEFORE DELETE ON adaptive_agent_manager_execution_operations
WHEN EXISTS(SELECT 1 FROM adaptive_agent_manager_controllers WHERE id=OLD.controller_id)
BEGIN SELECT RAISE(ABORT,'execution belongs to retained controller'); END;
CREATE TRIGGER adaptive_agent_manager_resolution_immutable BEFORE UPDATE ON adaptive_agent_manager_execution_resolutions
BEGIN SELECT RAISE(ABORT,'Manager execution resolution is immutable'); END;
CREATE TRIGGER adaptive_agent_manager_resolution_retained BEFORE DELETE ON adaptive_agent_manager_execution_resolutions
WHEN EXISTS(SELECT 1 FROM adaptive_agent_manager_execution_operations WHERE id=OLD.operation_id)
BEGIN SELECT RAISE(ABORT,'resolution belongs to retained execution'); END;
CREATE TRIGGER adaptive_agent_manager_execution_exclusive BEFORE INSERT ON adaptive_agent_manager_execution_operations
WHEN EXISTS(SELECT 1 FROM adaptive_agent_manager_execution_operations o WHERE o.controller_id=NEW.controller_id
 AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_execution_resolutions r WHERE r.operation_id=o.id))
BEGIN SELECT RAISE(ABORT,'Manager native execution is unresolved'); END;
CREATE TRIGGER adaptive_agent_manager_release_fence BEFORE UPDATE OF released_at ON adaptive_agent_manager_controllers
WHEN NEW.released_at IS NOT NULL AND EXISTS(SELECT 1 FROM adaptive_agent_manager_execution_operations o WHERE o.controller_id=OLD.id
 AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_execution_resolutions r WHERE r.operation_id=o.id))
BEGIN SELECT RAISE(ABORT,'Manager native execution must be reconciled before release'); END;
CREATE TRIGGER adaptive_agent_manager_restore_fence BEFORE UPDATE OF is_terminated ON sessions
WHEN OLD.is_terminated=1 AND NEW.is_terminated=0
 AND EXISTS(SELECT 1 FROM adaptive_agent_manager_dispatches d WHERE d.session_id=NEW.id)
 AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_dispatches d
 JOIN adaptive_agent_manager_controllers c ON c.id=d.controller_id AND c.released_at IS NULL
 JOIN adaptive_agent_manager_execution_operations o ON o.controller_id=c.id AND o.session_id=NEW.id
 WHERE d.session_id=NEW.id AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_execution_resolutions r WHERE r.operation_id=o.id))
BEGIN SELECT RAISE(ABORT,'Manager restoration requires reserved execution'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE manager_controller_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO manager_controller_history_guard SELECT count(*) FROM adaptive_agent_manager_controllers;
DROP TABLE manager_controller_history_guard;
DROP TRIGGER adaptive_agent_manager_restore_fence;
DROP TRIGGER adaptive_agent_manager_release_fence;
DROP TABLE adaptive_agent_manager_execution_resolutions;
DROP TABLE adaptive_agent_manager_execution_operations;
DROP TABLE adaptive_agent_manager_dispatches;
DROP TABLE adaptive_agent_manager_controllers;
-- +goose StatementEnd
