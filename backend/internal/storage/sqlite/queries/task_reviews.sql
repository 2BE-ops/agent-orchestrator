-- name: InsertTaskReviewContext :exec
INSERT INTO adaptive_task_review_contexts(run_id,result_id,scope_hash,snapshot,content_hash,launch_id,created_at)
VALUES (?,?,?,?,?,?,?);

-- name: GetTaskReviewContext :one
SELECT * FROM adaptive_task_review_contexts WHERE run_id=?;

-- name: CountTaskReviewContexts :one
SELECT count(*) FROM adaptive_task_review_contexts WHERE result_id=?;

-- name: ListTaskReviewRuns :many
SELECT r.* FROM review_run r JOIN adaptive_task_review_contexts c ON c.run_id=r.id
WHERE c.result_id=? ORDER BY r.created_at,r.id LIMIT 64;

-- name: MarkTaskReviewStarted :execrows
UPDATE adaptive_task_review_contexts SET started_at=sqlc.arg(started_at)
WHERE run_id=sqlc.arg(run_id) AND launch_id=sqlc.arg(launch_id) AND started_at IS NULL
AND EXISTS (SELECT 1 FROM review_run r JOIN review v ON v.id=r.review_id
 WHERE r.id=adaptive_task_review_contexts.run_id AND r.status IN ('running','complete','delivered')
 AND v.reviewer_launch_id=adaptive_task_review_contexts.launch_id AND v.reviewer_handle_id!='');

-- name: SubmitTaskReviewResult :execrows
UPDATE review_run SET status='complete', verdict=sqlc.arg(verdict), body=sqlc.arg(body),
github_review_id=sqlc.arg(github_review_id), auto_inject_review=sqlc.arg(auto_inject_review)
WHERE review_run.id=sqlc.arg(run_id) AND review_run.session_id=sqlc.arg(session_id) AND review_run.status='running' AND review_run.task_scope!=''
AND EXISTS(SELECT 1 FROM adaptive_task_review_contexts c JOIN review v ON v.id=review_run.review_id
 WHERE c.run_id=review_run.id AND c.launch_id=sqlc.arg(source_generation)
 AND v.reviewer_launch_id=c.launch_id);
