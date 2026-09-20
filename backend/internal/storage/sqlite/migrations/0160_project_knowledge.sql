-- +goose Up
CREATE TABLE project_knowledge (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK(version > 0),
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    FOREIGN KEY(id,version) REFERENCES project_knowledge_versions(knowledge_id,number) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX project_knowledge_project ON project_knowledge(project_id,id);
CREATE TABLE project_knowledge_versions (
    knowledge_id TEXT NOT NULL REFERENCES project_knowledge(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK(number > 0),
    definition TEXT NOT NULL CHECK(json_valid(definition) AND length(definition)<=65536),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    actor TEXT NOT NULL CHECK(json_valid(actor)),
    reason TEXT NOT NULL CHECK(length(trim(reason)) BETWEEN 1 AND 2000),
    created_at DATETIME NOT NULL,
    PRIMARY KEY(knowledge_id,number)
);
-- +goose StatementBegin
CREATE TRIGGER project_knowledge_version_immutable BEFORE UPDATE ON project_knowledge_versions
BEGIN SELECT RAISE(ABORT,'knowledge version is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER project_knowledge_version_no_delete BEFORE DELETE ON project_knowledge_versions
WHEN EXISTS(SELECT 1 FROM project_knowledge WHERE id=OLD.knowledge_id)
BEGIN SELECT RAISE(ABORT,'knowledge version belongs to retained identity'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER project_knowledge_version_cdc AFTER INSERT ON project_knowledge_versions
BEGIN
    INSERT INTO change_log(project_id,event_type,payload,created_at)
    SELECT project_id,'project_knowledge_changed',json_object('knowledgeId',NEW.knowledge_id,'version',NEW.number),NEW.created_at
    FROM project_knowledge WHERE id=NEW.knowledge_id;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER project_knowledge_version_no_delete;
DELETE FROM project_knowledge;
DROP TABLE project_knowledge_versions;
DROP TABLE project_knowledge;
