-- +goose Up
CREATE TABLE adaptive_project_controls (
    project_id TEXT PRIMARY KEY NOT NULL REFERENCES projects(id),
    state TEXT NOT NULL CHECK(state IN ('running','paused','draining','stopped')),
    actor TEXT NOT NULL CHECK(json_valid(actor) AND length(CAST(actor AS BLOB))<=4096),
    reason TEXT NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 2000),
    updated_at DATETIME NOT NULL
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_project_control_scope BEFORE INSERT ON adaptive_project_controls
WHEN NOT EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id)
 OR coalesce(json_extract(NEW.actor,'$.kind'),'')=''
 OR coalesce(json_extract(NEW.actor,'$.id'),'')=''
 OR NEW.updated_at IS NULL
BEGIN SELECT RAISE(ABORT,'Project control requires an existing project and a real actor'); END;
CREATE TRIGGER adaptive_project_control_transition BEFORE UPDATE ON adaptive_project_controls
WHEN OLD.project_id!=NEW.project_id
 OR NEW.state=OLD.state
 OR NOT (
    (OLD.state='running' AND NEW.state IN ('paused','draining','stopped'))
 OR (OLD.state='paused' AND NEW.state IN ('running','stopped'))
 OR (OLD.state='draining' AND NEW.state IN ('running','paused','stopped'))
 OR (OLD.state='stopped' AND NEW.state='running')
 )
 OR NOT EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id)
 OR coalesce(json_extract(NEW.actor,'$.kind'),'')=''
 OR coalesce(json_extract(NEW.actor,'$.id'),'')=''
BEGIN SELECT RAISE(ABORT,'Project control follows its state machine and never changes project'); END;
CREATE TRIGGER adaptive_project_control_retained BEFORE DELETE ON adaptive_project_controls
BEGIN SELECT RAISE(ABORT,'Project control history is retained'); END;

CREATE TABLE adaptive_task_needs_human (
    id TEXT PRIMARY KEY NOT NULL,
    task_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    reason_code TEXT NOT NULL CHECK(reason_code IN ('credential_missing','approval_required','ambiguous_intent','provider_unavailable','recovery_inconclusive')),
    detail TEXT NOT NULL CHECK(length(trim(detail)) BETWEEN 1 AND 4000),
    actor TEXT NOT NULL CHECK(json_valid(actor) AND length(CAST(actor AS BLOB))<=4096),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=16384),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    resolved_at DATETIME,
    resolution TEXT CHECK(resolution IS NULL OR (json_valid(resolution) AND length(CAST(resolution AS BLOB))<=16384)),
    resolved_by TEXT CHECK(resolved_by IS NULL OR (json_valid(resolved_by) AND length(CAST(resolved_by AS BLOB))<=4096)),
    CHECK((resolved_at IS NULL) = (resolution IS NULL)),
    CHECK((resolved_at IS NULL) = (resolved_by IS NULL)),
    FOREIGN KEY(task_id) REFERENCES adaptive_tasks(id)
);
CREATE INDEX adaptive_task_needs_human_project ON adaptive_task_needs_human(project_id, id);
CREATE UNIQUE INDEX adaptive_task_needs_human_pending ON adaptive_task_needs_human(task_id) WHERE resolved_at IS NULL;
CREATE TRIGGER adaptive_task_needs_human_scope BEFORE INSERT ON adaptive_task_needs_human
WHEN NOT EXISTS(SELECT 1 FROM adaptive_tasks t WHERE t.id=NEW.task_id AND t.project_id=NEW.project_id)
 OR coalesce(json_extract(NEW.snapshot,'$.id'),'')!=NEW.id
 OR coalesce(json_extract(NEW.snapshot,'$.taskId'),'')!=NEW.task_id
 OR coalesce(json_extract(NEW.snapshot,'$.projectId'),'')!=NEW.project_id
 OR coalesce(json_extract(NEW.snapshot,'$.reasonCode'),'')!=NEW.reason_code
 OR coalesce(json_extract(NEW.snapshot,'$.createdAt'),'')=''
 OR coalesce(json_extract(NEW.snapshot,'$.resolution'),'')!=''
 OR coalesce(json_extract(NEW.actor,'$.kind'),'')=''
BEGIN SELECT RAISE(ABORT,'Needs human requires its exact task scope and a sealed pending snapshot'); END;
CREATE TRIGGER adaptive_task_needs_human_resolved BEFORE UPDATE ON adaptive_task_needs_human
WHEN NOT (OLD.resolved_at IS NULL AND NEW.resolved_at IS NOT NULL
 AND OLD.resolution IS NULL AND NEW.resolution IS NOT NULL
 AND OLD.resolved_by IS NULL AND NEW.resolved_by IS NOT NULL
 AND OLD.id=NEW.id AND OLD.task_id=NEW.task_id AND OLD.project_id=NEW.project_id
 AND OLD.reason_code=NEW.reason_code AND OLD.detail=NEW.detail AND OLD.actor=NEW.actor
 AND OLD.snapshot=NEW.snapshot AND OLD.content_hash=NEW.content_hash
 AND OLD.created_at=NEW.created_at
 AND json_extract(NEW.resolution,'$.resolution') IS NOT NULL
 AND json_extract(NEW.resolution,'$.resolvedAt') IS NOT NULL
 AND coalesce(json_extract(NEW.resolved_by,'$.kind'),'')!='')
BEGIN SELECT RAISE(ABORT,'Needs human is sealed at creation and may only be resolved once'); END;
CREATE TRIGGER adaptive_task_needs_human_retained BEFORE DELETE ON adaptive_task_needs_human
BEGIN SELECT RAISE(ABORT,'Needs human history is retained'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE project_control_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO project_control_history_guard SELECT count(*) FROM adaptive_project_controls;
INSERT INTO project_control_history_guard SELECT count(*) FROM adaptive_task_needs_human;
DROP TABLE project_control_history_guard;
DROP TRIGGER adaptive_task_needs_human_retained;
DROP TRIGGER adaptive_task_needs_human_resolved;
DROP TRIGGER adaptive_task_needs_human_scope;
DROP INDEX adaptive_task_needs_human_pending;
DROP INDEX adaptive_task_needs_human_project;
DROP TABLE adaptive_task_needs_human;
DROP TRIGGER adaptive_project_control_retained;
DROP TRIGGER adaptive_project_control_transition;
DROP TRIGGER adaptive_project_control_scope;
DROP TABLE adaptive_project_controls;
-- +goose StatementEnd
