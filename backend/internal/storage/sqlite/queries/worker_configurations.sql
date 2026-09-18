-- name: InsertWorkerConfiguration :exec
INSERT INTO adaptive_worker_configurations (session_id,agent_type_id,agent_type_version,configuration,content_hash,created_at)
VALUES (?,?,?,?,?,?);

-- name: GetWorkerConfiguration :one
SELECT * FROM adaptive_worker_configurations WHERE session_id=?;
