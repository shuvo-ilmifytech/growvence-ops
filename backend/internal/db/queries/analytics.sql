-- Queries behind the Analytics page: how much work comes in, how much goes out, and how
-- each team and person performs, for a date range. Counts only, never money.
--   incoming  = projects whose first submission (creation, since Phase 16) falls in the range
--   delivered = projects marked Delivered in the range (delivered_at), whatever they are now
--   completed = projects the client accepted in the range (completed_at)
--   on time   = delivered no later than the due date, or the client deadline when there is none
--   turnaround = hours from first submission to delivery
-- team_id narrows everything to one team; NULL means the whole company.

-- name: AnalyticsSeries :many
-- One row per day, week or month in the range (empty buckets included), in the org timezone.
WITH b AS (
  SELECT gs AS local_start,
         (gs AT TIME ZONE org_timezone()) AS start_ts,
         ((gs + ('1 ' || sqlc.arg('bucket')::text)::interval) AT TIME ZONE org_timezone()) AS end_ts
  FROM generate_series(
    date_trunc(sqlc.arg('bucket')::text, sqlc.arg('from_ts')::timestamptz AT TIME ZONE org_timezone()),
    sqlc.arg('to_ts')::timestamptz AT TIME ZONE org_timezone(),
    ('1 ' || sqlc.arg('bucket')::text)::interval
  ) gs
), p AS (
  SELECT first_submitted_at, delivered_at, completed_at, cancelled_at, status
  FROM projects
  WHERE sqlc.narg('team_id')::uuid IS NULL OR team_id = sqlc.narg('team_id')::uuid
)
SELECT to_char(b.local_start, 'YYYY-MM-DD') AS start,
  (SELECT count(*) FROM p WHERE p.first_submitted_at >= greatest(b.start_ts, sqlc.arg('from_ts')::timestamptz)
     AND p.first_submitted_at < b.end_ts AND p.first_submitted_at <= sqlc.arg('to_ts')::timestamptz) AS incoming,
  (SELECT count(*) FROM p WHERE p.delivered_at >= greatest(b.start_ts, sqlc.arg('from_ts')::timestamptz)
     AND p.delivered_at < b.end_ts AND p.delivered_at <= sqlc.arg('to_ts')::timestamptz) AS delivered,
  (SELECT count(*) FROM p WHERE p.status = 'completed' AND p.completed_at >= greatest(b.start_ts, sqlc.arg('from_ts')::timestamptz)
     AND p.completed_at < b.end_ts AND p.completed_at <= sqlc.arg('to_ts')::timestamptz) AS completed,
  (SELECT count(*) FROM p WHERE p.status = 'cancelled' AND p.cancelled_at >= greatest(b.start_ts, sqlc.arg('from_ts')::timestamptz)
     AND p.cancelled_at < b.end_ts AND p.cancelled_at <= sqlc.arg('to_ts')::timestamptz) AS cancelled
FROM b
ORDER BY b.local_start;

-- name: AnalyticsTotals :one
-- The range in numbers, plus where the work stands right now.
SELECT
  count(*) FILTER (WHERE first_submitted_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS incoming,
  count(*) FILTER (WHERE delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS delivered,
  count(*) FILTER (WHERE status = 'completed' AND completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS completed,
  count(*) FILTER (WHERE status = 'cancelled' AND cancelled_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS cancelled,
  count(*) FILTER (WHERE delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                     AND coalesce(end_of_day(due_date), deadline) IS NOT NULL) AS delivered_with_due,
  count(*) FILTER (WHERE delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                     AND delivered_at <= coalesce(end_of_day(due_date), deadline)) AS on_time,
  coalesce(avg(extract(epoch FROM delivered_at - first_submitted_at) / 3600)
    FILTER (WHERE delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz), 0)::float8 AS turnaround_avg_h,
  coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM delivered_at - first_submitted_at) / 3600)
    FILTER (WHERE delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz), 0)::float8 AS turnaround_median_h,
  count(*) FILTER (WHERE delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                     AND revision_count > 0) AS delivered_with_revisions,
  count(*) FILTER (WHERE status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')) AS live_now,
  count(*) FILTER (WHERE status = 'new') AS new_now,
  count(*) FILTER (WHERE status = 'in_progress') AS in_progress_now,
  count(*) FILTER (WHERE status = 'blocked') AS blocked_now,
  count(*) FILTER (WHERE status = 'ready_for_review') AS in_review_now,
  count(*) FILTER (WHERE status = 'ready_to_deliver') AS ready_to_deliver_now,
  count(*) FILTER (WHERE status = 'delivered') AS delivered_now,
  count(*) FILTER (WHERE status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')
                     AND coalesce(end_of_day(due_date), deadline) < now()) AS overdue_now
FROM projects
WHERE sqlc.narg('team_id')::uuid IS NULL OR team_id = sqlc.narg('team_id')::uuid;

-- name: AnalyticsTeams :many
-- Per active team: what came in and went out in the range, how well, and what it holds now.
SELECT t.id, t.name, lu.name AS lead_name,
  (SELECT count(*) FROM team_members tm WHERE tm.team_id = t.id) AS members,
  count(p.id) FILTER (WHERE p.first_submitted_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS incoming,
  count(p.id) FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS delivered,
  count(p.id) FILTER (WHERE p.status = 'completed' AND p.completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS completed,
  count(p.id) FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                        AND coalesce(end_of_day(p.due_date), p.deadline) IS NOT NULL) AS delivered_with_due,
  count(p.id) FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                        AND p.delivered_at <= coalesce(end_of_day(p.due_date), p.deadline)) AS on_time,
  coalesce(avg(extract(epoch FROM p.delivered_at - p.first_submitted_at) / 3600)
    FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz), 0)::float8 AS turnaround_avg_h,
  count(p.id) FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                        AND p.revision_count > 0) AS delivered_with_revisions,
  count(p.id) FILTER (WHERE p.status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')) AS live_now,
  count(p.id) FILTER (WHERE p.status = 'new') AS new_now,
  count(p.id) FILTER (WHERE p.status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')
                        AND coalesce(end_of_day(p.due_date), p.deadline) < now()) AS overdue_now
FROM teams t
LEFT JOIN users lu ON lu.id = t.lead_id AND lu.is_active
LEFT JOIN projects p ON p.team_id = t.id
WHERE t.is_active
GROUP BY t.id, t.name, lu.name
ORDER BY delivered DESC, incoming DESC, t.name;

-- name: AnalyticsPeople :many
-- Per active member and team lead (leads take projects on too): what they delivered in the
-- range and how well, and what they carry now. Credit goes to everyone on the project.
-- A lead is shown under the first team they lead, like the workload view.
SELECT u.id, u.name, u.role, t.id AS team_id, t.name AS team_name,
  count(p.id) FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS delivered,
  count(p.id) FILTER (WHERE p.status = 'completed' AND p.completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS completed,
  count(p.id) FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                        AND coalesce(end_of_day(p.due_date), p.deadline) IS NOT NULL) AS delivered_with_due,
  count(p.id) FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                        AND p.delivered_at <= coalesce(end_of_day(p.due_date), p.deadline)) AS on_time,
  coalesce(avg(extract(epoch FROM p.delivered_at - p.first_submitted_at) / 3600)
    FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz), 0)::float8 AS turnaround_avg_h,
  count(p.id) FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                        AND p.revision_count > 0) AS delivered_with_revisions,
  count(p.id) FILTER (WHERE p.status IN ('in_progress','blocked','ready_for_review','ready_to_deliver')) AS open_now,
  count(p.id) FILTER (WHERE p.status IN ('in_progress','blocked','ready_for_review','ready_to_deliver')
                        AND coalesce(end_of_day(p.due_date), p.deadline) < now()) AS overdue_now
FROM users u
LEFT JOIN team_members tm ON tm.user_id = u.id
LEFT JOIN LATERAL (
  SELECT lt.id FROM teams lt WHERE u.role = 'tl' AND lt.lead_id = u.id AND lt.is_active ORDER BY lt.name LIMIT 1
) led ON true
LEFT JOIN teams t ON t.id = coalesce(tm.team_id, led.id)
LEFT JOIN project_members pm ON pm.user_id = u.id
LEFT JOIN projects p ON p.id = pm.project_id
WHERE u.is_active AND u.role IN ('member', 'tl')
  AND (sqlc.narg('team_id')::uuid IS NULL OR t.id = sqlc.narg('team_id')::uuid)
GROUP BY u.id, u.name, u.role, t.id, t.name
ORDER BY delivered DESC, open_now DESC, u.name;

-- name: AnalyticsByType :many
-- Per project type: what came in and went out in the range.
SELECT pt.id, pt.name,
  count(p.id) FILTER (WHERE p.first_submitted_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS incoming,
  count(p.id) FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS delivered,
  count(p.id) FILTER (WHERE p.status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')) AS live_now
FROM project_types pt
JOIN projects p ON p.project_type_id = pt.id
WHERE sqlc.narg('team_id')::uuid IS NULL OR p.team_id = sqlc.narg('team_id')::uuid
GROUP BY pt.id, pt.name
HAVING count(p.id) FILTER (WHERE p.first_submitted_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) > 0
    OR count(p.id) FILTER (WHERE p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) > 0
    OR count(p.id) FILTER (WHERE p.status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')) > 0
ORDER BY incoming DESC, delivered DESC, pt.name;

-- name: AnalyticsSales :many
-- Per salesperson: orders they brought in during the range and how those orders ended up.
SELECT u.id, u.name,
  count(p.id) AS incoming,
  count(p.id) FILTER (WHERE p.status IN ('delivered', 'completed')) AS delivered,
  count(p.id) FILTER (WHERE p.status = 'completed') AS completed,
  count(p.id) FILTER (WHERE p.status = 'cancelled') AS cancelled
FROM users u
JOIN projects p ON p.created_by = u.id
  AND p.first_submitted_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
  AND (sqlc.narg('team_id')::uuid IS NULL OR p.team_id = sqlc.narg('team_id')::uuid)
WHERE u.role = 'sales'
GROUP BY u.id, u.name
ORDER BY incoming DESC, u.name;

-- name: AnalyticsRevenue :many
-- What was delivered in the range, after the marketplace's fee, per team and currency.
-- Only projects still Delivered or Completed count (a reopened one counts when it's
-- delivered again). team_id is NULL for projects with no team.
SELECT p.team_id, p.currency::text AS currency,
       sum(round(p.agreed_price * (100 - p.platform_fee_percent) / 100, 2))::text AS net,
       count(*) AS projects
FROM projects p
WHERE p.status IN ('delivered', 'completed') AND p.agreed_price IS NOT NULL
  AND p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
  AND (sqlc.narg('team_id')::uuid IS NULL OR p.team_id = sqlc.narg('team_id')::uuid)
GROUP BY p.team_id, p.currency
ORDER BY p.currency;

-- name: AnalyticsStepTimes :many
-- Hours spent in each step by the projects delivered in the range, up to their delivery:
-- the average and median per project, and how many projects passed through the step.
WITH d AS (
  SELECT id, delivered_at FROM projects
  WHERE delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
    AND (sqlc.narg('team_id')::uuid IS NULL OR team_id = sqlc.narg('team_id')::uuid)
), h AS (
  SELECT sh.project_id, sh.to_status, sh.created_at,
         lead(sh.created_at) OVER (PARTITION BY sh.project_id ORDER BY sh.created_at, sh.id) AS next_at
  FROM project_status_history sh JOIN d ON d.id = sh.project_id
), per AS (
  SELECT h.project_id, h.to_status AS step,
         sum(extract(epoch FROM least(coalesce(h.next_at, d.delivered_at), d.delivered_at) - h.created_at) / 3600) AS hours
  FROM h JOIN d ON d.id = h.project_id
  WHERE h.created_at < d.delivered_at
    AND h.to_status IN ('new', 'in_progress', 'blocked', 'ready_for_review', 'ready_to_deliver')
  GROUP BY h.project_id, h.to_status
)
SELECT step::text AS step, count(*) AS projects,
       coalesce(avg(hours), 0)::float8 AS avg_h,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY hours), 0)::float8 AS median_h,
       coalesce(max(hours), 0)::float8 AS max_h
FROM per
GROUP BY step;

-- name: AnalyticsStuckNow :many
-- Open projects by the step they're in now: how many, and how long they've sat there.
SELECT s.status::text AS step, count(*) AS projects,
       coalesce(avg(extract(epoch FROM now() - s.since) / 3600), 0)::float8 AS avg_h,
       coalesce(max(extract(epoch FROM now() - s.since) / 3600), 0)::float8 AS max_h
FROM (
  SELECT p.status,
         coalesce((SELECT max(sh.created_at) FROM project_status_history sh
                   WHERE sh.project_id = p.id AND sh.to_status = p.status),
                  p.first_submitted_at, p.created_at) AS since
  FROM projects p
  WHERE p.status IN ('new', 'in_progress', 'blocked', 'ready_for_review', 'ready_to_deliver', 'delivered')
    AND (sqlc.narg('team_id')::uuid IS NULL OR p.team_id = sqlc.narg('team_id')::uuid)
) s
GROUP BY s.status;

-- name: AnalyticsLateness :one
-- How late the late deliveries in the range were, in hours past the due moment.
SELECT count(*) AS late,
       coalesce(avg(extract(epoch FROM delivered_at - coalesce(end_of_day(due_date), deadline)) / 3600), 0)::float8 AS avg_h,
       coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM delivered_at - coalesce(end_of_day(due_date), deadline)) / 3600), 0)::float8 AS median_h,
       coalesce(max(extract(epoch FROM delivered_at - coalesce(end_of_day(due_date), deadline)) / 3600), 0)::float8 AS max_h
FROM projects
WHERE delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
  AND delivered_at > coalesce(end_of_day(due_date), deadline)
  AND (sqlc.narg('team_id')::uuid IS NULL OR team_id = sqlc.narg('team_id')::uuid);

-- name: AnalyticsPersonTrend :many
-- Deliveries per person per month (org timezone) since from_ts, for the trend table.
SELECT pm.user_id, to_char(date_trunc('month', p.delivered_at AT TIME ZONE org_timezone()), 'YYYY-MM') AS month,
       count(*) AS delivered
FROM projects p JOIN project_members pm ON pm.project_id = p.id
WHERE p.delivered_at >= sqlc.arg('from_ts')::timestamptz
  AND (sqlc.narg('team_id')::uuid IS NULL OR p.team_id = sqlc.narg('team_id')::uuid)
GROUP BY pm.user_id, month;

-- name: AnalyticsTeamTrend :many
-- Deliveries and revenue (after the fee) per team, month and currency since from_ts.
SELECT p.team_id, to_char(date_trunc('month', p.delivered_at AT TIME ZONE org_timezone()), 'YYYY-MM') AS month,
       p.currency::text AS currency, count(*) AS delivered,
       coalesce(sum(round(p.agreed_price * (100 - p.platform_fee_percent) / 100, 2))
         FILTER (WHERE p.status IN ('delivered', 'completed')), 0)::text AS net
FROM projects p
WHERE p.delivered_at >= sqlc.arg('from_ts')::timestamptz
  AND (sqlc.narg('team_id')::uuid IS NULL OR p.team_id = sqlc.narg('team_id')::uuid)
GROUP BY p.team_id, month, p.currency;

-- name: AnalyticsProjects :many
-- The projects behind a number on the page, plus the CSV. Filters are optional; metric
-- picks which projects count:
--   incoming, delivered, completed, cancelled, on_time, late, revisions (delivered with
--   at least one revision): in the range;  open, overdue, and the steps (new, in_progress,
--   blocked, ready_for_review, ready_to_deliver, awaiting_client): now;  all: incoming or
--   delivered in the range.
SELECT p.id, p.number, p.name, p.status, p.client_name, p.order_id,
       t.name AS team_name, pt.name AS type_name, su.name AS sales_name,
       coalesce((SELECT string_agg(mu.name, ', ' ORDER BY mu.name) FROM project_members m
                 JOIN users mu ON mu.id = m.user_id WHERE m.project_id = p.id), '')::text AS members,
       p.first_submitted_at, p.first_assigned_at, p.delivered_at, p.completed_at, p.cancelled_at,
       coalesce(iso(coalesce(end_of_day(p.due_date), p.deadline)), '')::text AS due,
       CASE WHEN p.delivered_at IS NOT NULL AND coalesce(end_of_day(p.due_date), p.deadline) IS NOT NULL
            THEN (extract(epoch FROM p.delivered_at - coalesce(end_of_day(p.due_date), p.deadline)) / 3600)::float8 END AS late_h,
       CASE WHEN p.delivered_at IS NOT NULL AND p.first_submitted_at IS NOT NULL
            THEN (extract(epoch FROM p.delivered_at - p.first_submitted_at) / 3600)::float8 END AS turnaround_h,
       p.revision_count, p.currency::text AS currency,
       coalesce(p.agreed_price::text, '')::text AS agreed_price,
       coalesce(round(p.agreed_price * (100 - p.platform_fee_percent) / 100, 2)::text, '')::text AS net
FROM projects p
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN project_types pt ON pt.id = p.project_type_id
JOIN users su ON su.id = p.created_by
WHERE (sqlc.narg('team_id')::uuid IS NULL OR p.team_id = sqlc.narg('team_id')::uuid)
  AND (sqlc.narg('type_id')::uuid IS NULL OR p.project_type_id = sqlc.narg('type_id')::uuid)
  AND (sqlc.narg('sales_id')::uuid IS NULL OR p.created_by = sqlc.narg('sales_id')::uuid)
  AND (sqlc.narg('user_id')::uuid IS NULL OR EXISTS (
        SELECT 1 FROM project_members m WHERE m.project_id = p.id AND m.user_id = sqlc.narg('user_id')::uuid))
  AND CASE sqlc.arg('metric')::text
    WHEN 'incoming' THEN p.first_submitted_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
    WHEN 'delivered' THEN p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
    WHEN 'completed' THEN p.status = 'completed' AND p.completed_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
    WHEN 'cancelled' THEN p.status = 'cancelled' AND p.cancelled_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
    WHEN 'on_time' THEN p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                        AND p.delivered_at <= coalesce(end_of_day(p.due_date), p.deadline)
    WHEN 'late' THEN p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                     AND p.delivered_at > coalesce(end_of_day(p.due_date), p.deadline)
    WHEN 'revisions' THEN p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                          AND p.revision_count > 0
    WHEN 'open' THEN p.status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')
    WHEN 'member_open' THEN p.status IN ('in_progress','blocked','ready_for_review','ready_to_deliver')
    WHEN 'overdue' THEN p.status IN ('new','in_progress','blocked','ready_for_review','ready_to_deliver')
                        AND coalesce(end_of_day(p.due_date), p.deadline) < now()
    WHEN 'awaiting_client' THEN p.status = 'delivered'
    WHEN 'new' THEN p.status = 'new'
    WHEN 'in_progress' THEN p.status = 'in_progress'
    WHEN 'blocked' THEN p.status = 'blocked'
    WHEN 'ready_for_review' THEN p.status = 'ready_for_review'
    WHEN 'ready_to_deliver' THEN p.status = 'ready_to_deliver'
    WHEN 'all' THEN p.first_submitted_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
                 OR p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz
    ELSE false
  END
ORDER BY coalesce(p.delivered_at, p.first_submitted_at, p.created_at) DESC
LIMIT 2000;

-- name: ListTeamTargets :many
-- Every active team for a month, with its target (if any) and what it delivered that month
-- in the target's currency (USD when there's no target), after the marketplace's fee.
SELECT t.id AS team_id, t.name AS team_name, lu.name AS lead_name,
       -- amount is '' when no target is set.
       coalesce(tt.amount::text, '')::text AS amount, coalesce(tt.currency, 'USD')::text AS currency,
       sb.name AS set_by_name, tt.updated_at,
       coalesce((SELECT sum(round(p.agreed_price * (100 - p.platform_fee_percent) / 100, 2)) FROM projects p
         WHERE p.team_id = t.id AND p.status IN ('delivered', 'completed') AND p.agreed_price IS NOT NULL
           AND p.currency = coalesce(tt.currency, 'USD')
           AND p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz), 0)::text AS achieved,
       (SELECT count(*) FROM projects p
         WHERE p.team_id = t.id AND p.status IN ('delivered', 'completed') AND p.agreed_price IS NOT NULL
           AND p.currency = coalesce(tt.currency, 'USD')
           AND p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS projects,
       (SELECT count(*) FROM projects p
         WHERE p.team_id = t.id AND p.status IN ('delivered', 'completed')
           AND p.currency <> coalesce(tt.currency, 'USD')
           AND p.delivered_at BETWEEN sqlc.arg('from_ts')::timestamptz AND sqlc.arg('to_ts')::timestamptz) AS other_currency
FROM teams t
LEFT JOIN users lu ON lu.id = t.lead_id AND lu.is_active
LEFT JOIN team_targets tt ON tt.team_id = t.id AND tt.month = sqlc.arg('month')::text::date
LEFT JOIN users sb ON sb.id = tt.set_by
WHERE t.is_active
ORDER BY t.name;

-- name: ListTargetHistory :many
-- Targets set for the months from from_month on, for the trend table.
SELECT team_id, to_char(month, 'YYYY-MM') AS month, amount::text AS amount, currency::text AS currency
FROM team_targets
WHERE month >= sqlc.arg('from_month')::text::date;

-- name: UpsertTeamTarget :one
INSERT INTO team_targets (team_id, month, amount, currency, set_by)
VALUES (sqlc.arg('team_id'), sqlc.arg('month')::text::date, sqlc.arg('amount')::text::numeric, sqlc.arg('currency'), sqlc.arg('set_by'))
ON CONFLICT (team_id, month) DO UPDATE
  SET amount = excluded.amount, currency = excluded.currency, set_by = excluded.set_by, updated_at = now()
RETURNING team_id, to_char(month, 'YYYY-MM') AS month, amount::text AS amount, currency::text AS currency;

-- name: DeleteTeamTarget :execrows
DELETE FROM team_targets WHERE team_id = sqlc.arg('team_id') AND month = sqlc.arg('month')::text::date;
