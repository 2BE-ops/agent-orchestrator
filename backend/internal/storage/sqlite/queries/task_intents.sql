-- name: InsertTaskIntent :exec
INSERT INTO adaptive_task_intents(task_id,version,task_revision,intent,actor,reason,created_at) VALUES (?,?,?,?,?,?,?);

-- name: GetTaskIntent :one
SELECT * FROM adaptive_task_intents WHERE task_id=? ORDER BY version DESC LIMIT 1;

-- name: ListTaskIntents :many
SELECT * FROM adaptive_task_intents WHERE task_id=? AND version > ? ORDER BY version LIMIT ?;

-- name: CancelledTaskAncestor :one
WITH RECURSIVE ancestry(id,parent_id,depth) AS (
    SELECT seed.id,seed.parent_id,0 FROM adaptive_tasks seed WHERE seed.id=?
    UNION ALL
    SELECT t.id,t.parent_id,a.depth+1 FROM adaptive_tasks t JOIN ancestry a ON t.id=a.parent_id WHERE a.depth<8
)
SELECT a.id FROM ancestry a JOIN adaptive_task_intents i ON i.task_id=a.id
WHERE i.intent='cancel' AND i.version=(SELECT MAX(version) FROM adaptive_task_intents WHERE task_id=a.id)
ORDER BY a.depth LIMIT 1;
