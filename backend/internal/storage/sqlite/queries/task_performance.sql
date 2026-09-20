-- name: ListTaskPerformanceAttempts :many
SELECT a.* FROM adaptive_task_attempts a JOIN adaptive_tasks t ON t.id=a.task_id
WHERE t.project_id=sqlc.arg(project_id) AND a.created_at>=sqlc.arg(window_start)
AND a.created_at<sqlc.arg(window_end) AND a.id>sqlc.arg(after_id)
ORDER BY a.id LIMIT sqlc.arg(page_limit);

-- name: TaskPerformanceAssessmentStats :one
SELECT count(*) AS evaluations,
CAST(EXISTS(SELECT 1 FROM adaptive_task_evaluations e,json_each(e.snapshot,'$.definition.criteria') decision
 WHERE e.attempt_id=sqlc.arg(attempt_id) AND json_extract(decision.value,'$.outcome')='failed'
 AND json_extract(decision.value,'$.criterionId') IN (SELECT value FROM json_each(sqlc.arg(ci_criteria)))) AS INTEGER) AS ci_failure_observed
FROM adaptive_task_evaluations WHERE attempt_id=sqlc.arg(attempt_id);

-- name: FirstTaskEvaluation :one
SELECT * FROM adaptive_task_evaluations WHERE attempt_id=? ORDER BY number LIMIT 1;

-- name: TaskPerformanceReviewChanges :one
SELECT count(*) FROM review_run r JOIN adaptive_task_review_contexts c ON c.run_id=r.id
JOIN adaptive_task_results result ON result.id=c.result_id
WHERE result.attempt_id=? AND c.started_at IS NOT NULL
AND r.status IN ('complete','delivered') AND r.verdict='changes_requested';

-- name: TaskPerformanceActivations :one
SELECT count(*) FROM adaptive_worker_execution_activations WHERE session_id=?;

-- name: TaskPerformanceUsage :one
SELECT count(*) AS events,
CAST(count(CASE WHEN e.usage_measurement_kind='native_reported' THEN 1 END) AS INTEGER) AS native_reported_events,
CAST(count(CASE WHEN e.usage_measurement_kind IN ('ao_estimated','mixed') THEN 1 END) AS INTEGER) AS estimated_events,
CAST(count(CASE WHEN e.usage_measurement_kind NOT IN ('native_reported','ao_estimated','mixed') THEN 1 END) AS INTEGER) AS unknown_events,
CAST(count(e.input_tokens) AS INTEGER) AS known_input_events,
CAST(count(e.output_tokens) AS INTEGER) AS known_output_events,
CAST(COALESCE(sum(e.input_tokens),0) AS INTEGER) AS input_tokens,
CAST(COALESCE(sum(e.output_tokens),0) AS INTEGER) AS output_tokens,
CAST(count(e.estimated_cost_nanos) AS INTEGER) AS priced_events,
CAST(COALESCE(sum(e.estimated_cost_nanos),0) AS INTEGER) AS priced_cost_nanos
FROM model_usage_events e JOIN usage_bindings b ON b.id=e.binding_id WHERE b.session_id=?;
