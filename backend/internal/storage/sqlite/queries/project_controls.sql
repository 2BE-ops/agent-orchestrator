-- name: GetProjectControl :one
SELECT * FROM adaptive_project_controls WHERE project_id=?;

-- name: InsertProjectControl :exec
INSERT INTO adaptive_project_controls(project_id,state,actor,reason,updated_at) VALUES (?,?,?,?,?);

-- name: UpdateProjectControlState :execrows
UPDATE adaptive_project_controls SET state=?,actor=?,reason=?,updated_at=? WHERE project_id=?;

-- name: CountProjectActiveAttempts :one
SELECT COUNT(*) FROM adaptive_task_attempts a
 JOIN adaptive_tasks t ON t.id=a.task_id
 JOIN adaptive_task_leases l ON l.attempt_id=a.id
 WHERE t.project_id=? AND l.released_at IS NULL;

-- name: InsertTaskNeedsHuman :exec
INSERT INTO adaptive_task_needs_human(id,task_id,project_id,reason_code,detail,actor,snapshot,content_hash,created_at) VALUES (?,?,?,?,?,?,?,?,?);

-- name: GetTaskNeedsHuman :one
SELECT * FROM adaptive_task_needs_human WHERE id=?;

-- name: GetPendingTaskNeedsHuman :one
SELECT * FROM adaptive_task_needs_human WHERE task_id=? AND resolved_at IS NULL;

-- name: ListProjectNeedsHumanIDs :many
SELECT id FROM adaptive_task_needs_human WHERE project_id=? AND resolved_at IS NULL AND id > ? ORDER BY id LIMIT ?;

-- name: ResolveTaskNeedsHuman :execrows
UPDATE adaptive_task_needs_human SET resolved_at=?,resolution=?,resolved_by=? WHERE task_id=? AND resolved_at IS NULL;

-- name: NeedsHumanTaskAncestor :one
WITH RECURSIVE ancestry(id,parent_id,depth) AS (
    SELECT seed.id,seed.parent_id,0 FROM adaptive_tasks seed WHERE seed.id=?
    UNION ALL
    SELECT t.id,t.parent_id,a.depth+1 FROM adaptive_tasks t JOIN ancestry a ON t.id=a.parent_id WHERE a.depth<8
)
SELECT n.task_id FROM ancestry a JOIN adaptive_task_needs_human n ON n.task_id=a.id
WHERE n.resolved_at IS NULL
ORDER BY a.depth LIMIT 1;

-- name: ListProjectActiveAttemptSessions :many
SELECT s.id FROM adaptive_task_attempts a
 JOIN adaptive_tasks t ON t.id=a.task_id
 JOIN adaptive_task_leases l ON l.attempt_id=a.id
 JOIN adaptive_task_dispatches d ON d.attempt_id=a.id
 JOIN sessions s ON s.id=d.session_id
 WHERE t.project_id=? AND l.released_at IS NULL AND s.is_terminated=0 AND s.kind='worker'
 ORDER BY s.id;
