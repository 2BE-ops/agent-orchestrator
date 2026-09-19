-- +goose Up
CREATE TABLE adaptive_agent_manager_proposals (
    id TEXT PRIMARY KEY NOT NULL,
    request_id TEXT NOT NULL REFERENCES adaptive_agent_manager_requests(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK(number BETWEEN 1 AND 5),
    idempotency_key TEXT NOT NULL,
    controller_id TEXT NOT NULL REFERENCES adaptive_agent_manager_controllers(id),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    source_owner TEXT NOT NULL CHECK(json_valid(source_owner)),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=524288),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    UNIQUE(request_id,number),
    UNIQUE(request_id,idempotency_key)
);
-- +goose StatementBegin
CREATE TRIGGER adaptive_agent_manager_proposal_immutable BEFORE UPDATE ON adaptive_agent_manager_proposals
BEGIN SELECT RAISE(ABORT,'Manager proposal is immutable'); END;
CREATE TRIGGER adaptive_agent_manager_proposal_retained BEFORE DELETE ON adaptive_agent_manager_proposals
WHEN EXISTS(SELECT 1 FROM adaptive_agent_manager_requests WHERE id=OLD.request_id)
BEGIN SELECT RAISE(ABORT,'proposal belongs to retained Manager work'); END;
CREATE TRIGGER adaptive_agent_manager_proposal_scope BEFORE INSERT ON adaptive_agent_manager_proposals
WHEN NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_requests r
 JOIN adaptive_agent_manager_controllers c ON c.project_id=r.project_id AND c.id=NEW.controller_id
 JOIN adaptive_agent_manager_dispatches d ON d.controller_id=c.id AND d.session_id=NEW.session_id
 WHERE r.id=NEW.request_id)
BEGIN SELECT RAISE(ABORT,'Manager proposal must belong to its project controller'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE manager_proposal_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO manager_proposal_history_guard SELECT count(*) FROM adaptive_agent_manager_proposals;
DROP TABLE manager_proposal_history_guard;
DROP TABLE adaptive_agent_manager_proposals;
-- +goose StatementEnd
