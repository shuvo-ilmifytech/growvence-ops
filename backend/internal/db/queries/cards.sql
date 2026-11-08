-- name: ListCards :many
-- A container's live cards in board order (a project's, or a client request's).
-- source limits Sales to client requests.
SELECT * FROM card_details c
WHERE c.container_id = sqlc.arg('container_id')
  AND (sqlc.narg('source')::text IS NULL OR c.source = sqlc.narg('source'))
ORDER BY CASE c.column_key WHEN 'todo' THEN 0 WHEN 'in_progress' THEN 1 WHEN 'review' THEN 2 ELSE 3 END, c.position, c.number;

-- name: GetCardDetail :one
SELECT * FROM card_details WHERE id = $1;

-- name: LockCard :one
SELECT * FROM project_cards WHERE id = $1 AND removed_at IS NULL FOR UPDATE;

-- name: CreateCard :one
-- Exactly one of project_id / client_request_id is set; container_id is the same id,
-- which is what the number and the position count within.
INSERT INTO project_cards (project_id, client_request_id, number, title, description, source, position, due_date, created_by)
VALUES (
    sqlc.narg('project_id'), sqlc.narg('client_request_id'),
    (SELECT coalesce(max(pc.number), 0) + 1 FROM project_cards pc WHERE pc.container_id = sqlc.arg('container_id')),
    sqlc.arg('title'), sqlc.arg('description'), sqlc.arg('source'),
    (SELECT coalesce(max(pc.position), 0) + 1 FROM project_cards pc
     WHERE pc.container_id = sqlc.arg('container_id') AND pc.column_key = 'todo' AND pc.removed_at IS NULL),
    sqlc.narg('due_date')::text::date, sqlc.arg('created_by'))
RETURNING *;

-- name: UpdateCard :exec
UPDATE project_cards SET
    title = sqlc.arg('title'), description = sqlc.arg('description'),
    due_date = sqlc.narg('due_date')::text::date, updated_at = now()
WHERE id = sqlc.arg('id');

-- name: ColumnCardIDs :many
SELECT id FROM project_cards
WHERE container_id = $1 AND column_key = $2 AND removed_at IS NULL
ORDER BY position, number;

-- name: PlaceCard :exec
UPDATE project_cards SET
    column_key = sqlc.arg('column_key'), position = sqlc.arg('position'),
    done_at = CASE WHEN sqlc.arg('column_key') = 'done' THEN coalesce(done_at, now()) ELSE NULL END,
    updated_at = CASE WHEN column_key <> sqlc.arg('column_key') THEN now() ELSE updated_at END
WHERE id = sqlc.arg('id');

-- name: RemoveCard :exec
UPDATE project_cards SET removed_at = now(), removed_by = $2 WHERE id = $1;

-- name: ClearCardAssignees :exec
DELETE FROM card_assignees WHERE card_id = $1;

-- name: AddCardAssignee :exec
INSERT INTO card_assignees (card_id, user_id) VALUES ($1, $2);

-- name: CardAssigneeIDs :many
SELECT user_id FROM card_assignees WHERE card_id = $1;

-- name: ListCardComments :many
SELECT c.id, c.body, c.created_at, c.author_id, u.name AS author_name, u.role AS author_role
FROM card_comments c JOIN users u ON u.id = c.author_id
WHERE c.card_id = $1
ORDER BY c.created_at, c.id;

-- name: CreateCardComment :one
INSERT INTO card_comments (card_id, author_id, body) VALUES ($1, $2, $3) RETURNING *;

-- name: ListAllCards :many
-- Cards across every project the viewer can see (same scope as the project list).
-- Filters come from the UI.
SELECT sqlc.embed(c), v.number AS project_number, v.name AS project_name, v.status AS project_status,
       v.created_by AS project_sales_id, v.member_ids AS project_member_ids, v.member_names AS project_member_names,
       v.team_name AS project_team_name
FROM card_details c
JOIN project_details v ON v.id = c.project_id
WHERE (sqlc.narg('scope_creator')::uuid IS NULL OR v.created_by = sqlc.narg('scope_creator'))
  -- Sales: client requests on every project except other people's drafts; never the TL's tasks.
  AND (sqlc.narg('scope_sales')::uuid IS NULL OR (c.source = 'client' AND (v.created_by = sqlc.narg('scope_sales') OR v.status <> 'draft')))
  AND (sqlc.narg('scope_assignee')::uuid IS NULL OR sqlc.narg('scope_assignee')::uuid = ANY (v.member_ids))
  AND (NOT sqlc.arg('hide_drafts')::boolean OR v.status <> 'draft')
  AND (sqlc.narg('scope_lead')::uuid IS NULL OR v.team_id IS NULL OR v.team_lead_id IS NULL
       OR v.team_lead_id = sqlc.narg('scope_lead') OR v.team_head_id = sqlc.narg('scope_lead'))
  AND (sqlc.narg('project')::uuid IS NULL OR v.id = sqlc.narg('project'))
  AND (sqlc.narg('team')::uuid IS NULL OR v.team_id = sqlc.narg('team'))
  AND (sqlc.narg('assignee')::uuid IS NULL OR sqlc.narg('assignee')::uuid = ANY (c.assignee_ids))
  -- Sales are never assignees, so "mine" means the requests they logged.
  AND (sqlc.narg('author')::uuid IS NULL OR c.created_by = sqlc.narg('author'))
  AND (sqlc.narg('source')::text IS NULL OR c.source = sqlc.narg('source'))
  AND (sqlc.narg('q')::text IS NULL OR c.title ILIKE '%' || sqlc.narg('q') || '%' OR v.name ILIKE '%' || sqlc.narg('q') || '%')
  AND v.status <> 'cancelled'
ORDER BY CASE c.column_key WHEN 'todo' THEN 0 WHEN 'in_progress' THEN 1 WHEN 'review' THEN 2 ELSE 3 END,
         c.due_date NULLS LAST, c.updated_at DESC
LIMIT 1000;

-- name: MarkCardViewed :exec
-- The first time someone opens a card, so "unopened" alerts can stop ringing.
INSERT INTO card_views (card_id, user_id) VALUES (sqlc.arg('card_id'), sqlc.arg('user_id'))
ON CONFLICT (card_id, user_id) DO NOTHING;

-- name: AlertCounts :one
-- What is waiting for this person right now, for the browser alert. Cards and updates
-- count the same whether they hang on a project or on a client request.
SELECT
  -- Sales are one team: any of them can send an approved update anywhere.
  (SELECT count(*) FROM updates u
     LEFT JOIN projects p ON p.id = u.project_id
     LEFT JOIN client_requests r ON r.id = u.client_request_id
    WHERE sqlc.arg('is_sales')::boolean AND u.status = 'approved'
      AND coalesce(p.status, r.status) <> 'cancelled') AS unsent_updates,
  -- Cards assigned to them that they have never opened.
  (SELECT count(*) FROM card_assignees ca
     JOIN project_cards c ON c.id = ca.card_id
     LEFT JOIN projects p ON p.id = c.project_id
     LEFT JOIN client_requests r ON r.id = c.client_request_id
     LEFT JOIN card_views v ON v.card_id = c.id AND v.user_id = ca.user_id
    WHERE ca.user_id = sqlc.arg('viewer') AND c.removed_at IS NULL AND c.column_key <> 'done'
      AND coalesce(p.status, r.status) NOT IN ('cancelled', 'completed', 'done')
      AND v.card_id IS NULL) AS unopened_cards;

-- name: ListAllRequestCards :many
-- The same board, for cards on client requests: Sales see every client request, a Team
-- Leader their teams', a member every request with the team they are on (and any they
-- still hold a card on), the CEO everything.
SELECT sqlc.embed(c), v.number AS request_number, v.title AS request_title, v.status AS request_status,
       v.created_by AS request_sales_id, v.member_ids AS request_member_ids,
       v.team_id AS request_team_id, v.team_name AS request_team_name,
       v.client_name AS request_client_name
FROM card_details c
JOIN client_request_details v ON v.id = c.client_request_id
WHERE (sqlc.narg('scope_sales')::uuid IS NULL OR c.source = 'client')
  AND (sqlc.narg('scope_assignee')::uuid IS NULL
       OR sqlc.narg('scope_assignee')::uuid = ANY (v.member_ids)
       OR EXISTS (SELECT 1 FROM team_members tm
                  WHERE tm.team_id = v.team_id AND tm.user_id = sqlc.narg('scope_assignee')))
  AND (sqlc.narg('scope_lead')::uuid IS NULL OR v.team_lead_id IS NULL
       OR v.team_lead_id = sqlc.narg('scope_lead') OR v.team_head_id = sqlc.narg('scope_lead'))
  AND (sqlc.narg('request')::uuid IS NULL OR v.id = sqlc.narg('request'))
  AND (sqlc.narg('team')::uuid IS NULL OR v.team_id = sqlc.narg('team'))
  AND (sqlc.narg('assignee')::uuid IS NULL OR sqlc.narg('assignee')::uuid = ANY (c.assignee_ids))
  -- Sales are never assignees, so "mine" means the requests they raised.
  AND (sqlc.narg('author')::uuid IS NULL OR c.created_by = sqlc.narg('author') OR v.created_by = sqlc.narg('author'))
  AND (sqlc.narg('source')::text IS NULL OR c.source = sqlc.narg('source'))
  AND (sqlc.narg('q')::text IS NULL OR c.title ILIKE '%' || sqlc.narg('q') || '%'
       OR v.title ILIKE '%' || sqlc.narg('q') || '%' OR v.client_name ILIKE '%' || sqlc.narg('q') || '%')
  AND v.status <> 'cancelled'
ORDER BY CASE c.column_key WHEN 'todo' THEN 0 WHEN 'in_progress' THEN 1 WHEN 'review' THEN 2 ELSE 3 END,
         c.due_date NULLS LAST, c.updated_at DESC
LIMIT 1000;
