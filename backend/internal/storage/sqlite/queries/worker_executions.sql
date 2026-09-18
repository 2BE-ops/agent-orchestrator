-- name: InsertWorkerExecution :exec
INSERT INTO adaptive_worker_executions(id,session_id,source_kind,source_id,previous_activation,configuration,content_hash,origin,actor_id,reason,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?);

-- name: GetWorkerExecution :one
SELECT * FROM adaptive_worker_executions WHERE id=? AND session_id=?;

-- name: GetWorkerExecutionBySource :one
SELECT * FROM adaptive_worker_executions WHERE session_id=? AND source_kind=? AND source_id=?;

-- name: GetCurrentWorkerActivation :one
SELECT * FROM adaptive_worker_execution_activations WHERE session_id=? ORDER BY seq DESC LIMIT 1;

-- name: GetWorkerActivation :one
SELECT * FROM adaptive_worker_execution_activations WHERE session_id=? AND seq=?;

-- name: InsertWorkerActivation :exec
INSERT INTO adaptive_worker_execution_activations(session_id,execution_id,operation_id,action,created_at)
VALUES(?,?,?,?,?);

-- name: ListWorkerActivations :many
SELECT * FROM adaptive_worker_execution_activations WHERE session_id=? AND seq>? ORDER BY seq LIMIT ?;
