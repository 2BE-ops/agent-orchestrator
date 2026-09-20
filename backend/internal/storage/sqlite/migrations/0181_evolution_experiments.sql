-- +goose Up
CREATE TABLE adaptive_experiments (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    kind TEXT NOT NULL CHECK(kind IN ('agent_type','skill')),
    entry_id TEXT NOT NULL,
    control_version INTEGER NOT NULL CHECK(control_version>0),
    candidate_version INTEGER NOT NULL CHECK(candidate_version>0),
    hypothesis TEXT NOT NULL CHECK(length(trim(hypothesis)) BETWEEN 1 AND 2000),
    minimum_samples INTEGER NOT NULL CHECK(minimum_samples BETWEEN 1 AND 1000),
    status TEXT NOT NULL CHECK(status IN ('running','concluded')),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=65536),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    concluded_at DATETIME,
    conclusion TEXT CHECK(conclusion IS NULL OR (json_valid(conclusion) AND length(CAST(conclusion AS BLOB))<=262144)),
    CHECK(control_version != candidate_version),
    CHECK((status='running') = (conclusion IS NULL)),
    CHECK((status='running') = (concluded_at IS NULL)),
    FOREIGN KEY(entry_id, control_version) REFERENCES adaptive_registry_versions(entry_id,number),
    FOREIGN KEY(entry_id, candidate_version) REFERENCES adaptive_registry_versions(entry_id,number)
);
CREATE INDEX adaptive_experiments_project ON adaptive_experiments(project_id, id);
-- +goose StatementBegin
CREATE TRIGGER adaptive_experiment_scope BEFORE INSERT ON adaptive_experiments
WHEN NOT EXISTS(SELECT 1 FROM adaptive_registry_versions c
 JOIN adaptive_registry_versions d ON d.entry_id=c.entry_id AND d.kind=c.kind
 WHERE c.entry_id=NEW.entry_id AND c.number=NEW.control_version
 AND d.number=NEW.candidate_version AND c.kind=NEW.kind)
 OR coalesce(json_extract(NEW.snapshot,'$.id'),'')!=NEW.id
 OR coalesce(json_extract(NEW.snapshot,'$.projectId'),'')!=NEW.project_id
 OR coalesce(json_extract(NEW.snapshot,'$.kind'),'')!=NEW.kind
 OR coalesce(json_extract(NEW.snapshot,'$.entryId'),'')!=NEW.entry_id
 OR coalesce(json_extract(NEW.snapshot,'$.controlVersion'),0)!=NEW.control_version
 OR coalesce(json_extract(NEW.snapshot,'$.candidateVersion'),0)!=NEW.candidate_version
 OR coalesce(json_extract(NEW.snapshot,'$.minimumSamples'),0)!=NEW.minimum_samples
 OR coalesce(json_extract(NEW.snapshot,'$.status'),'')!=NEW.status
 OR coalesce(json_extract(NEW.snapshot,'$.conclusion'),'')!=''
BEGIN SELECT RAISE(ABORT,'Evolution experiment requires two existing versions of one entry and an exact running snapshot'); END;
CREATE TRIGGER adaptive_experiment_sealed BEFORE UPDATE ON adaptive_experiments
WHEN NOT (OLD.status='running' AND NEW.status='concluded'
 AND OLD.conclusion IS NULL AND NEW.conclusion IS NOT NULL
 AND OLD.concluded_at IS NULL AND NEW.concluded_at IS NOT NULL
 AND OLD.id=NEW.id AND OLD.project_id=NEW.project_id AND OLD.kind=NEW.kind
 AND OLD.entry_id=NEW.entry_id AND OLD.control_version=NEW.control_version
 AND OLD.candidate_version=NEW.candidate_version AND OLD.hypothesis=NEW.hypothesis
 AND OLD.minimum_samples=NEW.minimum_samples AND OLD.snapshot=NEW.snapshot
 AND OLD.content_hash=NEW.content_hash AND OLD.created_at=NEW.created_at
 AND json_extract(NEW.conclusion,'$.outcome') IN ('promote_candidate','keep_control','insufficient_evidence'))
BEGIN SELECT RAISE(ABORT,'Evolution experiment is sealed at creation and may only conclude once'); END;
CREATE TRIGGER adaptive_experiment_retained BEFORE DELETE ON adaptive_experiments
BEGIN SELECT RAISE(ABORT,'Evolution experiment history is retained'); END;

CREATE TABLE adaptive_recommendations (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    kind TEXT NOT NULL CHECK(kind IN ('agent_type','skill')),
    entry_id TEXT NOT NULL,
    from_version INTEGER NOT NULL CHECK(from_version>0),
    observation TEXT NOT NULL CHECK(length(trim(observation)) BETWEEN 1 AND 2000),
    sample_size INTEGER NOT NULL CHECK(sample_size BETWEEN 1 AND 1000),
    proposed TEXT NOT NULL CHECK(json_valid(proposed) AND length(CAST(proposed AS BLOB))<=2097152),
    proposed_hash TEXT NOT NULL CHECK(length(proposed_hash)=64),
    status TEXT NOT NULL CHECK(status IN ('pending','dismissed','adopted')),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=65536),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    created_at DATETIME NOT NULL,
    decided_at DATETIME,
    decision TEXT CHECK(decision IS NULL OR (json_valid(decision) AND length(CAST(decision AS BLOB))<=16384)),
    CHECK((status='pending') = (decision IS NULL)),
    CHECK((status='pending') = (decided_at IS NULL)),
    FOREIGN KEY(entry_id, from_version) REFERENCES adaptive_registry_versions(entry_id,number)
);
CREATE INDEX adaptive_recommendations_project ON adaptive_recommendations(project_id, id);
CREATE TRIGGER adaptive_recommendation_scope BEFORE INSERT ON adaptive_recommendations
WHEN NOT EXISTS(SELECT 1 FROM adaptive_registry_versions v
 WHERE v.entry_id=NEW.entry_id AND v.number=NEW.from_version AND v.kind=NEW.kind)
 OR coalesce(json_extract(NEW.snapshot,'$.id'),'')!=NEW.id
 OR coalesce(json_extract(NEW.snapshot,'$.projectId'),'')!=NEW.project_id
 OR coalesce(json_extract(NEW.snapshot,'$.kind'),'')!=NEW.kind
 OR coalesce(json_extract(NEW.snapshot,'$.entryId'),'')!=NEW.entry_id
 OR coalesce(json_extract(NEW.snapshot,'$.fromVersion'),0)!=NEW.from_version
 OR coalesce(json_extract(NEW.snapshot,'$.sampleSize'),0)!=NEW.sample_size
 OR coalesce(json_extract(NEW.snapshot,'$.status'),'')!=NEW.status
 OR coalesce(json_extract(NEW.snapshot,'$.decision'),'')!=''
 OR coalesce(json_extract(NEW.snapshot,'$.proposedHash'),'')!=NEW.proposed_hash
BEGIN SELECT RAISE(ABORT,'Evolution recommendation requires an existing version and an exact pending snapshot'); END;
CREATE TRIGGER adaptive_recommendation_decided BEFORE UPDATE ON adaptive_recommendations
WHEN NOT (OLD.status='pending' AND NEW.status IN ('dismissed','adopted')
 AND OLD.decision IS NULL AND NEW.decision IS NOT NULL
 AND OLD.decided_at IS NULL AND NEW.decided_at IS NOT NULL
 AND OLD.id=NEW.id AND OLD.project_id=NEW.project_id AND OLD.kind=NEW.kind
 AND OLD.entry_id=NEW.entry_id AND OLD.from_version=NEW.from_version
 AND OLD.observation=NEW.observation AND OLD.sample_size=NEW.sample_size
 AND OLD.proposed=NEW.proposed AND OLD.proposed_hash=NEW.proposed_hash
 AND OLD.snapshot=NEW.snapshot AND OLD.content_hash=NEW.content_hash
 AND OLD.created_at=NEW.created_at
 AND json_extract(NEW.decision,'$.disposition')=NEW.status)
BEGIN SELECT RAISE(ABORT,'Evolution recommendation is sealed at creation and may be decided once'); END;
CREATE TRIGGER adaptive_recommendation_retained BEFORE DELETE ON adaptive_recommendations
BEGIN SELECT RAISE(ABORT,'Evolution recommendation history is retained'); END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TEMP TABLE evolution_history_guard(retained INTEGER CHECK(retained=0));
INSERT INTO evolution_history_guard SELECT count(*) FROM adaptive_experiments;
INSERT INTO evolution_history_guard SELECT count(*) FROM adaptive_recommendations;
DROP TABLE evolution_history_guard;
DROP TRIGGER adaptive_experiment_retained;
DROP TRIGGER adaptive_experiment_sealed;
DROP TRIGGER adaptive_experiment_scope;
DROP TABLE adaptive_experiments;
DROP TRIGGER adaptive_recommendation_retained;
DROP TRIGGER adaptive_recommendation_decided;
DROP TRIGGER adaptive_recommendation_scope;
DROP TABLE adaptive_recommendations;
-- +goose StatementEnd
