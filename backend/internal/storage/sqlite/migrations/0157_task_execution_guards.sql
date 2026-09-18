-- +goose Up
CREATE TABLE adaptive_task_execution_operations (
    id TEXT PRIMARY KEY NOT NULL,
    attempt_id TEXT NOT NULL REFERENCES adaptive_task_attempts(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    generation INTEGER NOT NULL,
    holder_id TEXT NOT NULL,
    source_owner TEXT NOT NULL CHECK(json_valid(source_owner)),
    kind TEXT NOT NULL CHECK(kind IN ('dispatch','restore')),
    created_at DATETIME NOT NULL
);
CREATE INDEX adaptive_task_execution_session ON adaptive_task_execution_operations(session_id);
CREATE TABLE adaptive_task_execution_resolutions (
    operation_id TEXT PRIMARY KEY NOT NULL REFERENCES adaptive_task_execution_operations(id) ON DELETE CASCADE,
    observed_owner TEXT NOT NULL CHECK(json_valid(observed_owner)),
    outcome TEXT NOT NULL CHECK(outcome IN ('connected','terminated')),
    reason TEXT NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 2000),
    created_at DATETIME NOT NULL
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_execution_exclusive BEFORE INSERT ON adaptive_task_execution_operations
WHEN EXISTS (SELECT 1 FROM adaptive_task_execution_operations o
    WHERE o.attempt_id=NEW.attempt_id AND NOT EXISTS(SELECT 1 FROM adaptive_task_execution_resolutions r WHERE r.operation_id=o.id))
BEGIN SELECT RAISE(ABORT,'native task execution is unresolved'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_execution_immutable BEFORE UPDATE ON adaptive_task_execution_operations
BEGIN SELECT RAISE(ABORT,'task execution operation is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_execution_resolution_immutable BEFORE UPDATE ON adaptive_task_execution_resolutions
BEGIN SELECT RAISE(ABORT,'task execution resolution is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_execution_no_delete BEFORE DELETE ON adaptive_task_execution_operations
WHEN EXISTS(SELECT 1 FROM adaptive_task_attempts WHERE id=OLD.attempt_id)
BEGIN SELECT RAISE(ABORT,'native execution belongs to retained attempt'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_execution_resolution_no_delete BEFORE DELETE ON adaptive_task_execution_resolutions
WHEN EXISTS(SELECT 1 FROM adaptive_task_execution_operations WHERE id=OLD.operation_id)
BEGIN SELECT RAISE(ABORT,'resolution belongs to retained native execution'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_lease_execution_fence BEFORE UPDATE OF released_at,holder_id ON adaptive_task_leases
WHEN (NEW.released_at IS NOT NULL OR OLD.holder_id <> NEW.holder_id)
 AND EXISTS(SELECT 1 FROM adaptive_task_execution_operations o WHERE o.attempt_id=OLD.attempt_id
    AND NOT EXISTS(SELECT 1 FROM adaptive_task_execution_resolutions r WHERE r.operation_id=o.id))
BEGIN SELECT RAISE(ABORT,'native task execution must be reconciled before release'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_worker_restore_fence BEFORE UPDATE OF is_terminated ON sessions
WHEN OLD.is_terminated=1 AND NEW.is_terminated=0
 AND EXISTS(SELECT 1 FROM adaptive_task_dispatches d WHERE d.session_id=NEW.id)
 AND NOT EXISTS(SELECT 1 FROM adaptive_task_dispatches d
    JOIN adaptive_task_leases l ON l.attempt_id=d.attempt_id AND l.released_at IS NULL
    JOIN adaptive_task_execution_operations o ON o.attempt_id=l.attempt_id AND o.session_id=NEW.id
    WHERE d.session_id=NEW.id AND NOT EXISTS(SELECT 1 FROM adaptive_task_execution_resolutions r WHERE r.operation_id=o.id))
BEGIN SELECT RAISE(ABORT,'task worker restoration requires reserved execution'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER adaptive_task_lease_execution_fence;
DROP TRIGGER adaptive_task_worker_restore_fence;
DROP TRIGGER adaptive_task_execution_resolution_no_delete;
DROP TRIGGER adaptive_task_execution_no_delete;
DROP TABLE adaptive_task_execution_resolutions;
DROP TABLE adaptive_task_execution_operations;
