-- name: CreateRegistryEntry :exec
INSERT INTO adaptive_registry (id, kind, name, description, origin, created_by, enabled,
 manager_can_select, manager_can_modify, manager_can_version, revision, active_version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?);

-- name: GetRegistryEntry :one
SELECT * FROM adaptive_registry WHERE id = ?;

-- name: ListRegistryEntries :many
SELECT * FROM adaptive_registry
WHERE kind = sqlc.arg(kind) AND id > sqlc.arg(after_id)
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: CreateRegistryVersion :exec
INSERT INTO adaptive_registry_versions (entry_id, number, kind, parent_version, definition,
 content_hash, actor_origin, actor_id, reason, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CreateRegistrySkillPin :exec
INSERT INTO adaptive_registry_skill_pins (entry_id, version, position, skill_id, skill_version)
VALUES (?, ?, ?, ?, ?);

-- name: GetRegistryVersion :one
SELECT * FROM adaptive_registry_versions WHERE entry_id = ? AND number = ?;

-- name: ListRegistryVersions :many
SELECT * FROM adaptive_registry_versions
WHERE entry_id = sqlc.arg(entry_id) AND number > sqlc.arg(after_version)
ORDER BY number LIMIT sqlc.arg(page_limit);

-- name: NextRegistryVersion :one
SELECT CAST(COALESCE(MAX(number), 0) + 1 AS INTEGER) FROM adaptive_registry_versions WHERE entry_id = ?;

-- name: UpdateRegistryMetadata :execrows
UPDATE adaptive_registry SET name = ?, description = ?, enabled = ?,
 manager_can_select = ?, manager_can_modify = ?, manager_can_version = ?,
 revision = revision + 1, updated_at = ?
WHERE id = ? AND revision = ?;

-- name: AdvanceRegistryRevision :execrows
UPDATE adaptive_registry SET revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?;

-- name: ActivateRegistryVersion :execrows
UPDATE adaptive_registry SET active_version = ?, revision = revision + 1, updated_at = ?
WHERE id = ? AND revision = ?;

-- name: CreateRegistryAudit :exec
INSERT INTO adaptive_registry_audit (entry_id, revision, action, version_number, actor_origin, actor_id, reason, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListRegistryAudit :many
SELECT * FROM adaptive_registry_audit
WHERE entry_id = sqlc.arg(entry_id) AND seq > sqlc.arg(after_seq)
ORDER BY seq LIMIT sqlc.arg(page_limit);
