-- name: InsertAgentManagerProposal :exec
INSERT INTO adaptive_agent_manager_proposals(id,request_id,number,idempotency_key,controller_id,session_id,source_owner,snapshot,content_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?);

-- name: GetAgentManagerProposal :one
SELECT * FROM adaptive_agent_manager_proposals WHERE id=?;

-- name: GetAgentManagerProposalByKey :one
SELECT * FROM adaptive_agent_manager_proposals WHERE request_id=? AND idempotency_key=?;

-- name: ListAgentManagerProposals :many
SELECT * FROM adaptive_agent_manager_proposals WHERE request_id=? ORDER BY number;

-- name: AgentManagerConnectedGeneration :one
SELECT r.created_at FROM adaptive_agent_manager_execution_operations o
JOIN adaptive_agent_manager_execution_resolutions r ON r.operation_id=o.id
WHERE o.controller_id=sqlc.arg(controller_id) AND o.session_id=sqlc.arg(session_id) AND r.outcome='connected'
AND json_extract(r.observed_owner,'$.Mode')=sqlc.arg(mode)
AND json_extract(r.observed_owner,'$.Harness')=sqlc.arg(harness)
AND CASE WHEN sqlc.arg(mode)='chat' THEN json_extract(r.observed_owner,'$.ControllerGeneration') ELSE json_extract(r.observed_owner,'$.RuntimeLaunchID') END=sqlc.arg(generation)
ORDER BY r.created_at DESC LIMIT 1;
