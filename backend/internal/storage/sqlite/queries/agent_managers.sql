-- name: InsertAgentManager :exec
INSERT INTO adaptive_agent_managers(project_id,revision,created_at,updated_at) VALUES(?,1,?,?);

-- name: GetAgentManagerRecord :one
SELECT * FROM adaptive_agent_managers WHERE project_id=?;

-- name: AdvanceAgentManagerConfiguration :execrows
UPDATE adaptive_agent_managers SET revision=revision+1,updated_at=sqlc.arg(updated_at)
WHERE project_id=sqlc.arg(project_id) AND revision=sqlc.arg(expected_revision);

-- name: InsertAgentManagerConfiguration :exec
INSERT INTO adaptive_agent_manager_configurations(project_id,number,agent_type_id,agent_type_version,snapshot,content_hash,created_at) VALUES(?,?,?,?,?,?,?);

-- name: GetAgentManager :one
SELECT c.* FROM adaptive_agent_manager_configurations c JOIN adaptive_agent_managers m ON m.project_id=c.project_id AND m.revision=c.number WHERE m.project_id=?;

-- name: GetAgentManagerConfiguration :one
SELECT * FROM adaptive_agent_manager_configurations WHERE project_id=? AND number=?;

-- name: ListAgentManagerConfigurations :many
SELECT * FROM adaptive_agent_manager_configurations WHERE project_id=? AND number>? ORDER BY number LIMIT ?;

-- name: InsertAgentManagerAudit :exec
INSERT INTO adaptive_agent_manager_audit(project_id,configuration_version,action,actor,reason,created_at) VALUES(?,?,?,?,?,?);

-- name: ListAgentManagerAudit :many
SELECT * FROM adaptive_agent_manager_audit WHERE project_id=? AND seq>? ORDER BY seq LIMIT ?;
