-- name: InsertWorkerNativeChange :exec
INSERT INTO adaptive_worker_native_changes (id,session_id,conversation_id,owner,previous_activation,previous_options,requested,created_at)
VALUES (?,?,?,?,?,?,?,?);

-- name: PendingWorkerNativeChange :one
SELECT c.* FROM adaptive_worker_native_changes c
WHERE c.session_id=? AND NOT EXISTS(SELECT 1 FROM adaptive_worker_native_resolutions r WHERE r.change_id=c.id)
ORDER BY c.created_at LIMIT 1;

-- name: GetWorkerNativeChange :one
SELECT * FROM adaptive_worker_native_changes WHERE id=?;

-- name: ResolveWorkerNativeChange :exec
INSERT INTO adaptive_worker_native_resolutions (change_id,outcome,execution_id,reason,created_at) VALUES (?,?,?,?,?);
