-- name: InsertAgentManagerDecision :exec
INSERT INTO adaptive_agent_manager_decisions(proposal_id,request_id,outcome,snapshot,content_hash,created_at) VALUES(?,?,?,?,?,?);

-- name: GetAgentManagerDecision :one
SELECT * FROM adaptive_agent_manager_decisions WHERE proposal_id=?;

-- name: ListAgentManagerDecisions :many
SELECT d.* FROM adaptive_agent_manager_decisions d JOIN adaptive_agent_manager_proposals p ON p.id=d.proposal_id
WHERE d.request_id=? ORDER BY p.number;
