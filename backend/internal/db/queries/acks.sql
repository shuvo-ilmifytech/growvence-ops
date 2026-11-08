-- name: CreateAcknowledgement :exec
INSERT INTO acknowledgements (project_change_id, user_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListProjectAcknowledgements :many
SELECT a.id, a.project_change_id, a.user_id, u.name AS user_name, a.acknowledged_at, a.created_at
FROM acknowledgements a
JOIN project_changes c ON c.id = a.project_change_id
JOIN users u ON u.id = a.user_id
WHERE c.project_id = $1
ORDER BY a.created_at;

-- name: AcknowledgeAll :many
-- Acknowledge every pending important change on a project for one user.
UPDATE acknowledgements a SET acknowledged_at = now()
FROM project_changes c
WHERE c.id = a.project_change_id AND c.project_id = $1 AND a.user_id = $2 AND a.acknowledged_at IS NULL
RETURNING a.id;

-- name: GetLastViewed :one
SELECT last_viewed_at FROM project_views WHERE user_id = $1 AND project_id = $2;

-- name: MarkProjectViewed :exec
INSERT INTO project_views (user_id, project_id, last_viewed_at) VALUES ($1, $2, now())
ON CONFLICT (user_id, project_id) DO UPDATE SET last_viewed_at = now();
