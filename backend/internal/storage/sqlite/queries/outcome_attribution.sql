-- Routing attribution pages over the request sequence because it is the only
-- monotonic cursor in the Manager inbox; a request enters the cohort only when
-- at least one sealed decision exists for it.
-- name: ListRoutingAttributionRequests :many
SELECT r.id AS request_id
FROM adaptive_agent_manager_requests r
WHERE r.project_id = sqlc.arg(project_id)
  AND r.sequence > sqlc.arg(after_sequence)
  AND EXISTS(SELECT 1 FROM adaptive_agent_manager_decisions d WHERE d.request_id = r.id)
ORDER BY r.sequence
LIMIT sqlc.arg(page_limit);

-- name: CountRoutingAttributionRequestsInWindow :one
SELECT count(DISTINCT r.id)
FROM adaptive_agent_manager_requests r
JOIN adaptive_agent_manager_decisions d ON d.request_id = r.id
WHERE r.project_id = sqlc.arg(project_id)
  AND d.created_at >= sqlc.arg(from_time)
  AND d.created_at <= sqlc.arg(to_time);

-- name: ListRoutingAttributionRequestIDsInWindow :many
SELECT DISTINCT r.id AS request_id
FROM adaptive_agent_manager_requests r
JOIN adaptive_agent_manager_decisions d ON d.request_id = r.id
WHERE r.project_id = sqlc.arg(project_id)
  AND d.created_at >= sqlc.arg(from_time)
  AND d.created_at <= sqlc.arg(to_time)
ORDER BY r.id
LIMIT sqlc.arg(page_limit);

-- name: CountOrchestratorPlanReceiptsInWindow :one
SELECT count(*) FROM adaptive_orchestrator_plan_receipts
WHERE project_id = sqlc.arg(project_id)
  AND created_at >= sqlc.arg(from_time)
  AND created_at <= sqlc.arg(to_time);

-- name: ListOrchestratorPlanReceiptsInWindow :many
SELECT * FROM adaptive_orchestrator_plan_receipts
WHERE project_id = sqlc.arg(project_id)
  AND created_at >= sqlc.arg(from_time)
  AND created_at <= sqlc.arg(to_time)
ORDER BY id
LIMIT sqlc.arg(page_limit);
