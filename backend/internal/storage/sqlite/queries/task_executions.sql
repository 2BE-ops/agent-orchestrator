-- name: InsertTaskExecutionOperation :exec
INSERT INTO adaptive_task_execution_operations(id,attempt_id,session_id,generation,holder_id,source_owner,kind,created_at) VALUES(?,?,?,?,?,?,?,?);

-- name: GetTaskExecutionOperation :one
SELECT * FROM adaptive_task_execution_operations WHERE id=?;

-- name: PendingTaskExecution :one
SELECT o.* FROM adaptive_task_execution_operations o
WHERE o.session_id=? AND NOT EXISTS(SELECT 1 FROM adaptive_task_execution_resolutions r WHERE r.operation_id=o.id);

-- name: PendingTaskAttemptExecution :one
SELECT o.* FROM adaptive_task_execution_operations o
WHERE o.attempt_id=? AND NOT EXISTS(SELECT 1 FROM adaptive_task_execution_resolutions r WHERE r.operation_id=o.id);

-- name: InsertTaskExecutionResolution :exec
INSERT INTO adaptive_task_execution_resolutions(operation_id,observed_owner,outcome,reason,created_at) VALUES(?,?,?,?,?);

-- name: GetTaskExecutionResolution :one
SELECT * FROM adaptive_task_execution_resolutions WHERE operation_id=?;
