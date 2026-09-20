-- +goose Up
CREATE TABLE adaptive_agent_manager_decision_cursor (
    id INTEGER PRIMARY KEY CHECK(id=1),
    after_proposal_id TEXT NOT NULL CHECK(length(CAST(after_proposal_id AS BLOB))<=200)
);
INSERT INTO adaptive_agent_manager_decision_cursor VALUES(1,'');

-- +goose Down
-- A scan position is disposable; retained proposals and decisions are unchanged.
DROP TABLE adaptive_agent_manager_decision_cursor;
