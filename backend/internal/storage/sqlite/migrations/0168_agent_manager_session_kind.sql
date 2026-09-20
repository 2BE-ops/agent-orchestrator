-- +goose Up
-- +goose StatementBegin
-- Reuse the established native session/conversation lifecycle with a distinct
-- controller kind. As in 0140, edit the exact CHECK instead of rebuilding the
-- parent table and invalidating its many foreign keys and CDC triggers.
PRAGMA writable_schema=ON;
UPDATE sqlite_schema SET sql=replace(sql,
    'CHECK (kind IN (''worker'', ''orchestrator''))',
    'CHECK (kind IN (''worker'', ''orchestrator'', ''agent_manager''))')
WHERE type='table' AND name='sessions';
PRAGMA writable_schema=RESET;

CREATE TEMP TABLE manager_kind_schema_guard (valid INTEGER CHECK(valid=1));
INSERT INTO manager_kind_schema_guard
SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='sessions'
AND instr(sql,'CHECK (kind IN (''worker'', ''orchestrator'', ''agent_manager''))')>0;
DROP TABLE manager_kind_schema_guard;

CREATE UNIQUE INDEX sessions_one_active_agent_manager ON sessions(project_id)
WHERE kind='agent_manager' AND is_terminated=0;

CREATE TRIGGER sessions_agent_manager_project_insert
BEFORE INSERT ON sessions
WHEN NEW.kind='agent_manager' AND coalesce(trim(NEW.project_id),'')=''
BEGIN SELECT RAISE(ABORT,'agent manager requires a project'); END;

CREATE TRIGGER sessions_agent_manager_identity_update
BEFORE UPDATE OF kind,project_id ON sessions
WHEN (OLD.kind='agent_manager' OR NEW.kind='agent_manager')
AND (OLD.kind<>NEW.kind OR OLD.project_id IS NOT NEW.project_id)
BEGIN SELECT RAISE(ABORT,'agent manager role and project are immutable'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Retained manager history cannot be represented by the old kind constraint.
-- Refuse downgrade atomically rather than relabel or delete that history.
CREATE TEMP TABLE manager_kind_history_guard (retained INTEGER CHECK(retained=0));
INSERT INTO manager_kind_history_guard SELECT count(*) FROM sessions WHERE kind='agent_manager';
DROP TABLE manager_kind_history_guard;
DROP TRIGGER sessions_agent_manager_identity_update;
DROP TRIGGER sessions_agent_manager_project_insert;
DROP INDEX sessions_one_active_agent_manager;
PRAGMA writable_schema=ON;
UPDATE sqlite_schema SET sql=replace(sql,
    'CHECK (kind IN (''worker'', ''orchestrator'', ''agent_manager''))',
    'CHECK (kind IN (''worker'', ''orchestrator''))')
WHERE type='table' AND name='sessions';
PRAGMA writable_schema=RESET;
CREATE TEMP TABLE manager_kind_down_guard (valid INTEGER CHECK(valid=1));
INSERT INTO manager_kind_down_guard
SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='sessions'
AND instr(sql,'CHECK (kind IN (''worker'', ''orchestrator''))')>0;
DROP TABLE manager_kind_down_guard;
-- +goose StatementEnd
