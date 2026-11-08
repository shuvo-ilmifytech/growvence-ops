-- Client requests: work for a client with no project in the portal (monthly management,
-- or an order delivered before the portal existed). Sales raise one and send it to a team;
-- the team works its board cards and writes its client updates.

-- name: GetClientRequestDetail :one
SELECT * FROM client_request_details WHERE id = $1;

-- name: LockClientRequest :one
SELECT * FROM client_requests WHERE id = $1 FOR UPDATE;

-- name: CreateClientRequest :one
INSERT INTO client_requests (profile_id, client_name, order_id, title, details, team_id, created_by)
VALUES (
    sqlc.arg('profile_id'), sqlc.arg('client_name'), sqlc.narg('order_id'),
    sqlc.arg('title'), sqlc.arg('details'), sqlc.arg('team_id'), sqlc.arg('created_by'))
RETURNING *;

-- name: UpdateClientRequest :exec
UPDATE client_requests SET
    profile_id  = sqlc.arg('profile_id'),
    client_name = sqlc.arg('client_name'),
    order_id    = sqlc.narg('order_id'),
    title       = sqlc.arg('title'),
    details     = sqlc.arg('details'),
    updated_at  = now()
WHERE id = sqlc.arg('id');

-- name: SetClientRequestTeam :exec
UPDATE client_requests SET team_id = $2, updated_at = now() WHERE id = $1;

-- name: SetClientRequestStatus :exec
UPDATE client_requests SET
    status     = sqlc.arg('status'),
    closed_by  = sqlc.narg('closed_by'),
    closed_at  = sqlc.narg('closed_at'),
    updated_at = now()
WHERE id = sqlc.arg('id');

-- name: TouchClientRequest :exec
UPDATE client_requests SET last_activity_at = now() WHERE id = $1;

-- name: ListClientRequests :many
-- Scope args mirror the project list: Sales (any of them) see every request, a Team Leader
-- their teams' (and any whose team has no active lead), a member every request with the
-- team they are on — the whole team reads the work, whether or not the leader has handed
-- it to them yet — plus any they still hold a card on, the CEO everything. The rest are
-- UI filters.
SELECT sqlc.embed(v), count(*) OVER () AS total
FROM client_request_details v
WHERE (sqlc.narg('scope_member')::uuid IS NULL
       OR sqlc.narg('scope_member')::uuid = ANY (v.member_ids)
       OR EXISTS (SELECT 1 FROM team_members tm
                  WHERE tm.team_id = v.team_id AND tm.user_id = sqlc.narg('scope_member')))
  AND (sqlc.narg('scope_lead')::uuid IS NULL OR v.team_lead_id IS NULL
       OR v.team_lead_id = sqlc.narg('scope_lead') OR v.team_head_id = sqlc.narg('scope_lead'))
  AND (sqlc.narg('statuses')::text[] IS NULL OR v.status = ANY (sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('team')::uuid IS NULL OR v.team_id = sqlc.narg('team'))
  AND (sqlc.narg('author')::uuid IS NULL OR v.created_by = sqlc.narg('author'))
  AND (sqlc.narg('q')::text IS NULL
       OR v.title ILIKE '%' || sqlc.narg('q') || '%'
       OR v.client_name ILIKE '%' || sqlc.narg('q') || '%'
       OR v.order_id ILIKE '%' || sqlc.narg('q') || '%'
       OR v.details ILIKE '%' || sqlc.narg('q') || '%'
       OR v.number::text = sqlc.narg('q'))
ORDER BY CASE v.status WHEN 'open' THEN 0 WHEN 'done' THEN 1 ELSE 2 END, v.last_activity_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountClientRequestsByStatus :many
SELECT v.status, count(*) AS n
FROM client_request_details v
WHERE (sqlc.narg('scope_member')::uuid IS NULL
       OR sqlc.narg('scope_member')::uuid = ANY (v.member_ids)
       OR EXISTS (SELECT 1 FROM team_members tm
                  WHERE tm.team_id = v.team_id AND tm.user_id = sqlc.narg('scope_member')))
  AND (sqlc.narg('scope_lead')::uuid IS NULL OR v.team_lead_id IS NULL
       OR v.team_lead_id = sqlc.narg('scope_lead') OR v.team_head_id = sqlc.narg('scope_lead'))
  AND (sqlc.narg('author')::uuid IS NULL OR v.created_by = sqlc.narg('author'))
GROUP BY v.status;

-- name: ClearRequestCardAssignees :exec
-- A request moved to another team: nobody on the old team keeps its cards.
DELETE FROM card_assignees ca USING project_cards c
WHERE c.id = ca.card_id AND c.client_request_id = $1;
