-- name: InsertTaskEvaluation :exec
INSERT INTO adaptive_task_evaluations(id,project_id,task_id,attempt_id,result_id,number,task_revision,criteria_version,idempotency_key,request_hash,snapshot,content_hash,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?);

-- name: GetTaskEvaluation :one
SELECT * FROM adaptive_task_evaluations WHERE id=?;

-- name: GetTaskEvaluationByKey :one
SELECT * FROM adaptive_task_evaluations WHERE attempt_id=? AND idempotency_key=?;

-- name: LatestTaskEvaluation :one
SELECT * FROM adaptive_task_evaluations WHERE attempt_id=? ORDER BY number DESC LIMIT 1;

-- name: ListTaskEvaluations :many
SELECT * FROM adaptive_task_evaluations WHERE attempt_id=? AND number>? ORDER BY number LIMIT ?;

-- name: CollectTaskCIChecks :many
SELECT p.url AS pr_url,p.head_sha,c.name,c.commit_hash,c.status,c.conclusion,p.ci_observed_at,c.observed_at,c.url
FROM pr p JOIN pr_checks c ON c.pr_url=p.url
WHERE p.session_id=sqlc.arg(session_id)
AND c.commit_hash=sqlc.arg(target_commit)
AND c.name IN (SELECT CAST(value AS TEXT) FROM json_each(sqlc.arg(check_names)))
ORDER BY p.url,c.name,c.commit_hash LIMIT 129;

-- name: CollectTaskPRFacts :many
SELECT url,head_sha,mergeability,is_merged,is_closed,is_draft,observed_at
FROM pr WHERE session_id=? ORDER BY url LIMIT 17;

-- name: CollectTaskReviewFacts :many
-- Latest pass per PR and harness at this exact commit, including incomplete
-- passes. An older approval cannot hide a newer running or failed review.
SELECT r.id,r.review_id,r.pr_url,r.target_sha,r.harness,r.status,r.verdict,
CAST(substr(r.body,1,4096) AS TEXT) AS body_preview,
CAST(length(CAST(r.body AS BLOB)) AS INTEGER) AS body_bytes,r.created_at
FROM review_run r
WHERE r.session_id=sqlc.arg(session_id) AND r.target_sha=sqlc.arg(target_commit)
AND NOT EXISTS (SELECT 1 FROM review_run newer
 WHERE newer.session_id=r.session_id AND newer.pr_url=r.pr_url
 AND newer.target_sha=r.target_sha AND newer.harness=r.harness
 AND (newer.created_at>r.created_at OR (newer.created_at=r.created_at AND newer.id>r.id)))
ORDER BY r.pr_url,r.harness,r.id LIMIT 33;
