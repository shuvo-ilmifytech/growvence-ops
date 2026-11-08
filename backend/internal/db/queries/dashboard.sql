-- Queries behind the Team Leader and CEO dashboards and the workload view.
-- "Open" means the project is with the team: assigned, in progress, blocked or ready for review.
-- "Live" adds the two states before assignment, so it counts everything not finished.

-- name: Workload :many
-- One row per active member, and per team lead (leads take projects on too): what
-- they are carrying now and what is late. A lead is shown under the team they lead.
-- teams limits it to those teams (the ones a Team Leader leads or heads); unteamed
-- limits it to members in no team, which is what a Team Leader without a team can
-- see. NULL and false together cover everyone, for the CEO.
SELECT u.id, u.name, t.id AS team_id, t.name AS team_name,
  (SELECT count(*) FROM project_members pm JOIN projects p ON p.id = pm.project_id
    WHERE pm.user_id = u.id AND p.status IN ('in_progress','blocked','ready_for_review','ready_to_deliver')) AS open_projects,
  (SELECT count(*) FROM project_members pm JOIN projects p ON p.id = pm.project_id
    WHERE pm.user_id = u.id AND p.status IN ('in_progress','blocked','ready_for_review','ready_to_deliver')
      AND coalesce(end_of_day(p.due_date), p.deadline) < now()) AS overdue_projects,
  (SELECT count(*) FROM project_members pm JOIN projects p ON p.id = pm.project_id
    WHERE pm.user_id = u.id AND p.status = 'blocked') AS blocked_projects,
  -- Cards count whether they hang on a project or on a client request.
  (SELECT count(*) FROM card_assignees ca JOIN project_cards c ON c.id = ca.card_id
    LEFT JOIN projects p ON p.id = c.project_id
    LEFT JOIN client_requests r ON r.id = c.client_request_id
    WHERE ca.user_id = u.id AND c.removed_at IS NULL AND c.column_key <> 'done'
      AND coalesce(p.status, r.status) NOT IN ('cancelled','completed','done')) AS open_cards,
  (SELECT count(*) FROM card_assignees ca JOIN project_cards c ON c.id = ca.card_id
    LEFT JOIN projects p ON p.id = c.project_id
    LEFT JOIN client_requests r ON r.id = c.client_request_id
    WHERE ca.user_id = u.id AND c.removed_at IS NULL AND c.column_key <> 'done'
      AND coalesce(p.status, r.status) NOT IN ('cancelled','completed','done')
      AND c.due_date < (now() AT TIME ZONE org_timezone())::date) AS late_cards,
  (SELECT count(*) FROM project_members pm JOIN projects p ON p.id = pm.project_id
    WHERE pm.user_id = u.id AND p.status IN ('delivered','completed')
      AND p.delivered_at >= now() - interval '30 days') AS completed_30d,
  -- Already away, so a Team Leader thinks twice before handing them more.
  EXISTS (SELECT 1 FROM leave_requests l WHERE l.user_id = u.id AND l.status = 'approved'
          AND (now() AT TIME ZONE org_timezone())::date BETWEEN l.start_date AND l.end_date) AS on_leave_today
FROM users u
LEFT JOIN team_members tm ON tm.user_id = u.id
LEFT JOIN LATERAL (
  SELECT lt.id FROM teams lt
  WHERE u.role = 'tl' AND lt.lead_id = u.id AND lt.is_active
    AND (sqlc.narg('teams')::uuid[] IS NULL OR lt.id = ANY (sqlc.narg('teams')::uuid[]))
  ORDER BY lt.name LIMIT 1
) led ON true
LEFT JOIN teams t ON t.id = coalesce(tm.team_id, led.id)
WHERE u.is_active AND (u.role = 'member' OR led.id IS NOT NULL)
  AND (sqlc.narg('teams')::uuid[] IS NULL OR coalesce(tm.team_id, led.id) = ANY (sqlc.narg('teams')::uuid[]))
  AND (NOT sqlc.arg('unteamed')::boolean OR (u.role = 'member' AND tm.team_id IS NULL))
ORDER BY open_projects DESC, open_cards DESC, u.name;

-- name: TeamThroughput :many
-- Per team: how much is live, how much is late, and what it delivered this month.
SELECT t.id, t.name, lu.name AS lead_name,
  (SELECT count(*) FROM team_members tm WHERE tm.team_id = t.id) AS members,
  count(p.id) FILTER (WHERE p.status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')) AS live,
  count(p.id) FILTER (WHERE p.status = 'new') AS unassigned,
  count(p.id) FILTER (WHERE p.status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')
                        AND coalesce(end_of_day(p.due_date), p.deadline) < now()) AS overdue,
  count(p.id) FILTER (WHERE p.status IN ('delivered','completed')
                        AND p.delivered_at >= date_trunc('month', now() AT TIME ZONE org_timezone())
                            AT TIME ZONE org_timezone()) AS completed_this_month
FROM teams t
LEFT JOIN users lu ON lu.id = t.lead_id AND lu.is_active
LEFT JOIN projects p ON p.team_id = t.id
WHERE t.is_active
  AND (sqlc.narg('teams')::uuid[] IS NULL OR t.id = ANY (sqlc.narg('teams')::uuid[]))
GROUP BY t.id, t.name, lu.name
ORDER BY live DESC, t.name;

-- name: CompanyKpis :one
-- The company at a glance, as of now in the organisation's timezone.
SELECT
  (SELECT count(*) FROM projects
    WHERE status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')) AS live_projects,
  (SELECT count(*) FROM projects WHERE status = 'new') AS unassigned_projects,
  (SELECT count(*) FROM projects
    WHERE status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')
      AND coalesce(end_of_day(due_date), deadline) < now()) AS overdue_projects,
  (SELECT count(*) FROM projects WHERE status = 'blocked') AS blocked_projects,
  (SELECT count(*) FROM projects WHERE status IN ('delivered','completed')
      AND delivered_at >= date_trunc('month', now() AT TIME ZONE org_timezone()) AT TIME ZONE org_timezone())
    AS completed_this_month,
  (SELECT count(*) FROM updates WHERE status = 'pending_approval') AS updates_pending_approval,
  (SELECT count(*) FROM updates WHERE status = 'approved') AS updates_unsent,
  (SELECT count(*) FROM users WHERE is_active) AS headcount,
  (SELECT count(*) FROM attendance WHERE work_date = (now() AT TIME ZONE org_timezone())::date) AS checked_in_today,
  (SELECT count(*) FROM leave_requests l WHERE l.status = 'approved'
      AND (now() AT TIME ZONE org_timezone())::date BETWEEN l.start_date AND l.end_date) AS on_leave_today;

-- name: CompanyRevenue :many
-- What Growvence received for projects completed in the range (agreed price less the
-- marketplace's share), per currency (self orders excluded later).
SELECT currency, sum(round(agreed_price * (100 - platform_fee_percent) / 100, 2))::text AS total, count(*) AS projects
FROM projects
WHERE status = 'completed' AND agreed_price IS NOT NULL
  AND completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
GROUP BY currency
ORDER BY currency;
