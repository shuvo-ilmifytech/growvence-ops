-- name: InsertActivity :exec
INSERT INTO activity_log (actor_id, action, entity_type, entity_id, project_id, client_request_id, metadata)
VALUES (sqlc.narg('actor_id'), sqlc.arg('action'), sqlc.arg('entity_type'), sqlc.narg('entity_id'),
        sqlc.narg('project_id'), sqlc.narg('client_request_id'), sqlc.arg('metadata'));
