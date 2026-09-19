-- +goose Up
CREATE TABLE adaptive_agent_manager_contexts (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    request_id TEXT NOT NULL REFERENCES adaptive_agent_manager_requests(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK(number BETWEEN 1 AND 32),
    controller_id TEXT NOT NULL REFERENCES adaptive_agent_manager_controllers(id),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    native_generation TEXT NOT NULL,
    source_owner TEXT NOT NULL CHECK(json_valid(source_owner)),
    classification TEXT NOT NULL CHECK(classification IN ('technical','engagement','mission')),
    engagement_id TEXT NOT NULL CHECK(length(CAST(engagement_id AS BLOB))<=200 AND (classification!='engagement' OR engagement_id!='')),
    previous_context_hash TEXT NOT NULL CHECK(previous_context_hash='' OR length(previous_context_hash)=64),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=1048576),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    UNIQUE(request_id,number)
);
CREATE INDEX adaptive_agent_manager_context_controller ON adaptive_agent_manager_contexts(controller_id,sequence DESC);
-- +goose StatementBegin
CREATE TRIGGER adaptive_agent_manager_context_immutable BEFORE UPDATE ON adaptive_agent_manager_contexts
BEGIN SELECT RAISE(ABORT,'Manager context is immutable'); END;
CREATE TRIGGER adaptive_agent_manager_context_retained BEFORE DELETE ON adaptive_agent_manager_contexts
WHEN EXISTS(SELECT 1 FROM adaptive_agent_manager_requests WHERE id=OLD.request_id)
BEGIN SELECT RAISE(ABORT,'context belongs to retained Manager work'); END;
CREATE TRIGGER adaptive_agent_manager_context_scope BEFORE INSERT ON adaptive_agent_manager_contexts
WHEN NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_requests r
 JOIN adaptive_agent_manager_controllers c ON c.project_id=r.project_id AND c.id=NEW.controller_id
 JOIN adaptive_agent_manager_dispatches d ON d.controller_id=c.id AND d.session_id=NEW.session_id
 WHERE r.id=NEW.request_id)
BEGIN SELECT RAISE(ABORT,'Manager context must belong to its project controller'); END;
CREATE TRIGGER adaptive_agent_manager_context_chain BEFORE INSERT ON adaptive_agent_manager_contexts
WHEN NEW.previous_context_hash != COALESCE((SELECT content_hash FROM adaptive_agent_manager_contexts WHERE controller_id=NEW.controller_id ORDER BY sequence DESC LIMIT 1),'')
 OR EXISTS(SELECT 1 FROM adaptive_agent_manager_contexts p WHERE p.sequence=(SELECT MAX(sequence) FROM adaptive_agent_manager_contexts WHERE controller_id=NEW.controller_id)
 AND ((p.engagement_id!='' AND p.engagement_id!=NEW.engagement_id)
 OR (p.classification='mission' AND NEW.classification!='mission')
 OR (p.classification='engagement' AND NEW.classification='technical')))
BEGIN SELECT RAISE(ABORT,'Manager context cannot lose conversation sensitivity or engagement'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE manager_context_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO manager_context_history_guard SELECT count(*) FROM adaptive_agent_manager_contexts;
DROP TABLE manager_context_history_guard;
DROP TABLE adaptive_agent_manager_contexts;
-- +goose StatementEnd
