-- name: Search :many
-- One ranked list across projects, client requests, cards, people and files, scoped like
-- each list is.
-- Scope args mirror ListProjectDetails; people are visible to everyone, files follow
-- their project, and cards follow the board rules (Sales: client requests only).
WITH q AS (SELECT '%' || sqlc.arg('q')::text || '%' AS pat),
visible AS (
  SELECT v.* FROM project_details v
  WHERE (sqlc.narg('scope_creator')::uuid IS NULL OR v.created_by = sqlc.narg('scope_creator'))
    AND (sqlc.narg('scope_sales')::uuid IS NULL OR v.created_by = sqlc.narg('scope_sales') OR v.status <> 'draft')
    AND (sqlc.narg('scope_assignee')::uuid IS NULL OR sqlc.narg('scope_assignee')::uuid = ANY (v.member_ids))
    AND (NOT sqlc.arg('hide_drafts')::boolean OR v.status <> 'draft')
    AND (sqlc.narg('scope_lead')::uuid IS NULL OR v.team_id IS NULL OR v.team_lead_id IS NULL
         OR v.team_lead_id = sqlc.narg('scope_lead') OR v.team_head_id = sqlc.narg('scope_lead'))
),
-- Client requests follow their own list's rules: Sales see every one, a Team Leader their
-- teams', a member every request with the team they are on (and any they still hold a card
-- on), the CEO everything.
visible_requests AS (
  SELECT r.* FROM client_request_details r
  WHERE (sqlc.narg('scope_creator')::uuid IS NULL OR r.created_by = sqlc.narg('scope_creator'))
    AND (sqlc.narg('scope_assignee')::uuid IS NULL
         OR sqlc.narg('scope_assignee')::uuid = ANY (r.member_ids)
         OR EXISTS (SELECT 1 FROM team_members tm
                    WHERE tm.team_id = r.team_id AND tm.user_id = sqlc.narg('scope_assignee')))
    AND (sqlc.narg('scope_lead')::uuid IS NULL OR r.team_lead_id IS NULL
         OR r.team_lead_id = sqlc.narg('scope_lead') OR r.team_head_id = sqlc.narg('scope_lead'))
)
SELECT 'project' AS kind, v.id, v.project_id::text AS project_id, v.title, v.subtitle, v.badge, v.rank, v.at FROM (
  SELECT p.id, p.id AS project_id, coalesce(p.name, 'Untitled draft') AS title,
         concat_ws(' · ', p.client_contact, p.client_name, p.order_id) AS subtitle,
         p.status AS badge, p.last_activity_at AS at,
         CASE WHEN p.order_id ILIKE (SELECT pat FROM q) THEN 0
              WHEN p.client_contact ILIKE (SELECT pat FROM q) THEN 1 ELSE 2 END AS rank
  FROM visible p
  WHERE p.name ILIKE (SELECT pat FROM q) OR p.client_name ILIKE (SELECT pat FROM q)
     OR p.order_id ILIKE (SELECT pat FROM q) OR p.client_contact ILIKE (SELECT pat FROM q)
     OR p.number::text = sqlc.arg('q')
) v
UNION ALL
SELECT 'card', c.id, c.project_id::text, c.title, concat_ws(' · ', v.name, v.client_contact), c.column_key, 3, c.updated_at
FROM card_details c JOIN visible v ON v.id = c.project_id
WHERE (sqlc.narg('scope_sales')::uuid IS NULL OR c.source = 'client')
  AND c.title ILIKE (SELECT pat FROM q)
UNION ALL
-- A client request, and the cards on it. Its id goes in the request_id column so the UI
-- links to the request page instead of a project.
SELECT 'request', r.id, '', r.title, concat_ws(' · ', r.client_name, r.order_id, r.team_name), r.status, 2, r.last_activity_at
FROM visible_requests r
WHERE r.title ILIKE (SELECT pat FROM q) OR r.client_name ILIKE (SELECT pat FROM q)
   OR r.order_id ILIKE (SELECT pat FROM q) OR r.number::text = sqlc.arg('q')
UNION ALL
SELECT 'request_card', c.id, c.client_request_id::text, c.title, concat_ws(' · ', r.client_name, r.title), c.column_key, 3, c.updated_at
FROM card_details c JOIN visible_requests r ON r.id = c.client_request_id
WHERE (sqlc.narg('scope_sales')::uuid IS NULL OR c.source = 'client')
  AND c.title ILIKE (SELECT pat FROM q)
UNION ALL
SELECT 'file', f.id, f.project_id::text, f.original_name, concat_ws(' · ', v.name, v.client_contact),
       coalesce(fv.id::text, ''), 4, f.created_at
FROM files f
JOIN visible v ON v.id = f.project_id
LEFT JOIN file_versions fv ON fv.id = f.current_version_id
WHERE f.removed_at IS NULL AND f.original_name ILIKE (SELECT pat FROM q)
  -- Sales never reach attachments on the Team Leader's internal tasks.
  AND (sqlc.narg('scope_sales')::uuid IS NULL OR f.card_id IS NULL
       OR EXISTS (SELECT 1 FROM project_cards pc WHERE pc.id = f.card_id AND pc.source = 'client'))
UNION ALL
SELECT 'person', u.id, '', u.name, concat_ws(' · ', u.email, e.designation), u.role, 5, u.created_at
FROM users u
LEFT JOIN employees e ON e.user_id = u.id
WHERE u.is_active AND sqlc.arg('people')::boolean
  AND (u.name ILIKE (SELECT pat FROM q) OR u.email ILIKE (SELECT pat FROM q))
ORDER BY rank, at DESC
LIMIT sqlc.arg('lim');
