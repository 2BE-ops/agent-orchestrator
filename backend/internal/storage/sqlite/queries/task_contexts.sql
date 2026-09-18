-- name: InsertTaskContext :exec
INSERT INTO adaptive_task_contexts(attempt_id,session_id,execution_operation_id,configuration_hash,snapshot,content_hash,created_at) VALUES (?,?,?,?,?,?,?);

-- name: GetTaskContext :one
SELECT * FROM adaptive_task_contexts WHERE attempt_id=?;

-- name: GetTaskContextBySession :one
SELECT * FROM adaptive_task_contexts WHERE session_id=?;
