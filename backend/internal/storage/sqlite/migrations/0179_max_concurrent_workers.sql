-- +goose Up
-- Deterministic scheduler admission (stage 16): a durable daemon-wide cap on
-- simultaneously running worker sessions. Admission counts run inside the same
-- write transaction as session creation, so concurrent spawns cannot
-- oversubscribe, and the count derives from durable session state, so a daemon
-- restart cannot strand or leak capacity. Orchestrator and agent_manager
-- sessions are not workers and never consume worker capacity. Per-Agent-Type
-- parallelism is enforced from each launch's pinned WorkerConfiguration and
-- needs no schema.

ALTER TABLE app_settings ADD COLUMN max_concurrent_workers INTEGER NOT NULL DEFAULT 100
    CHECK (max_concurrent_workers BETWEEN 1 AND 1000);

-- +goose Down
ALTER TABLE app_settings DROP COLUMN max_concurrent_workers;
