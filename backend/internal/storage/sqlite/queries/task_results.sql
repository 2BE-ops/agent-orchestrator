-- name: InsertTaskResult :exec
INSERT INTO adaptive_task_results(id,attempt_id,task_id,number,session_id,native_generation,source_owner,task_revision,criteria_version,configuration_hash,configuration_sequence,context_hash,idempotency_key,definition,content_hash,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?);

-- name: GetTaskResult :one
SELECT * FROM adaptive_task_results WHERE id=?;

-- name: GetTaskResultByKey :one
SELECT * FROM adaptive_task_results WHERE attempt_id=? AND idempotency_key=?;

-- name: LatestTaskResult :one
SELECT * FROM adaptive_task_results WHERE attempt_id=? ORDER BY number DESC LIMIT 1;

-- name: ListTaskResults :many
SELECT * FROM adaptive_task_results WHERE attempt_id=? AND number>? ORDER BY number LIMIT ?;
