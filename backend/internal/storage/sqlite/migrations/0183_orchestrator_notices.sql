-- +goose Up
-- Orchestrator notices journal the terminal task facts pushed to a project's
-- live orchestrator session. They are delivery bookkeeping: the task state
-- itself stays derived at read time and one durable anchor dedups each fact.
CREATE TABLE adaptive_orchestrator_notices (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    task_id TEXT NOT NULL,
    fact TEXT NOT NULL CHECK(fact IN ('completed','failed','cancelled')),
    anchor TEXT NOT NULL CHECK(length(anchor) BETWEEN 1 AND 200),
    revision INTEGER NOT NULL CHECK(revision >= 1),
    detail TEXT NOT NULL CHECK(length(detail) BETWEEN 1 AND 4096),
    state TEXT NOT NULL CHECK(state IN ('pending','handed_off','not_sent','uncertain')),
    reason TEXT NOT NULL CHECK(length(reason) <= 1024),
    created_at DATETIME NOT NULL,
    resolved_at DATETIME,
    CHECK((state = 'pending') = (resolved_at IS NULL)),
    FOREIGN KEY(task_id) REFERENCES adaptive_tasks(id)
);
CREATE UNIQUE INDEX adaptive_orchestrator_notice_fact ON adaptive_orchestrator_notices(project_id, task_id, fact, anchor);
CREATE INDEX adaptive_orchestrator_notice_pending ON adaptive_orchestrator_notices(state, created_at);
-- +goose StatementBegin
CREATE TRIGGER adaptive_orchestrator_notice_scope BEFORE INSERT ON adaptive_orchestrator_notices
WHEN NOT EXISTS(SELECT 1 FROM adaptive_tasks t WHERE t.id=NEW.task_id AND t.project_id=NEW.project_id)
 OR NEW.state != 'pending'
 OR NEW.resolved_at IS NOT NULL
 OR NEW.created_at IS NULL
BEGIN SELECT RAISE(ABORT,'Orchestrator notice requires its exact project task scope and starts pending'); END;
CREATE TRIGGER adaptive_orchestrator_notice_seal BEFORE UPDATE ON adaptive_orchestrator_notices
WHEN OLD.id != NEW.id OR OLD.project_id != NEW.project_id OR OLD.task_id != NEW.task_id
 OR OLD.fact != NEW.fact OR OLD.anchor != NEW.anchor OR OLD.revision != NEW.revision
 OR OLD.detail != NEW.detail OR OLD.created_at != NEW.created_at
 OR NOT (OLD.state = 'pending' AND NEW.state IN ('handed_off','not_sent','uncertain')
   AND OLD.resolved_at IS NULL AND NEW.resolved_at IS NOT NULL)
BEGIN SELECT RAISE(ABORT,'Orchestrator notice is sealed at creation and settles exactly once'); END;
CREATE TRIGGER adaptive_orchestrator_notice_retained BEFORE DELETE ON adaptive_orchestrator_notices
BEGIN SELECT RAISE(ABORT,'Orchestrator notice history is retained'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE orchestrator_notice_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO orchestrator_notice_history_guard SELECT count(*) FROM adaptive_orchestrator_notices;
DROP TABLE orchestrator_notice_history_guard;
DROP TRIGGER adaptive_orchestrator_notice_retained;
DROP TRIGGER adaptive_orchestrator_notice_seal;
DROP TRIGGER adaptive_orchestrator_notice_scope;
DROP INDEX adaptive_orchestrator_notice_pending;
DROP INDEX adaptive_orchestrator_notice_fact;
DROP TABLE adaptive_orchestrator_notices;
-- +goose StatementEnd
