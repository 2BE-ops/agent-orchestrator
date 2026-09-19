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
