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

-- name: SelectTaskContextResults :many
SELECT r.* FROM adaptive_task_results r JOIN adaptive_task_attempts a ON a.id=r.attempt_id
WHERE r.task_id=sqlc.arg(task_id) AND (sqlc.arg(task_revision)=0 OR r.task_revision=sqlc.arg(task_revision))
AND r.attempt_id!=sqlc.arg(excluded_attempt)
AND NOT EXISTS(SELECT 1 FROM adaptive_task_results newer WHERE newer.attempt_id=r.attempt_id AND newer.number>r.number)
ORDER BY a.number DESC LIMIT sqlc.arg(page_limit);
