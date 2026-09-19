-- +goose Up
-- Historical checks have no per-check observation provenance. Leave them unknown
-- until a subsequent SCM observation actually includes them.
ALTER TABLE pr_checks ADD COLUMN observed_at DATETIME;

-- +goose Down
ALTER TABLE pr_checks DROP COLUMN observed_at;
