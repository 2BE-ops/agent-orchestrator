-- name: InsertEvolutionExperiment :exec
INSERT INTO adaptive_experiments (id, project_id, kind, entry_id, control_version, candidate_version,
 hypothesis, minimum_samples, status, snapshot, content_hash, created_at, concluded_at, conclusion)
VALUES (?,?,?,?,?,?,?,?,?,?,?,sqlc.arg(created_at),NULL,NULL);

-- name: GetEvolutionExperiment :one
SELECT * FROM adaptive_experiments
WHERE project_id=sqlc.arg(project_id) AND id=sqlc.arg(id);

-- name: ListEvolutionExperimentIDs :many
SELECT id FROM adaptive_experiments
WHERE project_id=sqlc.arg(project_id) AND id>sqlc.arg(after_id)
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: ConcludeEvolutionExperiment :execrows
UPDATE adaptive_experiments SET status='concluded', concluded_at=sqlc.arg(concluded_at), conclusion=sqlc.arg(conclusion)
WHERE project_id=sqlc.arg(project_id) AND id=sqlc.arg(id) AND status='running';

-- name: InsertEvolutionRecommendation :exec
INSERT INTO adaptive_recommendations (id, project_id, kind, entry_id, from_version, observation,
 sample_size, proposed, proposed_hash, status, snapshot, content_hash, created_at, decided_at, decision)
VALUES (?,?,?,?,?,?,?,?,?, 'pending', ?, ?, sqlc.arg(created_at), NULL, NULL);

-- name: GetEvolutionRecommendation :one
SELECT * FROM adaptive_recommendations
WHERE project_id=sqlc.arg(project_id) AND id=sqlc.arg(id);

-- name: ListEvolutionRecommendationIDs :many
SELECT id FROM adaptive_recommendations
WHERE project_id=sqlc.arg(project_id) AND id>sqlc.arg(after_id)
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: DecideEvolutionRecommendation :execrows
UPDATE adaptive_recommendations SET status=sqlc.arg(status), decided_at=sqlc.arg(decided_at), decision=sqlc.arg(decision)
WHERE project_id=sqlc.arg(project_id) AND id=sqlc.arg(id) AND status='pending';
