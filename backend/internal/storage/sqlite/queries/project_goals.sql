-- name: UpsertProjectGoalPointer :execrows
INSERT INTO adaptive_project_goals(project_id,current_version,updated_at) VALUES (?,?,?)
ON CONFLICT(project_id) DO UPDATE SET current_version=excluded.current_version, updated_at=excluded.updated_at;

-- name: GetProjectGoalPointer :one
SELECT * FROM adaptive_project_goals WHERE project_id=?;

-- name: InsertProjectGoalVersion :exec
INSERT INTO adaptive_project_goal_versions(project_id,number,goal,actor,reason,content_hash,created_at) VALUES (?,?,?,?,?,?,?);

-- name: GetProjectGoalVersion :one
SELECT * FROM adaptive_project_goal_versions WHERE project_id=? AND number=?;

-- name: ListProjectGoalVersions :many
SELECT * FROM adaptive_project_goal_versions WHERE project_id=? AND number > ? ORDER BY number LIMIT ?;

-- name: NextProjectGoalVersion :one
SELECT COALESCE(MAX(number),0)+1 FROM adaptive_project_goal_versions WHERE project_id=?;

-- name: InsertProjectGoalCompletion :exec
INSERT INTO adaptive_project_goal_completions(id,project_id,goal_version,summary,evidence,actor,reason,created_at) VALUES (?,?,?,?,?,?,?,?);

-- name: GetProjectGoalCompletion :one
SELECT * FROM adaptive_project_goal_completions WHERE project_id=? AND goal_version=?;

-- name: GetProjectGoalCompletionByID :one
SELECT * FROM adaptive_project_goal_completions WHERE project_id=? AND id=?;

-- name: ListProjectGoalCompletions :many
SELECT * FROM adaptive_project_goal_completions WHERE project_id=? AND id > ? ORDER BY id LIMIT ?;

-- name: InsertOrchestratorPlanReceipt :exec
INSERT INTO adaptive_orchestrator_plan_receipts(id,project_id,session_id,idempotency_key,action_kind,request_hash,outcome,created_at) VALUES (?,?,?,?,?,?,?,?);

-- name: GetOrchestratorPlanReceiptByKey :one
SELECT * FROM adaptive_orchestrator_plan_receipts WHERE project_id=? AND idempotency_key=?;

-- name: GetOrchestratorPlanReceiptByID :one
SELECT * FROM adaptive_orchestrator_plan_receipts WHERE project_id=? AND id=?;

-- name: ListOrchestratorPlanReceipts :many
SELECT * FROM adaptive_orchestrator_plan_receipts WHERE project_id=? AND id > ? ORDER BY id LIMIT ?;

-- name: CountProjectGoalTasks :one
SELECT count(*) FROM adaptive_tasks WHERE project_id=?;
