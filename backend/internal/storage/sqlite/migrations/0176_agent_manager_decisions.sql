-- +goose Up
CREATE TABLE adaptive_agent_manager_decisions (
    proposal_id TEXT PRIMARY KEY NOT NULL REFERENCES adaptive_agent_manager_proposals(id),
    request_id TEXT NOT NULL REFERENCES adaptive_agent_manager_requests(id),
    outcome TEXT NOT NULL CHECK(outcome IN ('accepted','rejected')),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=16777216),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL
);
CREATE INDEX adaptive_agent_manager_decision_request ON adaptive_agent_manager_decisions(request_id);
CREATE UNIQUE INDEX adaptive_agent_manager_one_accepted ON adaptive_agent_manager_decisions(request_id) WHERE outcome='accepted';
-- +goose StatementBegin
CREATE TRIGGER adaptive_agent_manager_decision_immutable BEFORE UPDATE ON adaptive_agent_manager_decisions
BEGIN SELECT RAISE(ABORT,'Manager decision is immutable'); END;
CREATE TRIGGER adaptive_agent_manager_decision_retained BEFORE DELETE ON adaptive_agent_manager_decisions
BEGIN SELECT RAISE(ABORT,'Manager decision history is retained'); END;
CREATE TRIGGER adaptive_agent_manager_decision_scope BEFORE INSERT ON adaptive_agent_manager_decisions
WHEN NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_proposals p
 JOIN adaptive_agent_manager_requests r ON r.id=p.request_id
 WHERE p.id=NEW.proposal_id AND p.request_id=NEW.request_id
 AND p.content_hash=json_extract(NEW.snapshot,'$.proposalHash')
 AND r.content_hash=json_extract(NEW.snapshot,'$.requestHash')
 AND r.project_id=json_extract(NEW.snapshot,'$.projectId')
 AND json_extract(p.snapshot,'$.definition.action')='select_existing'
 AND coalesce(json_extract(p.snapshot,'$.contextId'),'')!='')
 OR EXISTS(SELECT 1 FROM adaptive_agent_manager_request_resolutions WHERE request_id=NEW.request_id)
BEGIN SELECT RAISE(ABORT,'Manager decision requires attributed pending proposal'); END;

-- Preserve all existing foreign keys and trigger references while widening the
-- exact resolution CHECK, using the established 0140/0168 schema-edit convention.
PRAGMA writable_schema=ON;
UPDATE sqlite_schema SET sql=replace(sql,
    'CHECK(outcome IN (''cancelled'',''superseded'',''needs_human''))',
    'CHECK(outcome IN (''cancelled'',''superseded'',''needs_human'',''selected''))')
WHERE type='table' AND name='adaptive_agent_manager_request_resolutions';
PRAGMA writable_schema=RESET;
CREATE TEMP TABLE manager_decision_schema_guard(valid INTEGER CHECK(valid=1));
INSERT INTO manager_decision_schema_guard SELECT count(*) FROM sqlite_schema
WHERE type='table' AND name='adaptive_agent_manager_request_resolutions'
AND instr(sql,'CHECK(outcome IN (''cancelled'',''superseded'',''needs_human'',''selected''))')>0;
DROP TABLE manager_decision_schema_guard;
CREATE TRIGGER adaptive_agent_manager_selected_proof BEFORE INSERT ON adaptive_agent_manager_request_resolutions
WHEN NEW.outcome='selected' AND (NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_decisions WHERE request_id=NEW.request_id AND outcome='accepted')
 OR coalesce(json_extract(NEW.actor,'$.kind'),'')!='SYSTEM' OR coalesce(json_extract(NEW.actor,'$.id'),'')!='manager-selector'
 OR coalesce(json_extract(NEW.actor,'$.sessionId'),'')!='')
BEGIN SELECT RAISE(ABORT,'Manager selection requires deterministic decision proof'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE manager_decision_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO manager_decision_history_guard SELECT count(*) FROM adaptive_agent_manager_decisions;
INSERT INTO manager_decision_history_guard SELECT count(*) FROM adaptive_agent_manager_request_resolutions WHERE outcome='selected';
DROP TABLE manager_decision_history_guard;
DROP TRIGGER adaptive_agent_manager_selected_proof;
DROP TRIGGER adaptive_agent_manager_decision_retained;
DROP TABLE adaptive_agent_manager_decisions;
PRAGMA writable_schema=ON;
UPDATE sqlite_schema SET sql=replace(sql,
    'CHECK(outcome IN (''cancelled'',''superseded'',''needs_human'',''selected''))',
    'CHECK(outcome IN (''cancelled'',''superseded'',''needs_human''))')
WHERE type='table' AND name='adaptive_agent_manager_request_resolutions';
PRAGMA writable_schema=RESET;
CREATE TEMP TABLE manager_decision_down_guard(valid INTEGER CHECK(valid=1));
INSERT INTO manager_decision_down_guard SELECT count(*) FROM sqlite_schema
WHERE type='table' AND name='adaptive_agent_manager_request_resolutions'
AND instr(sql,'CHECK(outcome IN (''cancelled'',''superseded'',''needs_human''))')>0;
DROP TABLE manager_decision_down_guard;
-- +goose StatementEnd
