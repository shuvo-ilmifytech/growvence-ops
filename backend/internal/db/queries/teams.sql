-- name: ListTeams :many
SELECT t.id, t.name, t.lead_id, l.name AS lead_name, t.department_id, d.name AS department_name,
  t.is_active, t.created_at,
  (SELECT count(*) FROM team_members m JOIN users mu ON mu.id = m.user_id AND mu.is_active
   WHERE m.team_id = t.id) AS member_count,
  (SELECT coalesce(array_agg(pt.name::text ORDER BY pt.name), '{}') FROM project_types pt
   WHERE pt.team_id = t.id AND pt.is_active)::text[] AS project_types
FROM teams t
LEFT JOIN users l ON l.id = t.lead_id
LEFT JOIN departments d ON d.id = t.department_id
WHERE (NOT sqlc.arg('active_only')::boolean OR t.is_active)
ORDER BY t.name;

-- name: GetTeam :one
SELECT * FROM teams WHERE id = $1;

-- name: CreateTeam :one
INSERT INTO teams (name, lead_id, department_id) VALUES ($1, $2, $3) RETURNING *;

-- name: UpdateTeam :one
UPDATE teams SET
    name      = coalesce(sqlc.narg('name'), name),
    is_active = coalesce(sqlc.narg('is_active'), is_active),
    lead_id   = CASE WHEN sqlc.arg('set_lead')::boolean THEN sqlc.narg('lead_id') ELSE lead_id END,
    department_id = CASE WHEN sqlc.arg('set_department')::boolean THEN sqlc.narg('department_id') ELSE department_id END
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ListTeamMembers :many
SELECT u.id, u.name, u.role, u.is_active, m.team_id, m.added_at
FROM team_members m JOIN users u ON u.id = m.user_id
WHERE m.team_id = $1
ORDER BY u.is_active DESC, u.name;

-- name: UserTeamID :one
-- The team someone is on; a member is in one team at a time. No row when they are in none.
SELECT team_id FROM team_members WHERE user_id = $1;

-- name: ListAllTeamMembers :many
SELECT m.user_id, m.team_id FROM team_members m;

-- name: SetTeamMember :exec
-- Joins (or moves to) a team; a member is in one team at a time.
INSERT INTO team_members (user_id, team_id) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET team_id = excluded.team_id, added_at = now();

-- name: RemoveTeamMember :execrows
DELETE FROM team_members WHERE team_id = $1 AND user_id = $2;

-- name: CountOpenTeamProjects :one
SELECT count(*) FROM projects
WHERE team_id = $1 AND status NOT IN ('draft', 'completed', 'cancelled');


-- name: ListTeamRoster :many
-- Everyone a team's work can be given to: its active members, its lead, and the head of
-- its department (leaders do delivery work themselves, 8.4.5). A team whose members were
-- never added still has its lead here, so the work can always be handed out.
SELECT u.id, u.name, u.role, u.is_active
FROM users u
WHERE u.is_active AND u.role IN ('member', 'tl') AND (
      u.id IN (SELECT m.user_id FROM team_members m WHERE m.team_id = $1)
   OR u.id IN (SELECT t.lead_id FROM teams t WHERE t.id = $1 AND t.lead_id IS NOT NULL)
   OR u.id IN (SELECT d.head_id FROM departments d JOIN teams t ON t.department_id = d.id
               WHERE t.id = $1 AND d.is_active AND d.head_id IS NOT NULL))
ORDER BY u.name;
