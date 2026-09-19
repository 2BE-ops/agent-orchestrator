-- +goose Up
ALTER TABLE review_run ADD COLUMN task_scope TEXT NOT NULL DEFAULT '' CHECK(task_scope='' OR length(task_scope)=64);
DROP INDEX idx_review_run_session_pr_sha_harness;
CREATE UNIQUE INDEX idx_review_run_session_pr_sha_harness
    ON review_run(session_id,pr_url,target_sha,harness,task_scope)
    WHERE target_sha!='' AND status NOT IN ('failed','cancelled')
    AND (status='running' OR verdict NOT IN ('','changes_requested'));

CREATE TABLE adaptive_task_review_contexts (
    run_id TEXT PRIMARY KEY NOT NULL REFERENCES review_run(id),
    result_id TEXT NOT NULL REFERENCES adaptive_task_results(id),
    scope_hash TEXT NOT NULL CHECK(length(scope_hash)=64),
    snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=2097152),
    content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
    launch_id TEXT NOT NULL CHECK(length(launch_id) BETWEEN 1 AND 200),
    started_at DATETIME,
    created_at DATETIME NOT NULL
);
CREATE INDEX adaptive_task_review_contexts_result ON adaptive_task_review_contexts(result_id,created_at,run_id);
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_review_context_immutable BEFORE UPDATE ON adaptive_task_review_contexts
WHEN OLD.run_id!=NEW.run_id OR OLD.result_id!=NEW.result_id OR OLD.scope_hash!=NEW.scope_hash
 OR OLD.snapshot!=NEW.snapshot OR OLD.content_hash!=NEW.content_hash OR OLD.launch_id!=NEW.launch_id
 OR OLD.created_at!=NEW.created_at OR OLD.started_at IS NOT NULL OR NEW.started_at IS NULL
BEGIN SELECT RAISE(ABORT,'task review context is immutable; launch may be observed once'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_task_review_context_no_delete BEFORE DELETE ON adaptive_task_review_contexts
WHEN EXISTS(SELECT 1 FROM review_run WHERE id=OLD.run_id)
BEGIN SELECT RAISE(ABORT,'context belongs to retained review run'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_review_run_scope_immutable BEFORE UPDATE OF task_scope ON review_run
WHEN OLD.task_scope!=NEW.task_scope
BEGIN SELECT RAISE(ABORT,'task review scope is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER adaptive_review_run_identity_immutable BEFORE UPDATE ON review_run
WHEN OLD.task_scope!='' AND (OLD.id!=NEW.id OR OLD.review_id!=NEW.review_id OR OLD.session_id!=NEW.session_id
 OR OLD.pr_url!=NEW.pr_url OR OLD.target_sha!=NEW.target_sha OR OLD.harness!=NEW.harness)
BEGIN SELECT RAISE(ABORT,'task review identity is immutable'); END;
-- +goose StatementEnd

-- +goose Down
-- If retained passes collide in the legacy scope, index creation fails and
-- goose rolls back this transaction instead of silently deleting review history.
DROP TRIGGER adaptive_review_run_scope_immutable;
DROP TRIGGER adaptive_review_run_identity_immutable;
DROP TRIGGER adaptive_task_review_context_no_delete;
DROP TABLE adaptive_task_review_contexts;
DROP INDEX idx_review_run_session_pr_sha_harness;
ALTER TABLE review_run DROP COLUMN task_scope;
CREATE UNIQUE INDEX idx_review_run_session_pr_sha_harness
    ON review_run(session_id,pr_url,target_sha,harness)
    WHERE target_sha!='' AND status NOT IN ('failed','cancelled')
    AND (status='running' OR verdict NOT IN ('','changes_requested'));
