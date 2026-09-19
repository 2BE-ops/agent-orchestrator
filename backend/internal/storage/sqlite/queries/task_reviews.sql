-- name: InsertTaskReviewContext :exec
INSERT INTO adaptive_task_review_contexts(run_id,result_id,scope_hash,snapshot,content_hash,launch_id,created_at)
VALUES (?,?,?,?,?,?,?);

-- name: GetTaskReviewContext :one
SELECT * FROM adaptive_task_review_contexts WHERE run_id=?;

-- name: CountTaskReviewContexts :one
SELECT count(*) FROM adaptive_task_review_contexts WHERE result_id=?;

-- name: MarkTaskReviewStarted :execrows
UPDATE adaptive_task_review_contexts SET started_at=sqlc.arg(started_at)
WHERE run_id=sqlc.arg(run_id) AND launch_id=sqlc.arg(launch_id) AND started_at IS NULL
AND EXISTS (SELECT 1 FROM review_run r JOIN review v ON v.id=r.review_id
 WHERE r.id=adaptive_task_review_contexts.run_id AND r.status IN ('running','complete','delivered')
 AND v.reviewer_launch_id=adaptive_task_review_contexts.launch_id AND v.reviewer_handle_id!='');
