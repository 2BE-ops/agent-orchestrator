-- name: InsertAgentManagerRequest :one
INSERT INTO adaptive_agent_manager_requests(id,project_id,task_id,task_revision,criteria_version,configuration_version,snapshot,content_hash,created_at)
VALUES(?,?,?,?,?,?,?,?,?) RETURNING *;

-- name: GetAgentManagerRequest :one
SELECT * FROM adaptive_agent_manager_requests WHERE id=?;

-- name: PendingAgentManagerTaskRequest :one
SELECT r.* FROM adaptive_agent_manager_requests r WHERE r.task_id=?
AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_request_resolutions x WHERE x.request_id=r.id);

-- name: CountAgentManagerRequests :one
SELECT count(*) FROM adaptive_agent_manager_requests r WHERE r.project_id=sqlc.arg(project_id)
AND (sqlc.arg(pending_only)=0 OR NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_request_resolutions x WHERE x.request_id=r.id));

-- name: ListAgentManagerRequests :many
SELECT r.* FROM adaptive_agent_manager_requests r WHERE r.project_id=sqlc.arg(project_id) AND r.sequence>sqlc.arg(after_sequence)
AND (sqlc.arg(pending_only)=0 OR NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_request_resolutions x WHERE x.request_id=r.id))
ORDER BY r.sequence LIMIT sqlc.arg(page_limit);

-- name: InsertAgentManagerRequestResolution :exec
INSERT INTO adaptive_agent_manager_request_resolutions(request_id,outcome,actor,reason,created_at) VALUES(?,?,?,?,?);

-- name: GetAgentManagerRequestResolution :one
SELECT * FROM adaptive_agent_manager_request_resolutions WHERE request_id=?;
