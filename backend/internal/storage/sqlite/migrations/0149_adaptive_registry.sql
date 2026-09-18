-- +goose Up
CREATE TABLE adaptive_registry (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('agent_type', 'skill')),
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    description TEXT NOT NULL,
    origin TEXT NOT NULL CHECK (origin IN ('USER', 'AGENT_MANAGER', 'SYSTEM')),
    created_by TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    manager_can_select INTEGER NOT NULL CHECK (manager_can_select IN (0, 1)),
    manager_can_modify INTEGER NOT NULL CHECK (manager_can_modify IN (0, 1)),
    manager_can_version INTEGER NOT NULL CHECK (manager_can_version IN (0, 1)),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    active_version INTEGER NOT NULL CHECK (active_version >= 1),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (id, kind),
    FOREIGN KEY (id, active_version, kind) REFERENCES adaptive_registry_versions (entry_id, number, kind) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX idx_adaptive_registry_kind ON adaptive_registry (kind, id);

CREATE TABLE adaptive_registry_versions (
    entry_id TEXT NOT NULL,
    number INTEGER NOT NULL CHECK (number >= 1),
    kind TEXT NOT NULL CHECK (kind IN ('agent_type', 'skill')),
    parent_version INTEGER,
    definition TEXT NOT NULL CHECK (json_valid(definition) AND length(definition) <= 2097152),
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
    actor_origin TEXT NOT NULL CHECK (actor_origin IN ('USER', 'AGENT_MANAGER', 'SYSTEM')),
    actor_id TEXT NOT NULL,
    reason TEXT NOT NULL CHECK (length(trim(reason)) > 0),
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (entry_id, number),
    UNIQUE (entry_id, number, kind),
    FOREIGN KEY (entry_id, kind) REFERENCES adaptive_registry (id, kind),
    FOREIGN KEY (entry_id, parent_version) REFERENCES adaptive_registry_versions (entry_id, number),
    CHECK (parent_version IS NULL OR parent_version < number)
);

-- Pins are inserted before their owning version in the same transaction. Once
-- that version exists no attachment can be added, removed or changed.
CREATE TABLE adaptive_registry_skill_pins (
    entry_id TEXT NOT NULL,
    version INTEGER NOT NULL,
    owner_kind TEXT NOT NULL DEFAULT 'agent_type' CHECK (owner_kind = 'agent_type'),
    position INTEGER NOT NULL CHECK (position >= 0 AND position < 32),
    skill_id TEXT NOT NULL,
    skill_version INTEGER NOT NULL,
    skill_kind TEXT NOT NULL DEFAULT 'skill' CHECK (skill_kind = 'skill'),
    PRIMARY KEY (entry_id, version, position),
    UNIQUE (entry_id, version, skill_id),
    FOREIGN KEY (entry_id, version, owner_kind) REFERENCES adaptive_registry_versions (entry_id, number, kind) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY (skill_id, skill_version, skill_kind) REFERENCES adaptive_registry_versions (entry_id, number, kind)
);
CREATE INDEX idx_adaptive_registry_skill_pins_skill ON adaptive_registry_skill_pins (skill_id, skill_version);

CREATE TABLE adaptive_registry_audit (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id TEXT NOT NULL REFERENCES adaptive_registry (id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    action TEXT NOT NULL CHECK (action IN ('created', 'version_created', 'metadata_updated', 'version_activated')),
    version_number INTEGER NOT NULL,
    actor_origin TEXT NOT NULL CHECK (actor_origin IN ('USER', 'AGENT_MANAGER', 'SYSTEM')),
    actor_id TEXT NOT NULL,
    reason TEXT NOT NULL CHECK (length(trim(reason)) > 0),
    created_at TIMESTAMP NOT NULL,
    UNIQUE (entry_id, revision),
    FOREIGN KEY (entry_id, version_number) REFERENCES adaptive_registry_versions (entry_id, number)
);

-- +goose StatementBegin
CREATE TRIGGER adaptive_registry_identity_immutable
BEFORE UPDATE ON adaptive_registry
WHEN NEW.id <> OLD.id OR NEW.kind <> OLD.kind OR NEW.origin <> OLD.origin
 OR NEW.created_by <> OLD.created_by OR NEW.created_at <> OLD.created_at
 OR NEW.revision <> OLD.revision + 1
BEGIN SELECT RAISE(ABORT, 'registry identity is immutable; revision must advance once'); END;

CREATE TRIGGER adaptive_registry_versions_no_update BEFORE UPDATE ON adaptive_registry_versions
BEGIN SELECT RAISE(ABORT, 'registry versions are immutable'); END;
CREATE TRIGGER adaptive_registry_versions_no_delete BEFORE DELETE ON adaptive_registry_versions
BEGIN SELECT RAISE(ABORT, 'registry versions are retained'); END;
CREATE TRIGGER adaptive_registry_pins_no_update BEFORE UPDATE ON adaptive_registry_skill_pins
BEGIN SELECT RAISE(ABORT, 'registry skill pins are immutable'); END;
CREATE TRIGGER adaptive_registry_pins_no_delete BEFORE DELETE ON adaptive_registry_skill_pins
BEGIN SELECT RAISE(ABORT, 'registry skill pins are retained'); END;
CREATE TRIGGER adaptive_registry_pins_sealed BEFORE INSERT ON adaptive_registry_skill_pins
WHEN EXISTS (SELECT 1 FROM adaptive_registry_versions WHERE entry_id = NEW.entry_id AND number = NEW.version)
BEGIN SELECT RAISE(ABORT, 'registry version is already sealed'); END;
CREATE TRIGGER adaptive_registry_audit_no_update BEFORE UPDATE ON adaptive_registry_audit
BEGIN SELECT RAISE(ABORT, 'registry audit is immutable'); END;
CREATE TRIGGER adaptive_registry_audit_no_delete BEFORE DELETE ON adaptive_registry_audit
BEGIN SELECT RAISE(ABORT, 'registry audit is retained'); END;

-- CDC belongs to triggers. One event per audited, committed business mutation;
-- only safe identity/revision fields enter the live invalidation channel.
CREATE TRIGGER adaptive_registry_audit_cdc AFTER INSERT ON adaptive_registry_audit
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NULL, NULL, 'registry_changed', json_object(
        'id', NEW.entry_id, 'revision', NEW.revision,
        'kind', (SELECT kind FROM adaptive_registry WHERE id = NEW.entry_id)), NEW.created_at);
END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE adaptive_registry_audit;
DROP TABLE adaptive_registry_skill_pins;
-- Circular historical references intentionally prevent destructive downgrade of
-- populated registries. Empty registries can be removed safely.
DROP TABLE adaptive_registry_versions;
DROP TABLE adaptive_registry;
