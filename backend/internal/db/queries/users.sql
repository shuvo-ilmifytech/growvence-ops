-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: ListUsers :many
SELECT * FROM users
WHERE (sqlc.narg('role')::text IS NULL OR role = sqlc.narg('role'))
  AND (sqlc.narg('is_active')::boolean IS NULL OR is_active = sqlc.narg('is_active'))
ORDER BY is_active DESC, name;

-- name: CreateUser :one
INSERT INTO users (email, name, password_hash, role, is_admin, can_view_analytics)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateUser :one
UPDATE users SET
    name      = coalesce(sqlc.narg('name'), name),
    email     = coalesce(sqlc.narg('email'), email),
    role      = coalesce(sqlc.narg('role'), role),
    is_admin  = coalesce(sqlc.narg('is_admin'), is_admin),
    can_view_analytics = coalesce(sqlc.narg('can_view_analytics'), can_view_analytics),
    is_active = coalesce(sqlc.narg('is_active'), is_active),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;

-- name: TouchUserLogin :exec
UPDATE users SET last_login_at = now() WHERE id = $1;

-- name: CountActiveAdmins :one
SELECT count(*) FROM users WHERE is_admin AND is_active;

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: OpenWorkOf :one
-- What someone still holds and must hand over before losing access or changing role.
SELECT
  (SELECT count(*) FROM projects p JOIN project_members m ON m.project_id = p.id AND m.user_id = sqlc.arg('user_id')::uuid
     WHERE p.status IN ('in_progress', 'blocked', 'ready_for_review', 'ready_to_deliver')) AS assigned_projects,
  (SELECT count(*) FROM teams t WHERE t.lead_id = sqlc.arg('user_id')::uuid AND t.is_active) AS teams_led,
  (SELECT count(*) FROM departments d WHERE d.head_id = sqlc.arg('user_id')::uuid AND d.is_active) AS departments_headed,
  (SELECT count(*) FROM employees e JOIN users u ON u.id = e.user_id AND u.is_active
     WHERE e.manager_id = sqlc.arg('user_id')::uuid) AS direct_reports;
