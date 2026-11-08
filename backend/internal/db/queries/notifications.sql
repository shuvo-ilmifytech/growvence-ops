-- name: InsertNotification :one
INSERT INTO notifications (recipient_id, type, project_id, client_request_id, update_id, title, body, data, is_important)
VALUES (sqlc.arg('recipient_id'), sqlc.arg('type'), sqlc.narg('project_id'), sqlc.narg('client_request_id'),
        sqlc.narg('update_id'), sqlc.arg('title'), sqlc.arg('body'), sqlc.arg('data'), sqlc.arg('is_important'))
RETURNING id;

-- name: FindOpenBatch :one
-- An unread notification of the same kind about the same project or client request,
-- started within the window.
SELECT id, data FROM notifications
WHERE recipient_id = sqlc.arg('recipient_id')
  AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id')
  AND client_request_id IS NOT DISTINCT FROM sqlc.narg('client_request_id')
  AND type = sqlc.arg('type') AND read_at IS NULL
  AND created_at > now() - make_interval(mins => sqlc.arg('window_minutes')::int)
ORDER BY created_at DESC
LIMIT 1
FOR UPDATE;

-- name: UpdateNotificationBatch :exec
UPDATE notifications SET title = $2, body = $3, data = $4 WHERE id = $1;

-- name: GetNotification :one
SELECT * FROM notifications WHERE id = $1;

-- name: ListNotifications :many
SELECT * FROM notifications
WHERE recipient_id = $1 AND (NOT sqlc.arg('unread_only')::boolean OR read_at IS NULL)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notifications WHERE recipient_id = $1 AND read_at IS NULL;

-- name: MarkNotificationRead :execrows
UPDATE notifications SET read_at = now() WHERE id = $1 AND recipient_id = $2 AND read_at IS NULL;

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications SET read_at = now() WHERE recipient_id = $1 AND read_at IS NULL;

-- name: ActiveUserIDsByRole :many
SELECT id FROM users WHERE role = $1 AND is_active;

-- name: OverdueProjects :many
-- Active projects past their due date (TL due date, else the delivery deadline) not yet alerted.
SELECT id, number, name, iso(coalesce(end_of_day(due_date), deadline)) AS due, assigned_to
FROM projects
WHERE status IN ('new', 'in_progress', 'blocked', 'ready_for_review', 'ready_to_deliver')
  AND coalesce(end_of_day(due_date), deadline) < now()
  AND overdue_alert_at IS NULL;

-- name: MarkOverdueAlerted :exec
UPDATE projects SET overdue_alert_at = now() WHERE id = $1;

-- name: UnassignedProjects :many
-- New projects that have waited for assignment longer than the cutoff.
SELECT p.id, p.number, p.name, p.first_submitted_at::timestamptz AS submitted_at
FROM projects p
WHERE p.status = 'new' AND p.unassigned_alert_at IS NULL
  AND p.first_submitted_at < sqlc.arg('cutoff')::timestamptz;

-- name: MarkUnassignedAlerted :exec
UPDATE projects SET unassigned_alert_at = now() WHERE id = $1;

-- name: UnsentApprovedUpdates :many
-- Approved updates nobody has posted yet, on a project or on a client request.
SELECT u.id, u.number, u.project_id, u.client_request_id,
       coalesce(p.number, r.number) AS project_number,
       coalesce(CASE WHEN u.client_request_id IS NOT NULL THEN r.title ELSE p.name END, '')::text AS project_name,
       coalesce(p.created_by, r.created_by) AS sales_id,
       coalesce(u.checked_at, u.submitted_at)::timestamptz AS approved_at
FROM updates u
LEFT JOIN projects p ON p.id = u.project_id
LEFT JOIN client_requests r ON r.id = u.client_request_id
WHERE u.status = 'approved' AND u.unsent_reminder_at IS NULL
  AND coalesce(p.status, r.status) <> 'cancelled'
  AND coalesce(u.checked_at, u.submitted_at) < sqlc.arg('cutoff')::timestamptz;

-- name: MarkUnsentReminded :exec
UPDATE updates SET unsent_reminder_at = now() WHERE id = $1;
