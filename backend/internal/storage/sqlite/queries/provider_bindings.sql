-- name: CreateProviderBinding :exec
INSERT INTO adaptive_provider_bindings(id,name,harness,provider,project_id,enabled,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?);

-- name: GetProviderBinding :one
SELECT * FROM adaptive_provider_bindings WHERE id=?;

-- name: ListProviderBindings :many
SELECT * FROM adaptive_provider_bindings WHERE id > sqlc.arg(after_id)
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: UpdateProviderBinding :execrows
UPDATE adaptive_provider_bindings SET name=?,enabled=?,revision=revision+1,updated_at=?
WHERE id=? AND revision=sqlc.arg(expected_revision);

-- name: CreateProviderBindingAudit :exec
INSERT INTO adaptive_provider_binding_audit(binding_id,revision,action,actor_id,reason,name,enabled,created_at)
VALUES(?,?,?,?,?,?,?,?);

-- name: ListProviderBindingAudit :many
SELECT * FROM adaptive_provider_binding_audit WHERE binding_id=? AND seq > sqlc.arg(after_seq)
ORDER BY seq LIMIT sqlc.arg(page_limit);
