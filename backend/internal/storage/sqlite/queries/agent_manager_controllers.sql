-- name: InsertAgentManagerController :exec
INSERT INTO adaptive_agent_manager_controllers(id,project_id,configuration_version,actor,reason,created_at) VALUES(?,?,?,?,?,?);

-- name: GetAgentManagerController :one
SELECT * FROM adaptive_agent_manager_controllers WHERE id=?;

-- name: ActiveAgentManagerController :one
SELECT * FROM adaptive_agent_manager_controllers WHERE project_id=? AND released_at IS NULL;

-- name: CountAgentManagerControllers :one
SELECT count(*) FROM adaptive_agent_manager_controllers WHERE project_id=?;

-- name: ListAgentManagerControllers :many
SELECT * FROM adaptive_agent_manager_controllers WHERE project_id=? AND id>? ORDER BY id LIMIT ?;

-- name: ReleaseAgentManagerController :execrows
UPDATE adaptive_agent_manager_controllers SET released_at=?,release_reason=? WHERE id=? AND released_at IS NULL;

-- name: InsertAgentManagerDispatch :exec
INSERT INTO adaptive_agent_manager_dispatches(controller_id,session_id,configuration_hash,created_at) VALUES(?,?,?,?);

-- name: GetAgentManagerDispatch :one
SELECT * FROM adaptive_agent_manager_dispatches WHERE controller_id=?;

-- name: GetAgentManagerDispatchBySession :one
SELECT * FROM adaptive_agent_manager_dispatches WHERE session_id=?;

-- name: InsertAgentManagerExecution :exec
INSERT INTO adaptive_agent_manager_execution_operations(id,controller_id,session_id,source_owner,kind,created_at) VALUES(?,?,?,?,?,?);

-- name: GetAgentManagerExecution :one
SELECT * FROM adaptive_agent_manager_execution_operations WHERE id=?;

-- name: CountAgentManagerExecutions :one
SELECT count(*) FROM adaptive_agent_manager_execution_operations WHERE controller_id=?;

-- name: PendingAgentManagerControllerExecution :one
SELECT o.* FROM adaptive_agent_manager_execution_operations o WHERE o.controller_id=? AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_execution_resolutions r WHERE r.operation_id=o.id);

-- name: PendingAgentManagerExecution :one
SELECT o.* FROM adaptive_agent_manager_execution_operations o WHERE o.session_id=? AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_execution_resolutions r WHERE r.operation_id=o.id);

-- name: InsertAgentManagerExecutionResolution :exec
INSERT INTO adaptive_agent_manager_execution_resolutions(operation_id,observed_owner,outcome,reason,created_at) VALUES(?,?,?,?,?);

-- name: GetAgentManagerExecutionResolution :one
SELECT * FROM adaptive_agent_manager_execution_resolutions WHERE operation_id=?;
