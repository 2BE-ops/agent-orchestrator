-- name: InsertTaskAttempt :exec
INSERT INTO adaptive_task_attempts(id,task_id,task_revision,criteria_version,number,launch_intent_id,dependencies,actor,reason,created_at) VALUES(?,?,?,?,?,?,?,?,?,?);

-- name: GetTaskAttempt :one
SELECT * FROM adaptive_task_attempts WHERE id=?;

-- name: GetTaskAttemptByIntent :one
SELECT * FROM adaptive_task_attempts WHERE launch_intent_id=?;

-- name: NextTaskAttemptNumber :one
SELECT COALESCE(MAX(number),0)+1 FROM adaptive_task_attempts WHERE task_id=?;

-- name: ListTaskAttempts :many
SELECT * FROM adaptive_task_attempts WHERE task_id=? AND number > ? ORDER BY number LIMIT ?;

-- name: InsertTaskLease :exec
INSERT INTO adaptive_task_leases(attempt_id,task_id,generation,holder_id,heartbeat_at,expires_at) VALUES(?,?,?,?,?,?);

-- name: GetTaskLease :one
SELECT * FROM adaptive_task_leases WHERE attempt_id=?;

-- name: GetActiveTaskLease :one
SELECT * FROM adaptive_task_leases WHERE task_id=? AND released_at IS NULL;

-- name: ListExpiredTaskLeases :many
SELECT * FROM adaptive_task_leases WHERE released_at IS NULL AND expires_at <= ? AND attempt_id > ? ORDER BY attempt_id LIMIT ?;

-- name: RenewTaskLease :execrows
UPDATE adaptive_task_leases SET heartbeat_at=sqlc.arg(now), last_activity_at=COALESCE(sqlc.narg(activity_at),last_activity_at), expires_at=sqlc.arg(expires_at)
WHERE attempt_id=sqlc.arg(attempt_id) AND generation=sqlc.arg(generation) AND holder_id=sqlc.arg(holder_id) AND released_at IS NULL;

-- name: RecoverTaskLease :execrows
UPDATE adaptive_task_leases SET holder_id=sqlc.arg(new_holder_id), heartbeat_at=sqlc.arg(now), expires_at=sqlc.arg(expires_at)
WHERE attempt_id=sqlc.arg(attempt_id) AND generation=sqlc.arg(generation) AND holder_id=sqlc.arg(holder_id) AND released_at IS NULL;

-- name: ReleaseTaskLease :execrows
UPDATE adaptive_task_leases SET released_at=sqlc.arg(now), release_reason=sqlc.arg(reason)
WHERE attempt_id=sqlc.arg(attempt_id) AND generation=sqlc.arg(generation) AND holder_id=sqlc.arg(holder_id) AND released_at IS NULL;

-- name: InsertTaskWorkerDispatch :exec
INSERT INTO adaptive_task_dispatches(attempt_id,session_id,configuration_hash,created_at) VALUES(?,?,?,?);

-- name: GetTaskWorkerDispatch :one
SELECT * FROM adaptive_task_dispatches WHERE attempt_id=?;

-- name: GetTaskWorkerDispatchBySession :one
SELECT * FROM adaptive_task_dispatches WHERE session_id=?;
