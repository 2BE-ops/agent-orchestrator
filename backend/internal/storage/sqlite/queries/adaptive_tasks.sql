-- name: InsertAdaptiveTask :exec
INSERT INTO adaptive_tasks(id,project_id,revision,parent_id,created_by,created_at,updated_at) VALUES (?,?,1,?,?,?,?);

-- name: GetAdaptiveTask :one
SELECT * FROM adaptive_tasks WHERE id=?;

-- name: ListAdaptiveTaskIDs :many
SELECT id FROM adaptive_tasks WHERE project_id=? AND id > ? ORDER BY id LIMIT ?;

-- name: AdaptiveTaskGraphNodes :many
SELECT id,parent_id FROM adaptive_tasks WHERE project_id=?;

-- name: AdaptiveTaskGraphState :many
SELECT id,parent_id,revision FROM adaptive_tasks WHERE project_id=?;

-- name: AdaptiveTaskGraphEdges :many
SELECT task_id,dependency_id FROM adaptive_task_dependencies WHERE project_id=?;

-- name: ActivateAdaptiveTaskRevision :execrows
UPDATE adaptive_tasks SET revision=?, parent_id=?, updated_at=? WHERE id=? AND revision=?;

-- name: InsertAdaptiveTaskRevision :exec
INSERT INTO adaptive_task_revisions(task_id,number,criteria_version,definition,content_hash,actor,reason,created_at) VALUES (?,?,?,?,?,?,?,?);

-- name: GetAdaptiveTaskRevision :one
SELECT * FROM adaptive_task_revisions WHERE task_id=? AND number=?;

-- name: ListAdaptiveTaskRevisions :many
SELECT * FROM adaptive_task_revisions WHERE task_id=? AND number > ? ORDER BY number LIMIT ?;

-- name: InsertAdaptiveTaskCriteria :exec
INSERT INTO adaptive_task_criteria(task_id,number,previous_version,definition,content_hash,actor,reason,created_at) VALUES (?,?,?,?,?,?,?,?);

-- name: GetAdaptiveTaskCriteria :one
SELECT * FROM adaptive_task_criteria WHERE task_id=? AND number=?;

-- name: ClearAdaptiveTaskDependencies :exec
DELETE FROM adaptive_task_dependencies WHERE task_id=?;

-- name: InsertAdaptiveTaskDependency :exec
INSERT INTO adaptive_task_dependencies(project_id,task_id,dependency_id) VALUES (?,?,?);

-- name: InsertAdaptiveTaskAudit :exec
INSERT INTO adaptive_task_audit(task_id,revision,action,actor,reason,created_at) VALUES (?,?,?,?,?,?);

-- name: ListAdaptiveTaskAudit :many
SELECT * FROM adaptive_task_audit WHERE task_id=? AND seq > ? ORDER BY seq LIMIT ?;
