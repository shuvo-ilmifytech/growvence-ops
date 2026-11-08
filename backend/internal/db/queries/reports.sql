-- name: ReportProjectsByStatus :many
SELECT status, count(*) AS n FROM projects GROUP BY status;

-- name: ReportProjectsByMember :many
SELECT u.id, u.name,
  count(p.id) FILTER (WHERE p.status IN ('in_progress','blocked','ready_for_review','ready_to_deliver')) AS active,
  count(p.id) FILTER (WHERE p.status = 'blocked') AS blocked,
  count(p.id) FILTER (WHERE p.status IN ('in_progress','blocked','ready_for_review','ready_to_deliver')
                        AND coalesce(end_of_day(p.due_date), p.deadline) < now()) AS overdue,
  count(p.id) FILTER (WHERE p.status = 'completed') AS completed
FROM users u
LEFT JOIN projects p ON p.assigned_to = u.id
WHERE u.role IN ('member', 'tl') AND u.is_active
GROUP BY u.id, u.name
ORDER BY active DESC, u.name;

-- name: ReportOverdue :many
SELECT p.id, p.number, p.name, p.status, iso(coalesce(end_of_day(p.due_date), p.deadline)) AS due,
       -- Whole days late, so "due an hour ago" reads as 0 rather than 1.
       floor(extract(epoch FROM now() - coalesce(end_of_day(p.due_date), p.deadline)) / 86400)::int AS days_overdue,
       au.name AS assignee_name
FROM projects p LEFT JOIN users au ON au.id = p.assigned_to
WHERE p.status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')
  AND coalesce(end_of_day(p.due_date), p.deadline) < now()
ORDER BY days_overdue DESC
LIMIT 50;

-- name: ReportUnassigned :many
SELECT p.id, p.number, p.name, p.priority, iso(p.deadline) AS deadline, su.name AS sales_name,
       p.first_submitted_at AS waiting_since
FROM projects p JOIN users su ON su.id = p.created_by
WHERE p.status = 'new'
ORDER BY waiting_since
LIMIT 50;

-- name: ReportTiming :one
-- Hours from first submission to first assignment / completion, for events in the range.
SELECT
  count(*) FILTER (WHERE first_assigned_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS assigned_n,
  coalesce(avg(extract(epoch FROM first_assigned_at - first_submitted_at) / 3600)
    FILTER (WHERE first_assigned_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz), 0)::float8 AS assign_avg_h,
  coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM first_assigned_at - first_submitted_at) / 3600)
    FILTER (WHERE first_assigned_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz), 0)::float8 AS assign_median_h,
  count(*) FILTER (WHERE status = 'completed' AND completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS completed_n,
  coalesce(avg(extract(epoch FROM completed_at - first_submitted_at) / 3600)
    FILTER (WHERE status = 'completed' AND completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz), 0)::float8 AS complete_avg_h,
  coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM completed_at - first_submitted_at) / 3600)
    FILTER (WHERE status = 'completed' AND completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz), 0)::float8 AS complete_median_h
FROM projects
WHERE first_submitted_at IS NOT NULL;

-- name: ReportUpdatesByDestinationStatus :many
SELECT destination, status, count(*) AS n
FROM updates
WHERE created_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
GROUP BY destination, status;

-- name: ReportApprovalTurnaround :one
-- Time from each submission for approval to the TL's decision (approve or send back).
WITH ev AS (
  SELECT to_status, created_at,
         lead(to_status)  OVER w AS next_status,
         lead(created_at) OVER w AS next_at
  FROM update_status_history
  WINDOW w AS (PARTITION BY update_id ORDER BY created_at, id)
)
SELECT count(*) AS n,
       coalesce(avg(extract(epoch FROM next_at - created_at) / 3600), 0)::float8 AS avg_h,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM next_at - created_at) / 3600), 0)::float8 AS median_h,
       count(*) FILTER (WHERE next_status = 'changes_requested') AS sent_back
FROM ev
WHERE to_status = 'pending_approval' AND next_status IN ('approved', 'changes_requested')
  AND next_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz;

-- name: ReportRevisions :one
SELECT count(*) AS completed_n,
       coalesce(avg(revision_count), 0)::float8 AS avg_revisions,
       count(*) FILTER (WHERE revision_count > 0) AS with_revisions
FROM projects
WHERE status = 'completed' AND completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz;

-- name: ReportTopRevisions :many
SELECT p.id, p.number, p.name, p.revision_count, p.status, au.name AS assignee_name
FROM projects p LEFT JOIN users au ON au.id = p.assigned_to
WHERE p.revision_count > 0
ORDER BY p.revision_count DESC, p.updated_at DESC
LIMIT 10;

-- name: ReportSalesByPerson :many
SELECT u.id, u.name,
  count(p.id) FILTER (WHERE p.created_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS created,
  count(p.id) FILTER (WHERE p.first_submitted_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS submitted,
  count(p.id) FILTER (WHERE p.status = 'completed' AND p.completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS completed,
  count(p.id) FILTER (WHERE p.status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')) AS active_now
FROM users u
LEFT JOIN projects p ON p.created_by = u.id
WHERE u.role = 'sales'
GROUP BY u.id, u.name
ORDER BY completed DESC, submitted DESC, u.name;

-- name: ReportSalesRevenue :many
-- What Growvence received (agreed price less the marketplace's share) for projects
-- completed in the range, per Sales person and currency.
SELECT p.created_by AS user_id, p.currency::text AS currency,
       sum(round(p.agreed_price * (100 - p.platform_fee_percent) / 100, 2))::text AS total
FROM projects p
WHERE p.status = 'completed' AND p.completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
  AND p.agreed_price IS NOT NULL
GROUP BY p.created_by, p.currency;

-- name: ListActivity :many
SELECT a.id, a.action, a.entity_type, a.entity_id, a.project_id, a.client_request_id, a.metadata, a.created_at,
       a.actor_id, u.name AS actor_name, coalesce(p.number, r.number, 0)::bigint AS project_number,
       -- The container's name: a project's (a cancelled one may have none) or a request's
       -- title; empty when there is none, so the caller never has to unwrap it.
       coalesce(CASE WHEN a.client_request_id IS NOT NULL THEN r.title ELSE p.name END, '')::text AS project_name
FROM activity_log a
LEFT JOIN users u ON u.id = a.actor_id
LEFT JOIN projects p ON p.id = a.project_id
LEFT JOIN client_requests r ON r.id = a.client_request_id
WHERE (sqlc.narg('before_id')::bigint IS NULL OR a.id < sqlc.narg('before_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR a.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('client_request_id')::uuid IS NULL OR a.client_request_id = sqlc.narg('client_request_id'))
  AND (sqlc.narg('actor_id')::uuid IS NULL OR a.actor_id = sqlc.narg('actor_id'))
  AND (sqlc.narg('action_prefix')::text IS NULL OR a.action LIKE sqlc.narg('action_prefix') || '%')
  -- TLs see work activity only (projects and client requests), and never from drafts.
  AND (NOT sqlc.arg('projects_only')::boolean
       OR (a.project_id IS NOT NULL AND p.status <> 'draft') OR a.client_request_id IS NOT NULL)
  -- A Team Leader sees a request's trail only if it is with one of their teams.
  AND (sqlc.narg('scope_lead')::uuid IS NULL OR a.client_request_id IS NULL OR EXISTS (
        SELECT 1 FROM client_request_details cr WHERE cr.id = a.client_request_id
          AND (cr.team_lead_id IS NULL OR cr.team_lead_id = sqlc.narg('scope_lead')
               OR cr.team_head_id = sqlc.narg('scope_lead'))))
  AND (sqlc.narg('scope_lead')::uuid IS NULL OR p.team_id IS NULL OR NOT EXISTS (
        SELECT 1 FROM teams t JOIN users l ON l.id = t.lead_id AND l.is_active AND l.role = 'tl'
        LEFT JOIN departments d ON d.id = t.department_id AND d.is_active
        LEFT JOIN users h ON h.id = d.head_id AND h.is_active AND h.role = 'tl'
        WHERE t.id = p.team_id AND l.id <> sqlc.narg('scope_lead')
          AND h.id IS DISTINCT FROM sqlc.narg('scope_lead')))
ORDER BY a.id DESC
LIMIT sqlc.arg('lim');
