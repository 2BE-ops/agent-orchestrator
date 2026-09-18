-- +goose Up
CREATE TABLE adaptive_task_attempts (
    id TEXT PRIMARY KEY NOT NULL,
    task_id TEXT NOT NULL REFERENCES adaptive_tasks(id) ON DELETE CASCADE,
    task_revision INTEGER NOT NULL,
    criteria_version INTEGER NOT NULL,
    number INTEGER NOT NULL CHECK(number >= 1),
    launch_intent_id TEXT NOT NULL UNIQUE,
    dependencies TEXT NOT NULL CHECK(json_valid(dependencies) AND length(dependencies) <= 65536),
    actor TEXT NOT NULL CHECK(json_valid(actor)),
    reason TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    UNIQUE(task_id,number),
    UNIQUE(id,task_id,number),
    FOREIGN KEY(task_id,task_revision) REFERENCES adaptive_task_revisions(task_id,number),
    FOREIGN KEY(task_id,criteria_version) REFERENCES adaptive_task_criteria(task_id,number)
);
CREATE TABLE adaptive_task_leases (
    attempt_id TEXT PRIMARY KEY NOT NULL,
    task_id TEXT NOT NULL,
    generation INTEGER NOT NULL,
    holder_id TEXT NOT NULL,
    heartbeat_at DATETIME NOT NULL,
    last_activity_at DATETIME,
    expires_at DATETIME NOT NULL,
    released_at DATETIME,
    release_reason TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(attempt_id,task_id,generation) REFERENCES adaptive_task_attempts(id,task_id,number) ON DELETE CASCADE
);
CREATE UNIQUE INDEX adaptive_task_exclusive_lease ON adaptive_task_leases(task_id) WHERE released_at IS NULL;
CREATE INDEX adaptive_task_lease_expiry ON adaptive_task_leases(expires_at) WHERE released_at IS NULL;
CREATE TABLE adaptive_task_dispatches (
    attempt_id TEXT PRIMARY KEY NOT NULL REFERENCES adaptive_task_attempts(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL UNIQUE REFERENCES sessions(id),
    configuration_hash TEXT NOT NULL CHECK(length(configuration_hash)=64),
    created_at DATETIME NOT NULL
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_attempt_immutable BEFORE UPDATE ON adaptive_task_attempts
BEGIN SELECT RAISE(ABORT,'task attempt is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_attempt_no_delete BEFORE DELETE ON adaptive_task_attempts
WHEN EXISTS(SELECT 1 FROM adaptive_tasks WHERE id=OLD.task_id)
BEGIN SELECT RAISE(ABORT,'attempt belongs to retained task'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_dispatch_immutable BEFORE UPDATE ON adaptive_task_dispatches
BEGIN SELECT RAISE(ABORT,'task worker association is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_dispatch_no_delete BEFORE DELETE ON adaptive_task_dispatches
WHEN EXISTS(SELECT 1 FROM adaptive_task_attempts WHERE id=OLD.attempt_id)
BEGIN SELECT RAISE(ABORT,'dispatch belongs to retained attempt'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_lease_fence BEFORE UPDATE ON adaptive_task_leases
WHEN OLD.attempt_id <> NEW.attempt_id OR OLD.task_id <> NEW.task_id OR OLD.generation <> NEW.generation OR OLD.released_at IS NOT NULL
BEGIN SELECT RAISE(ABORT,'lease identity and release are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_lease_no_delete BEFORE DELETE ON adaptive_task_leases
WHEN EXISTS(SELECT 1 FROM adaptive_task_attempts WHERE id=OLD.attempt_id)
BEGIN SELECT RAISE(ABORT,'lease belongs to retained attempt'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER adaptive_task_dispatch_no_delete;
DROP TRIGGER adaptive_task_lease_no_delete;
DROP TRIGGER adaptive_task_attempt_no_delete;
DROP TABLE adaptive_task_dispatches;
DROP TABLE adaptive_task_leases;
DROP TABLE adaptive_task_attempts;
