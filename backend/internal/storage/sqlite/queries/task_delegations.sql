-- name: InsertTaskDelegation :exec
INSERT INTO adaptive_task_delegations(attempt_id,number,execution_operation_id,session_id,configuration_hash,context_hash,snapshot,content_hash,created_at)
VALUES (?,?,?,?,?,?,?,?,?);

-- name: GetTaskDelegation :one
SELECT * FROM adaptive_task_delegations WHERE attempt_id=? AND number=?;

-- name: ListTaskDelegations :many
SELECT * FROM adaptive_task_delegations WHERE attempt_id=? AND number>? ORDER BY number LIMIT ?;

-- name: GetTaskDelegationByOperation :one
SELECT * FROM adaptive_task_delegations WHERE execution_operation_id=?;

-- name: MaxTaskDelegationNumber :one
SELECT CAST(COALESCE(MAX(number), 0) AS INTEGER) AS number FROM adaptive_task_delegations WHERE attempt_id=?;
