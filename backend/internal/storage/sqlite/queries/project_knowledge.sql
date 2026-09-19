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

-- name: SelectContextKnowledge :many
WITH relevant AS (
  SELECT v.*,
    CASE
      WHEN json_extract(v.definition,'$.pinned')=1 THEN 0
      WHEN EXISTS (SELECT 1 FROM json_each(v.definition,'$.taskIds') t JOIN json_each(sqlc.arg(task_ids)) requested ON t.value=requested.value) THEN 1
      WHEN sqlc.arg(category)<>'' AND EXISTS (SELECT 1 FROM json_each(v.definition,'$.tags') tag WHERE tag.value=sqlc.arg(category)) THEN 2
      WHEN COALESCE(json_array_length(v.definition,'$.taskIds'),0)=0 AND COALESCE(json_array_length(v.definition,'$.tags'),0)=0 THEN 3
      ELSE 4
    END AS relevance
  FROM project_knowledge k JOIN project_knowledge_versions v ON v.knowledge_id=k.id AND v.number=k.version
  WHERE k.project_id=sqlc.arg(project_id) AND json_extract(v.definition,'$.status')='accepted'
    AND (CASE COALESCE(json_extract(v.definition,'$.classification'),'technical') WHEN 'technical' THEN 0 WHEN 'engagement' THEN 1 WHEN 'mission' THEN 2 ELSE 3 END) <= CAST(sqlc.arg(max_class_rank) AS INTEGER)
    AND (COALESCE(json_extract(v.definition,'$.classification'),'technical')='technical'
      OR COALESCE(json_extract(v.definition,'$.engagementId'),'')=''
      OR json_extract(v.definition,'$.engagementId')=sqlc.arg(engagement_id))
)
SELECT knowledge_id,number,definition,content_hash,actor,reason,created_at FROM relevant
WHERE relevance<4 ORDER BY relevance,knowledge_id LIMIT sqlc.arg(page_limit);
