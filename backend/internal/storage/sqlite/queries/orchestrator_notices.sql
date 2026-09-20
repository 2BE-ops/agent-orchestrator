-- name: InsertOrchestratorNotice :execrows
INSERT INTO adaptive_orchestrator_notices(id, project_id, task_id, fact, anchor, revision, detail, state, reason, created_at, resolved_at)
VALUES(?, ?, ?, ?, ?, ?, ?, 'pending', '', ?, NULL);

-- name: GetOrchestratorNoticeById :one
SELECT * FROM adaptive_orchestrator_notices WHERE id=?;

-- name: GetOrchestratorNoticeByAnchor :one
SELECT * FROM adaptive_orchestrator_notices WHERE project_id=? AND task_id=? AND fact=? AND anchor=?;

-- name: ResolveOrchestratorNotice :execrows
UPDATE adaptive_orchestrator_notices
SET state=?, reason=?, resolved_at=?
WHERE id=? AND state='pending' AND resolved_at IS NULL;

-- name: ListPendingOrchestratorNotices :many
SELECT * FROM adaptive_orchestrator_notices WHERE state='pending' ORDER BY created_at, id LIMIT ?;

-- name: ListOrchestratorNoticeProjects :many
SELECT DISTINCT s.project_id AS project_id
FROM sessions s
WHERE s.kind='orchestrator' AND s.project_id > ? AND s.project_id != ''
ORDER BY s.project_id LIMIT ?;
