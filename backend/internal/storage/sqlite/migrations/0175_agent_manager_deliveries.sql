-- +goose Up
CREATE TABLE adaptive_agent_manager_deliveries (
    id TEXT PRIMARY KEY NOT NULL,
    request_id TEXT NOT NULL REFERENCES adaptive_agent_manager_requests(id),
    context_id TEXT NOT NULL REFERENCES adaptive_agent_manager_contexts(id),
    controller_id TEXT NOT NULL REFERENCES adaptive_agent_manager_controllers(id),
    number INTEGER NOT NULL CHECK(number BETWEEN 1 AND 4),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    owner TEXT NOT NULL CHECK(json_valid(owner)),
    delivery_key TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('dispatching','handed_off','not_sent','uncertain')),
    reason TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE(request_id,number)
);
CREATE UNIQUE INDEX adaptive_agent_manager_delivery_exclusive ON adaptive_agent_manager_deliveries(request_id) WHERE state!='not_sent';
CREATE INDEX adaptive_agent_manager_delivery_controller ON adaptive_agent_manager_deliveries(controller_id,state);
CREATE TABLE adaptive_agent_manager_dispatch_cursor (
    id INTEGER PRIMARY KEY CHECK(id=1),
    after_sequence INTEGER NOT NULL CHECK(after_sequence>=0)
);
INSERT INTO adaptive_agent_manager_dispatch_cursor VALUES(1,0);
-- +goose StatementBegin
CREATE TRIGGER adaptive_agent_manager_delivery_guard BEFORE UPDATE ON adaptive_agent_manager_deliveries
WHEN OLD.state!='dispatching' OR NEW.state='dispatching' OR NEW.id!=OLD.id OR NEW.request_id!=OLD.request_id
 OR NEW.context_id!=OLD.context_id OR NEW.controller_id!=OLD.controller_id OR NEW.number!=OLD.number
 OR NEW.session_id!=OLD.session_id OR NEW.owner!=OLD.owner OR NEW.delivery_key!=OLD.delivery_key OR NEW.created_at!=OLD.created_at
BEGIN SELECT RAISE(ABORT,'Manager delivery attribution and terminal outcomes are immutable'); END;
CREATE TRIGGER adaptive_agent_manager_delivery_no_delete BEFORE DELETE ON adaptive_agent_manager_deliveries
BEGIN SELECT RAISE(ABORT,'Manager delivery history is retained'); END;
CREATE TRIGGER adaptive_agent_manager_delivery_scope BEFORE INSERT ON adaptive_agent_manager_deliveries
WHEN NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_contexts c WHERE c.id=NEW.context_id AND c.request_id=NEW.request_id AND c.controller_id=NEW.controller_id AND c.session_id=NEW.session_id AND c.source_owner=NEW.owner)
BEGIN SELECT RAISE(ABORT,'Manager delivery must match its sealed native input'); END;
CREATE TRIGGER adaptive_agent_manager_delivery_busy BEFORE INSERT ON adaptive_agent_manager_deliveries
WHEN EXISTS(SELECT 1 FROM adaptive_agent_manager_deliveries d WHERE d.controller_id=NEW.controller_id
 AND (d.state IN ('dispatching','uncertain') OR (d.state='handed_off' AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_request_resolutions r WHERE r.request_id=d.request_id))))
BEGIN SELECT RAISE(ABORT,'Manager conversation has unfinished or uncertain input'); END;
CREATE TRIGGER adaptive_agent_manager_proposal_context BEFORE INSERT ON adaptive_agent_manager_proposals
WHEN NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_contexts c
 JOIN adaptive_agent_manager_deliveries d ON d.context_id=c.id AND d.state!='not_sent'
 WHERE c.id=json_extract(NEW.snapshot,'$.contextId') AND c.content_hash=json_extract(NEW.snapshot,'$.contextHash')
 AND c.request_id=NEW.request_id AND c.controller_id=NEW.controller_id AND c.session_id=NEW.session_id
 AND c.native_generation=json_extract(NEW.snapshot,'$.nativeGeneration'))
BEGIN SELECT RAISE(ABORT,'Manager proposal requires attributed native input'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE manager_delivery_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO manager_delivery_history_guard SELECT count(*) FROM adaptive_agent_manager_deliveries;
DROP TABLE manager_delivery_history_guard;
DROP TRIGGER adaptive_agent_manager_proposal_context;
DROP TRIGGER adaptive_agent_manager_delivery_no_delete;
DROP TABLE adaptive_agent_manager_deliveries;
DROP TABLE adaptive_agent_manager_dispatch_cursor;
-- +goose StatementEnd
