-- name: InsertAgentManagerRegistryAction :exec
INSERT INTO adaptive_agent_manager_registry_actions(id,project_id,request_id,idempotency_key,action,kind,entry_id,version,source_owner,snapshot,content_hash,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?);

-- name: GetAgentManagerRegistryAction :one
SELECT * FROM adaptive_agent_manager_registry_actions WHERE id=?;

-- name: GetAgentManagerRegistryActionByKey :one
SELECT * FROM adaptive_agent_manager_registry_actions WHERE request_id=? AND idempotency_key=?;

-- name: ListAgentManagerRegistryActions :many
SELECT * FROM adaptive_agent_manager_registry_actions WHERE request_id=? AND id>sqlc.arg(after_id)
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: CountAgentManagerRegistryActions :one
SELECT count(*) FROM adaptive_agent_manager_registry_actions WHERE request_id=?;

-- name: CountAgentManagerCreatedEntries :one
SELECT count(*) FROM adaptive_agent_manager_registry_actions WHERE project_id=? AND action='create' AND kind=?;

-- name: CountAgentManagerAppendedVersions :one
SELECT count(*) FROM adaptive_registry_versions WHERE entry_id=? AND number>1 AND actor_origin='AGENT_MANAGER';
