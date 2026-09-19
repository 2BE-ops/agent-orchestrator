-- name: InsertAgentManagerContext :exec
INSERT INTO adaptive_agent_manager_contexts(id,request_id,number,controller_id,session_id,native_generation,source_owner,classification,engagement_id,previous_context_hash,snapshot,content_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?);

-- name: GetAgentManagerContext :one
SELECT * FROM adaptive_agent_manager_contexts WHERE id=?;

-- name: ListAgentManagerContexts :many
SELECT * FROM adaptive_agent_manager_contexts WHERE request_id=? ORDER BY number;

-- name: LatestAgentManagerContext :one
SELECT * FROM adaptive_agent_manager_contexts WHERE controller_id=? ORDER BY sequence DESC LIMIT 1;

-- name: CountAgentManagerContexts :one
SELECT count(*) FROM adaptive_agent_manager_contexts WHERE controller_id=?;
