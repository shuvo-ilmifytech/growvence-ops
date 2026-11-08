-- Milestone payment queries (Phase 2).

-- name: ListMilestonesByProject :many
SELECT m.id, m.project_id, m.label, m.amount::text AS amount, m.currency,
       m.due_date::text AS due_date, m.status, m.invoice_id,
       m.paid_amount::text AS paid_amount, m.paid_at,
       m.payment_method, m.payment_reference, m.created_by, m.created_at, m.updated_at,
       m.team_id, m.sla_response_hours, t.name AS team_name,
       i.invoice_number, i.status AS invoice_status
FROM payment_milestones m
LEFT JOIN teams t ON t.id = m.team_id
LEFT JOIN client_invoices i ON i.id = m.invoice_id
WHERE m.project_id = $1
ORDER BY m.due_date ASC, m.created_at ASC;

-- name: GetMilestone :one
SELECT m.id, m.project_id, m.label, m.amount::text AS amount, m.currency,
       m.due_date::text AS due_date, m.status, m.invoice_id,
       m.paid_amount::text AS paid_amount, m.paid_at,
       m.payment_method, m.payment_reference, m.created_by, m.created_at, m.updated_at,
       m.team_id, m.sla_response_hours, t.name AS team_name,
       i.invoice_number, i.status AS invoice_status
FROM payment_milestones m
LEFT JOIN teams t ON t.id = m.team_id
LEFT JOIN client_invoices i ON i.id = m.invoice_id
WHERE m.id = $1;

-- name: CreateMilestone :one
INSERT INTO payment_milestones (project_id, label, amount, currency, due_date, team_id, sla_response_hours, created_by)
VALUES (sqlc.arg('project_id'), sqlc.arg('label'), sqlc.arg('amount')::text::numeric,
        sqlc.arg('currency'), sqlc.arg('due_date')::text::date,
        sqlc.arg('team_id'), sqlc.arg('sla_response_hours'), sqlc.arg('created_by'))
RETURNING id;

-- name: UpdateMilestone :exec
UPDATE payment_milestones
SET label = $2, amount = $3::text::numeric, currency = $4, due_date = $5::text::date,
    team_id = $6, sla_response_hours = $7, updated_at = now()
WHERE id = $1 AND status IN ('scheduled', 'invoiced');

-- name: DeleteMilestone :exec
DELETE FROM payment_milestones
WHERE id = $1 AND status IN ('scheduled', 'invoiced') AND invoice_id IS NULL;

-- name: ListMilestonesDueForInvoice :many
-- Scheduled milestones whose due date has arrived and which have no invoice yet.
SELECT m.id, m.project_id, m.label, m.amount::text AS amount, m.currency,
       m.due_date::text AS due_date, p.name AS project_name
FROM payment_milestones m
JOIN projects p ON p.id = m.project_id
WHERE m.status = 'scheduled' AND m.due_date <= CURRENT_DATE AND m.invoice_id IS NULL
ORDER BY m.due_date ASC;

-- name: ListMilestonesToMarkOverdue :many
SELECT m.id, m.project_id, m.label, m.amount::text AS amount, m.currency,
       m.due_date::text AS due_date, p.name AS project_name
FROM payment_milestones m
JOIN projects p ON p.id = m.project_id
WHERE m.status = 'invoiced' AND m.due_date < CURRENT_DATE
ORDER BY m.due_date ASC;

-- name: ListDueMilestones :many
-- Team dashboard: overdue first, then due within the next $1 days.
SELECT m.id, m.project_id, m.label, m.amount::text AS amount, m.currency,
       m.due_date::text AS due_date, m.status, m.invoice_id,
       m.paid_amount::text AS paid_amount, m.paid_at,
       m.payment_method, m.payment_reference, m.created_by, m.created_at, m.updated_at,
       p.name AS project_name, p.number AS project_number,
       i.invoice_number, i.status AS invoice_status
FROM payment_milestones m
JOIN projects p ON p.id = m.project_id
LEFT JOIN client_invoices i ON i.id = m.invoice_id
WHERE m.status IN ('scheduled', 'invoiced', 'partially_paid', 'overdue')
  AND m.due_date <= CURRENT_DATE + ($1::int)
ORDER BY CASE WHEN m.status = 'overdue' THEN 0 WHEN m.due_date < CURRENT_DATE THEN 1 ELSE 2 END,
         m.due_date ASC;

-- name: CreateMilestoneReminder :one
INSERT INTO milestone_reminders (milestone_id, sent_by, channel, note)
VALUES ($1, $2, $3, $4)
RETURNING id, sent_at;

-- name: ListMilestoneReminders :many
SELECT r.id, r.milestone_id, r.sent_at, r.sent_by, r.channel, r.note,
       u.name AS sent_by_name
FROM milestone_reminders r
LEFT JOIN users u ON u.id = r.sent_by
WHERE r.milestone_id = $1
ORDER BY r.sent_at DESC;

-- name: CountManualRemindersToday :one
SELECT count(*) FROM milestone_reminders
WHERE milestone_id = $1 AND sent_by = $2 AND channel = 'manual'
  AND sent_at >= CURRENT_DATE;

-- name: ListProjectClientUsers :many
-- Client-portal users linked to a project (for invoice + reminder notices).
SELECT u.id, u.name, u.email
FROM client_project_access a
JOIN users u ON u.id = a.client_user_id
WHERE a.project_id = $1 AND u.is_active = true
ORDER BY u.name;
