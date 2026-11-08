-- name: ListProfiles :many
SELECT * FROM marketplace_profiles
WHERE (NOT sqlc.arg('active_only')::boolean OR is_active)
ORDER BY name;

-- name: GetProfile :one
SELECT * FROM marketplace_profiles WHERE id = $1;

-- name: CreateProfile :one
INSERT INTO marketplace_profiles (name, platform, fee_percent)
VALUES (sqlc.arg('name'), sqlc.arg('platform'), coalesce(sqlc.narg('fee_percent')::text::numeric, 20))
RETURNING *;

-- name: UpdateProfile :one
UPDATE marketplace_profiles SET
    name      = coalesce(sqlc.narg('name'), name),
    platform  = coalesce(sqlc.narg('platform'), platform),
    fee_percent = coalesce(sqlc.narg('fee_percent')::text::numeric, fee_percent),
    is_active = coalesce(sqlc.narg('is_active'), is_active)
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ListProjectTypes :many
SELECT * FROM project_types
WHERE (NOT sqlc.arg('active_only')::boolean OR is_active)
ORDER BY name;

-- name: CreateProjectType :one
INSERT INTO project_types (name, team_id) VALUES ($1, $2) RETURNING *;

-- name: UpdateProjectType :one
UPDATE project_types SET
    name      = coalesce(sqlc.narg('name'), name),
    is_active = coalesce(sqlc.narg('is_active'), is_active),
    team_id   = CASE WHEN sqlc.arg('set_team')::boolean THEN sqlc.narg('team_id') ELSE team_id END
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: RerouteWaitingProjects :execrows
-- A type moved to another team: projects still waiting for assignment follow it, unless
-- a Team Leader moved them by hand (their team isn't the one the type routed to before).
UPDATE projects p SET team_id = (SELECT t.id FROM teams t WHERE t.id = pt.team_id AND t.is_active)
FROM project_types pt
WHERE pt.id = p.project_type_id AND pt.id = sqlc.arg('id')
  AND p.status = 'new' AND p.assigned_to IS NULL
  AND p.team_id IS NOT DISTINCT FROM sqlc.narg('old_team')::uuid;

-- name: ListSettings :many
SELECT * FROM settings ORDER BY key;

-- name: GetSetting :one
SELECT * FROM settings WHERE key = $1;

-- name: UpdateSetting :one
UPDATE settings SET value = $2, updated_by = $3, updated_at = now()
WHERE key = $1
RETURNING *;
