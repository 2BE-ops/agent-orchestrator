-- name: InsertTaskMessage :one
INSERT INTO adaptive_task_messages(id,project_id,task_id,attempt_id,session_id,native_generation,source_owner,task_revision,criteria_version,configuration_hash,configuration_sequence,context_hash,target_task_id,correlation_id,reply_to_id,result_id,idempotency_key,definition,content_hash,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING *;

-- name: GetTaskMessage :one
SELECT * FROM adaptive_task_messages WHERE id=?;

-- name: GetTaskMessageByKey :one
SELECT * FROM adaptive_task_messages WHERE attempt_id=? AND idempotency_key=?;

-- name: CountTaskMessages :one
SELECT count(*) FROM adaptive_task_messages WHERE attempt_id=?;

-- name: CountProjectTaskMessages :one
SELECT count(*) FROM adaptive_task_messages WHERE project_id=?;

-- name: ListTaskMessages :many
SELECT * FROM adaptive_task_messages WHERE project_id=sqlc.arg(project_id) AND sequence>sqlc.arg(after_sequence)
AND (sqlc.arg(task_id)='' OR task_id=sqlc.arg(task_id) OR target_task_id=sqlc.arg(task_id))
ORDER BY sequence LIMIT sqlc.arg(page_limit);

-- name: ListPendingTaskMessages :many
SELECT * FROM adaptive_task_messages m WHERE sequence>?
AND NOT EXISTS(SELECT 1 FROM adaptive_task_message_deliveries d WHERE d.message_id=m.id AND d.state!='not_sent')
AND (SELECT count(*) FROM adaptive_task_message_deliveries d WHERE d.message_id=m.id)<4
ORDER BY sequence LIMIT ?;

-- name: InsertTaskMessageDelivery :exec
INSERT INTO adaptive_task_message_deliveries(id,message_id,number,target_attempt_id,session_id,owner,delivery_key,state,reason,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,'dispatching','Reserved before native delivery',?,?);

-- name: GetTaskMessageDelivery :one
SELECT * FROM adaptive_task_message_deliveries WHERE id=?;

-- name: ListTaskMessageDeliveries :many
SELECT * FROM adaptive_task_message_deliveries WHERE message_id=? ORDER BY number;

-- name: ListUnresolvedTaskMessageDeliveries :many
SELECT * FROM adaptive_task_message_deliveries WHERE state='dispatching' AND id>? ORDER BY id LIMIT ?;

-- name: ResolveTaskMessageDelivery :execrows
UPDATE adaptive_task_message_deliveries SET state=?, reason=?, updated_at=? WHERE id=? AND state='dispatching';
