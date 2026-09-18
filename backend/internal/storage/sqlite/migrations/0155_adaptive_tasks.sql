-- +goose Up
CREATE TABLE adaptive_tasks (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL CHECK(revision >= 1),
    parent_id TEXT REFERENCES adaptive_tasks(id) ON DELETE SET NULL,
    created_by TEXT NOT NULL CHECK(json_valid(created_by)),
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE(id,project_id),
    FOREIGN KEY(id,revision) REFERENCES adaptive_task_revisions(task_id,number) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX adaptive_tasks_project ON adaptive_tasks(project_id,id);
CREATE TABLE adaptive_task_criteria (
    task_id TEXT NOT NULL REFERENCES adaptive_tasks(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK(number >= 1),
    previous_version INTEGER,
    definition TEXT NOT NULL CHECK(json_valid(definition) AND length(definition) <= 65536),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    actor TEXT NOT NULL CHECK(json_valid(actor)),
    reason TEXT NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 2000),
    created_at DATETIME NOT NULL,
    PRIMARY KEY(task_id,number),
    FOREIGN KEY(task_id,previous_version) REFERENCES adaptive_task_criteria(task_id,number)
);
CREATE TABLE adaptive_task_revisions (
    task_id TEXT NOT NULL REFERENCES adaptive_tasks(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK(number >= 1),
    criteria_version INTEGER,
    definition TEXT NOT NULL CHECK(json_valid(definition) AND length(definition) <= 65536),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    actor TEXT NOT NULL CHECK(json_valid(actor)),
    reason TEXT NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 2000),
    created_at DATETIME NOT NULL,
    PRIMARY KEY(task_id,number),
    FOREIGN KEY(task_id,criteria_version) REFERENCES adaptive_task_criteria(task_id,number)
);
CREATE TABLE adaptive_task_dependencies (
    project_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    dependency_id TEXT NOT NULL,
    PRIMARY KEY(task_id,dependency_id),
    CHECK(task_id <> dependency_id),
    FOREIGN KEY(task_id,project_id) REFERENCES adaptive_tasks(id,project_id) ON DELETE CASCADE,
    FOREIGN KEY(dependency_id,project_id) REFERENCES adaptive_tasks(id,project_id) ON DELETE CASCADE
);
CREATE INDEX adaptive_task_dependencies_project ON adaptive_task_dependencies(project_id,task_id);
CREATE TABLE adaptive_task_audit (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES adaptive_tasks(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL,
    action TEXT NOT NULL,
    actor TEXT NOT NULL CHECK(json_valid(actor)),
    reason TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    FOREIGN KEY(task_id,revision) REFERENCES adaptive_task_revisions(task_id,number)
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_revision_immutable BEFORE UPDATE ON adaptive_task_revisions
BEGIN SELECT RAISE(ABORT,'task revision is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_criteria_immutable BEFORE UPDATE ON adaptive_task_criteria
BEGIN SELECT RAISE(ABORT,'acceptance criteria are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_audit_immutable BEFORE UPDATE ON adaptive_task_audit
BEGIN SELECT RAISE(ABORT,'task audit is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_revision_no_delete BEFORE DELETE ON adaptive_task_revisions
WHEN EXISTS(SELECT 1 FROM adaptive_tasks WHERE id=OLD.task_id)
BEGIN SELECT RAISE(ABORT,'task revision belongs to retained task'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_criteria_no_delete BEFORE DELETE ON adaptive_task_criteria
WHEN EXISTS(SELECT 1 FROM adaptive_tasks WHERE id=OLD.task_id)
BEGIN SELECT RAISE(ABORT,'criteria belong to retained task'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_audit_no_delete BEFORE DELETE ON adaptive_task_audit
WHEN EXISTS(SELECT 1 FROM adaptive_tasks WHERE id=OLD.task_id)
BEGIN SELECT RAISE(ABORT,'audit belongs to retained task'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_audit_cdc AFTER INSERT ON adaptive_task_audit
BEGIN
    INSERT INTO change_log(project_id,event_type,payload,created_at)
    SELECT project_id,'adaptive_task_changed',json_object('taskId',NEW.task_id,'revision',NEW.revision,'auditSequence',NEW.seq),NEW.created_at
    FROM adaptive_tasks WHERE id=NEW.task_id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER adaptive_task_revision_no_delete;
DROP TRIGGER adaptive_task_criteria_no_delete;
DROP TRIGGER adaptive_task_audit_no_delete;
DELETE FROM adaptive_tasks;
DROP TABLE adaptive_task_audit;
DROP TABLE adaptive_task_dependencies;
DROP TABLE adaptive_task_revisions;
DROP TABLE adaptive_task_criteria;
DROP TABLE adaptive_tasks;
