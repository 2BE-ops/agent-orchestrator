-- name: InsertAgentManagerDecision :exec
INSERT INTO adaptive_agent_manager_decisions(proposal_id,request_id,outcome,snapshot,content_hash,created_at) VALUES(?,?,?,?,?,?);

-- name: GetAgentManagerDecision :one
SELECT * FROM adaptive_agent_manager_decisions WHERE proposal_id=?;

-- name: ListAgentManagerDecisions :many
SELECT d.* FROM adaptive_agent_manager_decisions d JOIN adaptive_agent_manager_proposals p ON p.id=d.proposal_id
WHERE d.request_id=? ORDER BY p.number;

-- name: ListUnassessedAgentManagerProposals :many
SELECT r.project_id,p.request_id,p.id AS proposal_id FROM adaptive_agent_manager_proposals p
JOIN adaptive_agent_manager_requests r ON r.id=p.request_id
WHERE p.id>sqlc.arg(after_proposal_id) AND json_extract(p.snapshot,'$.definition.action')='select_existing'
AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_decisions d WHERE d.proposal_id=p.id)
AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_request_resolutions x WHERE x.request_id=p.request_id)
ORDER BY p.id LIMIT sqlc.arg(page_limit);

-- name: AgentManagerDecisionCursor :one
SELECT after_proposal_id FROM adaptive_agent_manager_decision_cursor WHERE id=1;

-- name: SetAgentManagerDecisionCursor :execrows
UPDATE adaptive_agent_manager_decision_cursor SET after_proposal_id=? WHERE id=1;
