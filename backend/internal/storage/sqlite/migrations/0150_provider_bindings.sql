-- +goose Up
CREATE TABLE adaptive_provider_bindings (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    harness TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    project_id TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE adaptive_provider_binding_audit (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    binding_id TEXT NOT NULL REFERENCES adaptive_provider_bindings(id),
    revision INTEGER NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('created','updated')),
    actor_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
    created_at DATETIME NOT NULL,
    UNIQUE(binding_id, revision)
);

-- +goose StatementBegin
CREATE TRIGGER adaptive_binding_identity_immutable BEFORE UPDATE ON adaptive_provider_bindings
WHEN NEW.id != OLD.id OR NEW.harness != OLD.harness OR NEW.provider != OLD.provider
  OR NEW.project_id != OLD.project_id OR NEW.created_at != OLD.created_at OR NEW.revision != OLD.revision+1
BEGIN SELECT RAISE(ABORT, 'provider binding identity is immutable; increment revision'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_binding_no_delete BEFORE DELETE ON adaptive_provider_bindings
BEGIN SELECT RAISE(ABORT, 'disable provider bindings instead of deleting'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_binding_audit_no_update BEFORE UPDATE ON adaptive_provider_binding_audit
BEGIN SELECT RAISE(ABORT, 'provider binding audit is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_binding_audit_no_delete BEFORE DELETE ON adaptive_provider_binding_audit
BEGIN SELECT RAISE(ABORT, 'provider binding audit is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_binding_audit_cdc AFTER INSERT ON adaptive_provider_binding_audit
BEGIN
    INSERT INTO change_log(project_id, session_id, event_type, payload, created_at)
    VALUES(NULL, NULL, 'registry_changed', json_object('providerBindingId',NEW.binding_id,'revision',NEW.revision,'action',NEW.action), NEW.created_at);
END;
-- +goose StatementEnd

-- +goose Down
-- Retained references must never be dropped during downgrade.
DELETE FROM adaptive_provider_bindings;
DROP TRIGGER adaptive_binding_audit_cdc;
DROP TRIGGER adaptive_binding_audit_no_delete;
DROP TRIGGER adaptive_binding_audit_no_update;
DROP TRIGGER adaptive_binding_no_delete;
DROP TRIGGER adaptive_binding_identity_immutable;
DROP TABLE adaptive_provider_binding_audit;
DROP TABLE adaptive_provider_bindings;
