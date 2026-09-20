-- +goose Up
-- Keep the exact historical JSON/hash bytes. Missing labels mean technical;
-- all newly authored labels are checked, including writes outside store helpers.
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_classification BEFORE INSERT ON adaptive_task_revisions
WHEN (json_type(NEW.definition,'$.classification') IS NOT NULL AND
      (json_type(NEW.definition,'$.classification')<>'text' OR json_extract(NEW.definition,'$.classification') NOT IN ('technical','engagement','mission')))
  OR (json_type(NEW.definition,'$.engagementId') IS NOT NULL AND
      (json_type(NEW.definition,'$.engagementId')<>'text' OR length(json_extract(NEW.definition,'$.engagementId')) NOT BETWEEN 1 AND 200 OR trim(json_extract(NEW.definition,'$.engagementId'))<>json_extract(NEW.definition,'$.engagementId')))
  OR (json_extract(NEW.definition,'$.classification')='engagement' AND COALESCE(length(json_extract(NEW.definition,'$.engagementId')),0)=0)
BEGIN SELECT RAISE(ABORT,'invalid task context classification'); END;

CREATE TRIGGER project_knowledge_classification BEFORE INSERT ON project_knowledge_versions
WHEN (json_type(NEW.definition,'$.classification') IS NOT NULL AND
      (json_type(NEW.definition,'$.classification')<>'text' OR json_extract(NEW.definition,'$.classification') NOT IN ('technical','engagement','mission')))
  OR (json_type(NEW.definition,'$.engagementId') IS NOT NULL AND
      (json_type(NEW.definition,'$.engagementId')<>'text' OR length(json_extract(NEW.definition,'$.engagementId')) NOT BETWEEN 1 AND 200 OR trim(json_extract(NEW.definition,'$.engagementId'))<>json_extract(NEW.definition,'$.engagementId')))
  OR (json_extract(NEW.definition,'$.classification')='engagement' AND COALESCE(length(json_extract(NEW.definition,'$.engagementId')),0)=0)
BEGIN SELECT RAISE(ABORT,'invalid knowledge context classification'); END;

CREATE TRIGGER adaptive_registry_clearance BEFORE INSERT ON adaptive_registry_versions
WHEN NEW.kind='agent_type' AND json_type(NEW.definition,'$.agentType.maxContextClass') IS NOT NULL
 AND (json_type(NEW.definition,'$.agentType.maxContextClass')<>'text' OR json_extract(NEW.definition,'$.agentType.maxContextClass') NOT IN ('technical','engagement','mission'))
BEGIN SELECT RAISE(ABORT,'invalid Agent Type context clearance'); END;
-- +goose StatementEnd

CREATE INDEX project_knowledge_context_class ON project_knowledge_versions(
    COALESCE(json_extract(definition,'$.classification'),'technical'),
    COALESCE(json_extract(definition,'$.engagementId'),''),knowledge_id,number);

-- Delegations are immutable, versioned delivery artifacts. Each version binds
-- the exact native execution and the original sealed context. No send is implied.
CREATE TABLE adaptive_task_delegations (
    attempt_id TEXT NOT NULL REFERENCES adaptive_task_contexts(attempt_id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK(number BETWEEN 1 AND 1000),
    execution_operation_id TEXT NOT NULL UNIQUE REFERENCES adaptive_task_execution_operations(id),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    configuration_hash TEXT NOT NULL CHECK(length(configuration_hash)=64),
    context_hash TEXT NOT NULL CHECK(length(context_hash)=64),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(snapshot)<=1048576),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    PRIMARY KEY(attempt_id,number)
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_delegation_immutable BEFORE UPDATE ON adaptive_task_delegations
BEGIN SELECT RAISE(ABORT,'task delegation is immutable'); END;
CREATE TRIGGER adaptive_task_delegation_retained BEFORE DELETE ON adaptive_task_delegations
WHEN EXISTS(SELECT 1 FROM adaptive_task_contexts WHERE attempt_id=OLD.attempt_id)
BEGIN SELECT RAISE(ABORT,'delegation belongs to retained context'); END;
CREATE TRIGGER adaptive_task_delegation_scope BEFORE INSERT ON adaptive_task_delegations
WHEN NOT EXISTS(SELECT 1 FROM adaptive_task_contexts c
 JOIN adaptive_task_execution_operations o ON o.attempt_id=c.attempt_id AND o.session_id=c.session_id
 WHERE c.attempt_id=NEW.attempt_id AND c.session_id=NEW.session_id
 AND c.content_hash=NEW.context_hash AND o.id=NEW.execution_operation_id)
BEGIN SELECT RAISE(ABORT,'delegation must belong to its attempt and execution'); END;
-- +goose StatementEnd

-- +goose Down
-- Old executables do not understand classified definitions or delegation seals.
-- Refuse to discard those boundaries; an untouched installation can downgrade.
-- +goose StatementBegin
CREATE TEMP TABLE context_classification_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO context_classification_history_guard
SELECT (SELECT count(*) FROM adaptive_task_delegations)
 + (SELECT count(*) FROM adaptive_task_revisions WHERE json_type(definition,'$.classification') IS NOT NULL OR json_type(definition,'$.engagementId') IS NOT NULL)
 + (SELECT count(*) FROM project_knowledge_versions WHERE json_type(definition,'$.classification') IS NOT NULL OR json_type(definition,'$.engagementId') IS NOT NULL)
 + (SELECT count(*) FROM adaptive_registry_versions WHERE json_type(definition,'$.agentType.maxContextClass') IS NOT NULL)
 + (SELECT count(*) FROM adaptive_task_contexts WHERE json_extract(snapshot,'$.schemaVersion')>1);
DROP TABLE context_classification_history_guard;
DROP TABLE adaptive_task_delegations;
DROP INDEX project_knowledge_context_class;
DROP TRIGGER adaptive_registry_clearance;
DROP TRIGGER project_knowledge_classification;
DROP TRIGGER adaptive_task_classification;
-- +goose StatementEnd
