-- +goose Up
CREATE TABLE adaptive_project_goals (
    project_id TEXT PRIMARY KEY NOT NULL REFERENCES projects(id),
    current_version INTEGER NOT NULL CHECK(current_version >= 1),
    updated_at DATETIME NOT NULL
);
CREATE TABLE adaptive_project_goal_versions (
    project_id TEXT NOT NULL REFERENCES projects(id),
    number INTEGER NOT NULL CHECK(number >= 1),
    goal TEXT NOT NULL CHECK(length(CAST(goal AS BLOB)) BETWEEN 1 AND 32768),
    actor TEXT NOT NULL CHECK(json_valid(actor) AND length(CAST(actor AS BLOB)) <= 4096),
    reason TEXT NOT NULL CHECK(length(CAST(reason AS BLOB)) BETWEEN 1 AND 2000),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    PRIMARY KEY(project_id, number)
);
CREATE TABLE adaptive_project_goal_completions (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL,
    goal_version INTEGER NOT NULL CHECK(goal_version >= 1),
    summary TEXT NOT NULL CHECK(length(CAST(summary AS BLOB)) BETWEEN 1 AND 8000),
    evidence TEXT NOT NULL CHECK(json_valid(evidence) AND length(CAST(evidence AS BLOB)) <= 262144),
    actor TEXT NOT NULL CHECK(json_valid(actor) AND length(CAST(actor AS BLOB)) <= 4096),
    reason TEXT NOT NULL CHECK(length(CAST(reason AS BLOB)) BETWEEN 1 AND 2000),
    created_at DATETIME NOT NULL,
    UNIQUE(project_id, goal_version),
    FOREIGN KEY(project_id, goal_version) REFERENCES adaptive_project_goal_versions(project_id, number)
);
CREATE INDEX adaptive_project_goal_completion_project ON adaptive_project_goal_completions(project_id);
CREATE TABLE adaptive_orchestrator_plan_receipts (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    session_id TEXT NOT NULL CHECK(length(session_id) BETWEEN 1 AND 200),
    idempotency_key TEXT NOT NULL CHECK(length(CAST(idempotency_key AS BLOB)) BETWEEN 1 AND 200),
    action_kind TEXT NOT NULL CHECK(action_kind IN ('create_task','revise_task','freeze_criteria')),
    request_hash TEXT NOT NULL CHECK(length(request_hash)=64),
    outcome TEXT NOT NULL CHECK(json_valid(outcome) AND length(CAST(outcome AS BLOB)) <= 16384),
    created_at DATETIME NOT NULL,
    UNIQUE(project_id, idempotency_key)
);
CREATE INDEX adaptive_orchestrator_plan_receipt_project ON adaptive_orchestrator_plan_receipts(project_id, id);
-- +goose StatementBegin
CREATE TRIGGER adaptive_project_goal_version_immutable BEFORE UPDATE ON adaptive_project_goal_versions
BEGIN SELECT RAISE(ABORT,'Project goal version is immutable'); END;
CREATE TRIGGER adaptive_project_goal_version_retained BEFORE DELETE ON adaptive_project_goal_versions
BEGIN SELECT RAISE(ABORT,'Project goal version history is retained'); END;
CREATE TRIGGER adaptive_project_goal_completion_immutable BEFORE UPDATE ON adaptive_project_goal_completions
BEGIN SELECT RAISE(ABORT,'Project goal completion is immutable'); END;
CREATE TRIGGER adaptive_project_goal_completion_retained BEFORE DELETE ON adaptive_project_goal_completions
BEGIN SELECT RAISE(ABORT,'Project goal completion history is retained'); END;
CREATE TRIGGER adaptive_project_goal_completion_scope BEFORE INSERT ON adaptive_project_goal_completions
WHEN NOT EXISTS(SELECT 1 FROM adaptive_project_goal_versions v
 JOIN adaptive_project_goals g ON g.project_id=v.project_id AND g.current_version=v.number
 WHERE v.project_id=NEW.project_id AND v.number=NEW.goal_version)
 OR coalesce(json_extract(NEW.evidence,'$.projectId'),'')!=NEW.project_id
 OR coalesce(json_extract(NEW.evidence,'$.goalVersion'),0)!=NEW.goal_version
 OR coalesce(json_extract(NEW.evidence,'$.goalHash'),'')=''
 OR json_type(NEW.evidence,'$.verifiedTasks')!='array'
 OR json_type(NEW.evidence,'$.blockers') IS NOT NULL
BEGIN SELECT RAISE(ABORT,'Project goal completion requires the current goal version and verified evidence'); END;
CREATE TRIGGER adaptive_orchestrator_plan_receipt_immutable BEFORE UPDATE ON adaptive_orchestrator_plan_receipts
BEGIN SELECT RAISE(ABORT,'Orchestrator plan receipt is immutable'); END;
CREATE TRIGGER adaptive_orchestrator_plan_receipt_retained BEFORE DELETE ON adaptive_orchestrator_plan_receipts
BEGIN SELECT RAISE(ABORT,'Orchestrator plan receipt history is retained'); END;
CREATE TRIGGER adaptive_orchestrator_plan_receipt_scope BEFORE INSERT ON adaptive_orchestrator_plan_receipts
WHEN coalesce(json_extract(NEW.outcome,'$.projectId'),'')!=NEW.project_id
 OR coalesce(json_extract(NEW.outcome,'$.sessionId'),'')!=NEW.session_id
 OR coalesce(json_extract(NEW.outcome,'$.action'),'')!=NEW.action_kind
 OR coalesce(json_extract(NEW.outcome,'$.requestHash'),'')!=NEW.request_hash
 OR coalesce(json_extract(NEW.outcome,'$.receiptId'),'')!=NEW.id
BEGIN SELECT RAISE(ABORT,'Orchestrator plan receipt outcome must match its sealed request'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE orchestrator_goal_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO orchestrator_goal_history_guard SELECT count(*) FROM adaptive_project_goal_versions;
INSERT INTO orchestrator_goal_history_guard SELECT count(*) FROM adaptive_project_goal_completions;
INSERT INTO orchestrator_goal_history_guard SELECT count(*) FROM adaptive_orchestrator_plan_receipts;
DROP TABLE orchestrator_goal_history_guard;
DROP TRIGGER adaptive_orchestrator_plan_receipt_retained;
DROP TRIGGER adaptive_orchestrator_plan_receipt_immutable;
DROP TRIGGER adaptive_orchestrator_plan_receipt_scope;
DROP TABLE adaptive_orchestrator_plan_receipts;
DROP TRIGGER adaptive_project_goal_completion_retained;
DROP TRIGGER adaptive_project_goal_completion_immutable;
DROP TRIGGER adaptive_project_goal_completion_scope;
DROP TABLE adaptive_project_goal_completions;
DROP TRIGGER adaptive_project_goal_version_retained;
DROP TRIGGER adaptive_project_goal_version_immutable;
DROP TABLE adaptive_project_goal_versions;
DROP TABLE adaptive_project_goals;
-- +goose StatementEnd
