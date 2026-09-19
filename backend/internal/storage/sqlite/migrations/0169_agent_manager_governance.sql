-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema=ON;
UPDATE sqlite_schema
SET sql=replace(sql,'''project_knowledge_changed''','''project_knowledge_changed'', ''agent_manager_changed''')
WHERE type='table' AND name='change_log' AND sql NOT LIKE '%''agent_manager_changed''%';
PRAGMA writable_schema=RESET;
CREATE TEMP TABLE agent_manager_cdc_guard(valid INTEGER CHECK(valid=1));
INSERT INTO agent_manager_cdc_guard SELECT count(*) FROM sqlite_schema
WHERE type='table' AND name='change_log' AND sql LIKE '%''agent_manager_changed''%';
DROP TABLE agent_manager_cdc_guard;
-- +goose StatementEnd

CREATE TABLE adaptive_agent_managers (
    project_id TEXT PRIMARY KEY NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL CHECK(revision BETWEEN 1 AND 1000),
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    FOREIGN KEY(project_id,revision) REFERENCES adaptive_agent_manager_configurations(project_id,number) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE adaptive_agent_manager_configurations (
    project_id TEXT NOT NULL REFERENCES adaptive_agent_managers(project_id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK(number BETWEEN 1 AND 1000),
    agent_type_id TEXT NOT NULL,
    agent_type_version INTEGER NOT NULL,
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(snapshot)<=65536),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    PRIMARY KEY(project_id,number),
    FOREIGN KEY(agent_type_id,agent_type_version) REFERENCES adaptive_registry_versions(entry_id,number)
);
CREATE TABLE adaptive_agent_manager_audit (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL REFERENCES adaptive_agent_managers(project_id) ON DELETE CASCADE,
    configuration_version INTEGER NOT NULL,
    action TEXT NOT NULL,
    actor TEXT NOT NULL CHECK(json_valid(actor)),
    reason TEXT NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 2000),
    created_at DATETIME NOT NULL,
    FOREIGN KEY(project_id,configuration_version) REFERENCES adaptive_agent_manager_configurations(project_id,number)
);
CREATE INDEX adaptive_agent_manager_audit_project ON adaptive_agent_manager_audit(project_id,seq);
-- +goose StatementBegin
CREATE TRIGGER adaptive_agent_manager_configuration_immutable BEFORE UPDATE ON adaptive_agent_manager_configurations
BEGIN SELECT RAISE(ABORT,'manager configuration is immutable'); END;
CREATE TRIGGER adaptive_agent_manager_configuration_retained BEFORE DELETE ON adaptive_agent_manager_configurations
WHEN EXISTS(SELECT 1 FROM adaptive_agent_managers WHERE project_id=OLD.project_id)
BEGIN SELECT RAISE(ABORT,'manager configuration belongs to retained manager'); END;
CREATE TRIGGER adaptive_agent_manager_audit_immutable BEFORE UPDATE ON adaptive_agent_manager_audit
BEGIN SELECT RAISE(ABORT,'manager audit is immutable'); END;
CREATE TRIGGER adaptive_agent_manager_audit_retained BEFORE DELETE ON adaptive_agent_manager_audit
WHEN EXISTS(SELECT 1 FROM adaptive_agent_managers WHERE project_id=OLD.project_id)
BEGIN SELECT RAISE(ABORT,'manager audit belongs to retained manager'); END;
CREATE TRIGGER adaptive_agent_manager_audit_cdc AFTER INSERT ON adaptive_agent_manager_audit
BEGIN
    INSERT INTO change_log(project_id,event_type,payload,created_at)
    VALUES(NEW.project_id,'agent_manager_changed',json_object('configurationVersion',NEW.configuration_version,'auditSequence',NEW.seq),NEW.created_at);
END;
-- +goose StatementEnd

-- +goose Down
-- Retained CDC vocabulary stays additive, as in 0148/0154/0159.
-- +goose StatementBegin
CREATE TEMP TABLE manager_governance_history_guard (retained INTEGER CHECK(retained=0));
INSERT INTO manager_governance_history_guard SELECT count(*) FROM adaptive_agent_manager_configurations;
DROP TABLE manager_governance_history_guard;
DROP TRIGGER adaptive_agent_manager_configuration_retained;
DROP TRIGGER adaptive_agent_manager_audit_retained;
DELETE FROM adaptive_agent_managers;
DROP TABLE adaptive_agent_manager_audit;
DROP TABLE adaptive_agent_manager_configurations;
DROP TABLE adaptive_agent_managers;
-- +goose StatementEnd
