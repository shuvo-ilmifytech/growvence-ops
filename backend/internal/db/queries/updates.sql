-- name: GetUpdateDetail :one
SELECT * FROM update_details WHERE id = $1;

-- name: ListUpdateDetails :many
-- Scope params restrict by role; the rest are UI filters.
SELECT sqlc.embed(v), count(*) OVER () AS total
FROM update_details v
-- Sales (any of them) see approved and sent updates on every project.
WHERE (sqlc.narg('scope_sales')::uuid IS NULL OR v.status IN ('approved', 'sent'))
  AND (sqlc.narg('scope_assignee')::uuid IS NULL
       OR sqlc.narg('scope_assignee')::uuid = ANY (v.project_member_ids)
       -- A client request is the whole team's work: everyone on the team it is with reads
       -- its updates, while a draft stays with whoever is still writing it.
       OR (v.client_request_id IS NOT NULL AND v.status <> 'draft' AND EXISTS (
             SELECT 1 FROM team_members tm
             WHERE tm.team_id = v.project_team_id AND tm.user_id = sqlc.narg('scope_assignee'))))
  -- A leader working the project sees its drafts like any assignee.
  AND (NOT sqlc.arg('hide_drafts')::boolean OR v.status <> 'draft' OR sqlc.narg('scope_lead')::uuid = ANY (v.project_member_ids))
  AND (sqlc.narg('scope_lead')::uuid IS NULL OR v.project_team_id IS NULL OR v.project_team_lead_id IS NULL
       OR v.project_team_lead_id = sqlc.narg('scope_lead') OR v.project_team_head_id = sqlc.narg('scope_lead'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR v.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('client_request_id')::uuid IS NULL OR v.client_request_id = sqlc.narg('client_request_id'))
  AND (sqlc.narg('statuses')::text[] IS NULL OR v.status = ANY (sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('destination')::text IS NULL OR v.destination = sqlc.narg('destination'))
  AND (sqlc.narg('created_from')::timestamptz IS NULL OR v.created_at >= sqlc.narg('created_from'))
  AND (sqlc.narg('created_to')::timestamptz IS NULL OR v.created_at <= sqlc.narg('created_to'))
  AND (sqlc.narg('q')::text IS NULL
       OR v.message ILIKE '%' || sqlc.narg('q') || '%'
       OR v.order_id ILIKE '%' || sqlc.narg('q') || '%'
       OR v.client_name ILIKE '%' || sqlc.narg('q') || '%'
       OR v.number::text = sqlc.narg('q'))
ORDER BY v.created_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountUpdatesByStatus :many
SELECT v.status, count(*) AS n
FROM update_details v
-- Sales (any of them) see approved and sent updates on every project.
WHERE (sqlc.narg('scope_sales')::uuid IS NULL OR v.status IN ('approved', 'sent'))
  AND (sqlc.narg('scope_assignee')::uuid IS NULL
       OR sqlc.narg('scope_assignee')::uuid = ANY (v.project_member_ids)
       -- A client request is the whole team's work: everyone on the team it is with reads
       -- its updates, while a draft stays with whoever is still writing it.
       OR (v.client_request_id IS NOT NULL AND v.status <> 'draft' AND EXISTS (
             SELECT 1 FROM team_members tm
             WHERE tm.team_id = v.project_team_id AND tm.user_id = sqlc.narg('scope_assignee'))))
  -- A leader working the project sees its drafts like any assignee.
  AND (NOT sqlc.arg('hide_drafts')::boolean OR v.status <> 'draft' OR sqlc.narg('scope_lead')::uuid = ANY (v.project_member_ids))
  AND (sqlc.narg('scope_lead')::uuid IS NULL OR v.project_team_id IS NULL OR v.project_team_lead_id IS NULL
       OR v.project_team_lead_id = sqlc.narg('scope_lead') OR v.project_team_head_id = sqlc.narg('scope_lead'))
GROUP BY v.status;

-- name: CreateUpdate :one
-- Exactly one of project_id / client_request_id is set: the update is on a project, or
-- on a client request for a client with no project.
INSERT INTO updates (project_id, client_request_id, author_id, message, destination)
VALUES (sqlc.narg('project_id'), sqlc.narg('client_request_id'), sqlc.arg('author_id'),
        sqlc.arg('message'), sqlc.arg('destination'))
RETURNING id;

-- name: LockUpdate :one
SELECT * FROM updates WHERE id = $1 FOR UPDATE;

-- name: SetUpdateContent :exec
UPDATE updates SET message = $2, destination = $3,
    lock_version = lock_version + 1, updated_at = now()
WHERE id = $1;

-- name: SetUpdateState :exec
-- One write for every status move; the service decides each field.
UPDATE updates SET
    status = sqlc.arg('status'),
    destination = sqlc.arg('destination'),
    submitted_at = sqlc.narg('submitted_at'),
    revision_number = sqlc.narg('revision_number'),
    checked_by = sqlc.narg('checked_by'),
    checked_at = sqlc.narg('checked_at'),
    auto_approved = sqlc.arg('auto_approved'),
    sent_by = sqlc.narg('sent_by'),
    sent_at = sqlc.narg('sent_at'),
    unsent_reminder_at = NULL, -- every status move re-arms the 24h reminder
    lock_version = lock_version + 1,
    updated_at = now()
WHERE id = sqlc.arg('id');

-- name: InsertUpdateStatusHistory :exec
INSERT INTO update_status_history (update_id, from_status, to_status, changed_by, comment)
VALUES ($1, $2, $3, $4, $5);

-- name: ListUpdateStatusHistory :many
SELECT h.id, h.from_status, h.to_status, h.comment, h.created_at, h.changed_by, u.name AS changed_by_name
FROM update_status_history h JOIN users u ON u.id = h.changed_by
WHERE h.update_id = $1
ORDER BY h.created_at, h.id;

-- name: InsertUpdateComment :one
INSERT INTO update_comments (update_id, thread, author_id, body) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListUpdateComments :many
SELECT c.id, c.thread, c.body, c.created_at, c.author_id, u.name AS author_name, u.role AS author_role
FROM update_comments c JOIN users u ON u.id = c.author_id
WHERE c.update_id = $1 AND c.thread = ANY (sqlc.arg('threads')::text[])
ORDER BY c.created_at, c.id;

-- name: ListUpdateFiles :many
SELECT f.id, f.original_name, f.created_at, f.uploaded_by, fu.name AS uploaded_by_name,
       v.id AS version_id, v.version_no, v.size_bytes, v.mime_type
FROM files f
JOIN file_versions v ON v.id = f.current_version_id
JOIN users fu ON fu.id = f.uploaded_by
WHERE f.update_id = $1 AND f.removed_at IS NULL
ORDER BY f.created_at;

-- name: CountUpdateFiles :one
SELECT count(*) FROM files WHERE update_id = $1 AND removed_at IS NULL;
