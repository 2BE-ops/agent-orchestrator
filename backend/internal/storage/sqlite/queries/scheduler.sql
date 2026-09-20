-- Deterministic scheduler admission (stage 16). Both counts run inside the
-- session-creation write transaction under the store's single-writer lock, so
-- a concurrent spawn cannot slip between the count and its insert. Counts
-- derive from durable session state; terminating a worker or restarting the
-- daemon changes them with no in-memory bookkeeping to drift.

-- name: CountActiveWorkerSessions :one
SELECT count(*) FROM sessions WHERE kind = 'worker' AND is_terminated = 0;

-- name: CountActiveWorkerSessionsByType :one
SELECT count(*) FROM adaptive_worker_configurations c
JOIN sessions s ON s.id = c.session_id
WHERE c.agent_type_id = ? AND s.kind = 'worker' AND s.is_terminated = 0;
