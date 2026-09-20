-- +goose Up
CREATE TABLE adaptive_agent_manager_registry_actions (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    request_id TEXT NOT NULL REFERENCES adaptive_agent_manager_requests(id),
    idempotency_key TEXT NOT NULL CHECK(length(idempotency_key) BETWEEN 1 AND 200),
    action TEXT NOT NULL CHECK(action IN ('create','append_version')),
    kind TEXT NOT NULL CHECK(kind IN ('agent_type','skill')),
    entry_id TEXT NOT NULL,
    version INTEGER NOT NULL CHECK(version>0),
    source_owner TEXT NOT NULL CHECK(json_valid(source_owner)),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=524288),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    FOREIGN KEY(entry_id,version) REFERENCES adaptive_registry_versions(entry_id,number),
    UNIQUE(request_id,idempotency_key),
    UNIQUE(entry_id,version)
);
CREATE INDEX adaptive_manager_registry_project ON adaptive_agent_manager_registry_actions(project_id,action,kind);
CREATE INDEX adaptive_manager_registry_request ON adaptive_agent_manager_registry_actions(request_id,id);
-- +goose StatementBegin
CREATE TRIGGER adaptive_manager_registry_immutable BEFORE UPDATE ON adaptive_agent_manager_registry_actions
BEGIN SELECT RAISE(ABORT,'Manager registry receipt is immutable'); END;
CREATE TRIGGER adaptive_manager_registry_retained BEFORE DELETE ON adaptive_agent_manager_registry_actions
BEGIN SELECT RAISE(ABORT,'Manager registry receipt history is retained'); END;
CREATE TRIGGER adaptive_manager_registry_scope BEFORE INSERT ON adaptive_agent_manager_registry_actions
WHEN NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_requests r
 JOIN adaptive_agent_manager_contexts c ON c.request_id=r.id
 JOIN adaptive_agent_manager_contexts conversation ON conversation.controller_id=c.controller_id
 AND conversation.content_hash=json_extract(NEW.snapshot,'$.conversationContextHash')
 JOIN adaptive_registry_versions v ON v.entry_id=NEW.entry_id AND v.number=NEW.version
 WHERE r.id=NEW.request_id AND r.project_id=NEW.project_id
 AND r.content_hash=json_extract(NEW.snapshot,'$.requestHash')
 AND c.id=json_extract(NEW.snapshot,'$.contextId') AND c.content_hash=json_extract(NEW.snapshot,'$.contextHash')
 AND c.controller_id=json_extract(NEW.snapshot,'$.controllerId') AND c.session_id=json_extract(NEW.snapshot,'$.sessionId')
 AND json_extract(c.snapshot,'$.classification')='technical' AND json_extract(conversation.snapshot,'$.classification')='technical'
 AND v.actor_origin='AGENT_MANAGER' AND v.actor_id=c.controller_id AND v.kind=NEW.kind
 AND v.content_hash=json_extract(NEW.snapshot,'$.target.contentHash'))
 OR coalesce(json_extract(NEW.snapshot,'$.classification'),'')!='technical'
 OR coalesce(json_extract(NEW.snapshot,'$.id'),'')!=NEW.id
 OR coalesce(json_extract(NEW.snapshot,'$.projectId'),'')!=NEW.project_id
 OR coalesce(json_extract(NEW.snapshot,'$.requestId'),'')!=NEW.request_id
 OR coalesce(json_extract(NEW.snapshot,'$.action.action'),'')!=NEW.action
 OR coalesce(json_extract(NEW.snapshot,'$.action.kind'),'')!=NEW.kind
 OR coalesce(json_extract(NEW.snapshot,'$.target.id'),'')!=NEW.entry_id
 OR coalesce(json_extract(NEW.snapshot,'$.target.version'),0)!=NEW.version
 OR coalesce(json_extract(NEW.snapshot,'$.contentHash'),'')!=NEW.content_hash
 OR EXISTS(SELECT 1 FROM adaptive_agent_manager_request_resolutions WHERE request_id=NEW.request_id)
BEGIN SELECT RAISE(ABORT,'Manager registry receipt requires attributed technical input and exact version'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE manager_registry_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO manager_registry_history_guard SELECT count(*) FROM adaptive_agent_manager_registry_actions;
DROP TABLE manager_registry_history_guard;
DROP TABLE adaptive_agent_manager_registry_actions;
-- +goose StatementEnd
