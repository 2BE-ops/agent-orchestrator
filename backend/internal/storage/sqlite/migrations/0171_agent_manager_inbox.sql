-- +goose Up
CREATE TABLE adaptive_agent_manager_requests (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    project_id TEXT NOT NULL REFERENCES adaptive_agent_managers(project_id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES adaptive_tasks(id),
    task_revision INTEGER NOT NULL,
    criteria_version INTEGER NOT NULL,
    configuration_version INTEGER NOT NULL,
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=16384),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    FOREIGN KEY(task_id,task_revision) REFERENCES adaptive_task_revisions(task_id,number),
    FOREIGN KEY(task_id,criteria_version) REFERENCES adaptive_task_criteria(task_id,number),
    FOREIGN KEY(project_id,configuration_version) REFERENCES adaptive_agent_manager_configurations(project_id,number)
);
CREATE INDEX adaptive_agent_manager_request_project ON adaptive_agent_manager_requests(project_id,sequence);
CREATE INDEX adaptive_agent_manager_request_task ON adaptive_agent_manager_requests(task_id);
CREATE TABLE adaptive_agent_manager_request_resolutions (
    request_id TEXT PRIMARY KEY NOT NULL REFERENCES adaptive_agent_manager_requests(id) ON DELETE CASCADE,
    outcome TEXT NOT NULL CHECK(outcome IN ('cancelled','superseded','needs_human')),
    actor TEXT NOT NULL CHECK(json_valid(actor)),
    reason TEXT NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 2000),
    created_at DATETIME NOT NULL
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_agent_manager_request_immutable BEFORE UPDATE ON adaptive_agent_manager_requests
BEGIN SELECT RAISE(ABORT,'Manager request is immutable'); END;
CREATE TRIGGER adaptive_agent_manager_request_retained BEFORE DELETE ON adaptive_agent_manager_requests
WHEN EXISTS(SELECT 1 FROM adaptive_agent_managers WHERE project_id=OLD.project_id)
BEGIN SELECT RAISE(ABORT,'request belongs to retained Manager'); END;
CREATE TRIGGER adaptive_agent_manager_request_project_guard BEFORE INSERT ON adaptive_agent_manager_requests
WHEN NOT EXISTS(SELECT 1 FROM adaptive_tasks WHERE id=NEW.task_id AND project_id=NEW.project_id)
BEGIN SELECT RAISE(ABORT,'Manager request task must belong to its project'); END;
CREATE TRIGGER adaptive_agent_manager_request_exclusive BEFORE INSERT ON adaptive_agent_manager_requests
WHEN EXISTS(SELECT 1 FROM adaptive_agent_manager_requests r WHERE r.task_id=NEW.task_id
 AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_request_resolutions x WHERE x.request_id=r.id))
BEGIN SELECT RAISE(ABORT,'task already has pending Manager work'); END;
CREATE TRIGGER adaptive_agent_manager_request_resolution_immutable BEFORE UPDATE ON adaptive_agent_manager_request_resolutions
BEGIN SELECT RAISE(ABORT,'Manager request resolution is immutable'); END;
CREATE TRIGGER adaptive_agent_manager_request_resolution_retained BEFORE DELETE ON adaptive_agent_manager_request_resolutions
WHEN EXISTS(SELECT 1 FROM adaptive_agent_manager_requests WHERE id=OLD.request_id)
BEGIN SELECT RAISE(ABORT,'resolution belongs to retained request'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE manager_inbox_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO manager_inbox_history_guard SELECT count(*) FROM adaptive_agent_manager_requests;
DROP TABLE manager_inbox_history_guard;
DROP TABLE adaptive_agent_manager_request_resolutions;
DROP TABLE adaptive_agent_manager_requests;
-- +goose StatementEnd
