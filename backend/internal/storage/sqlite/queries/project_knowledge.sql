-- name: InsertProjectKnowledge :exec
INSERT INTO project_knowledge(id,project_id,version,created_at,updated_at) VALUES (?,?,1,?,?);

-- name: GetProjectKnowledge :one
SELECT * FROM project_knowledge WHERE id=?;

-- name: ActivateKnowledgeVersion :execrows
UPDATE project_knowledge SET version=?,updated_at=? WHERE id=? AND version=?;

-- name: CountProjectKnowledge :one
SELECT count(*) FROM project_knowledge WHERE project_id=?;

-- name: InsertKnowledgeVersion :exec
INSERT INTO project_knowledge_versions(knowledge_id,number,definition,content_hash,actor,reason,created_at) VALUES (?,?,?,?,?,?,?);

-- name: GetKnowledgeVersion :one
SELECT * FROM project_knowledge_versions WHERE knowledge_id=? AND number=?;

-- name: ListKnowledgeVersions :many
SELECT * FROM project_knowledge_versions WHERE knowledge_id=? AND number > ? ORDER BY number LIMIT ?;

-- name: ListProjectKnowledge :many
SELECT k.* FROM project_knowledge k JOIN project_knowledge_versions v ON v.knowledge_id=k.id AND v.number=k.version
WHERE k.project_id=sqlc.arg(project_id) AND k.id > sqlc.arg(after_id)
  AND ((sqlc.arg(status)='' AND json_extract(v.definition,'$.status') <> 'deleted') OR json_extract(v.definition,'$.status')=sqlc.arg(status))
  AND (sqlc.arg(kind)='' OR json_extract(v.definition,'$.kind')=sqlc.arg(kind))
  AND (sqlc.arg(search)='' OR instr(lower(json_extract(v.definition,'$.title') || ' ' || json_extract(v.definition,'$.content')),lower(sqlc.arg(search))) > 0)
ORDER BY k.id LIMIT sqlc.arg(page_limit);
