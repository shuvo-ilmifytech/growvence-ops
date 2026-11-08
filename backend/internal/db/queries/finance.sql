-- ── Finance (HR + CEO) ─────────────────────────────────────

-- name: ListFinanceCategories :many
SELECT c.id, c.name::text AS name, c.kind, c.is_active,
       (SELECT count(*) FROM finance_entries e WHERE e.category_id = c.id) AS entries
FROM finance_categories c
ORDER BY c.kind DESC, c.is_active DESC, c.name;

-- name: GetFinanceCategory :one
SELECT id, name::text AS name, kind, is_active FROM finance_categories WHERE id = $1;

-- name: CreateFinanceCategory :one
INSERT INTO finance_categories (kind, name) VALUES (sqlc.arg('kind'), sqlc.arg('name'))
RETURNING id;

-- name: UpdateFinanceCategory :exec
UPDATE finance_categories
SET name      = coalesce(sqlc.narg('name'), name),
    is_active = coalesce(sqlc.narg('is_active'), is_active)
WHERE id = sqlc.arg('id');

-- name: ListFinanceEntries :many
SELECT e.id, e.category_id, c.name::text AS category_name, c.kind,
       e.amount::text AS amount, e.currency, e.rate_to_bdt::text AS rate_to_bdt, e.amount_bdt::text AS amount_bdt,
       e.entry_date::text AS entry_date, e.description,
       e.invoice_id, coalesce(i.number, 0)::bigint AS invoice_number,
       cb.name AS created_by_name, ub.name AS updated_by_name, e.created_at, e.updated_at
FROM finance_entries e
JOIN finance_categories c ON c.id = e.category_id
JOIN users cb ON cb.id = e.created_by
JOIN users ub ON ub.id = e.updated_by
LEFT JOIN invoices i ON i.id = e.invoice_id
WHERE e.entry_date BETWEEN sqlc.arg('from_day')::text::date AND sqlc.arg('to_day')::text::date
  AND (sqlc.narg('kind')::text IS NULL OR c.kind = sqlc.narg('kind'))
  AND (sqlc.narg('category_id')::uuid IS NULL OR e.category_id = sqlc.narg('category_id'))
  AND (sqlc.narg('q')::text IS NULL OR e.description ILIKE '%' || sqlc.narg('q') || '%')
ORDER BY e.entry_date DESC, e.created_at DESC;

-- name: GetFinanceEntry :one
-- kind and the invoice say who may change the line: an expense that came from an approved
-- invoice is the CEO's to correct, nobody else's.
SELECT e.id, e.category_id, c.kind, e.amount::text AS amount, e.currency, e.rate_to_bdt::text AS rate_to_bdt,
       e.entry_date::text AS entry_date, e.description,
       e.invoice_id, coalesce(i.number, 0)::bigint AS invoice_number
FROM finance_entries e
JOIN finance_categories c ON c.id = e.category_id
LEFT JOIN invoices i ON i.id = e.invoice_id
WHERE e.id = $1;

-- name: CreateFinanceEntry :one
INSERT INTO finance_entries (category_id, amount, currency, rate_to_bdt, entry_date, description, created_by, updated_by)
VALUES (sqlc.arg('category_id'), sqlc.arg('amount')::text::numeric, sqlc.arg('currency'),
        sqlc.arg('rate_to_bdt')::text::numeric, sqlc.arg('entry_date')::text::date, sqlc.arg('description'),
        sqlc.arg('actor_id'), sqlc.arg('actor_id'))
RETURNING id;

-- name: UpdateFinanceEntry :exec
UPDATE finance_entries
SET category_id = sqlc.arg('category_id'),
    amount      = sqlc.arg('amount')::text::numeric,
    currency    = sqlc.arg('currency'),
    rate_to_bdt = sqlc.arg('rate_to_bdt')::text::numeric,
    entry_date  = sqlc.arg('entry_date')::text::date,
    description = sqlc.arg('description'),
    updated_by  = sqlc.arg('actor_id'),
    updated_at  = now()
WHERE id = sqlc.arg('id');

-- name: DeleteFinanceEntry :exec
DELETE FROM finance_entries WHERE id = $1;

-- name: FinanceRateOn :one
-- The BDT rate for a currency on a day: 1 for BDT, NULL (empty) when none is set.
SELECT coalesce(finance_rate(sqlc.arg('currency')::text, sqlc.arg('on_day')::text::date)::text, '')::text AS rate;

-- name: ListFinanceRates :many
SELECT r.id, r.currency, r.rate::text AS rate, r.effective_from::text AS effective_from,
       u.name AS created_by_name, r.created_at
FROM finance_rates r
JOIN users u ON u.id = r.created_by
ORDER BY r.currency, r.effective_from DESC;

-- name: CreateFinanceRate :one
INSERT INTO finance_rates (currency, rate, effective_from, created_by)
VALUES (sqlc.arg('currency'), sqlc.arg('rate')::text::numeric, sqlc.arg('effective_from')::text::date, sqlc.arg('created_by'))
RETURNING id;

-- name: GetFinanceRate :one
SELECT id, currency, effective_from::text AS effective_from FROM finance_rates WHERE id = $1;

-- name: DeleteFinanceRate :exec
DELETE FROM finance_rates WHERE id = $1;

-- name: FinanceMonthly :many
-- One row per month in the range, all in BDT:
--   ledger income and expense (each entry at its own rate);
--   project earnings: projects completed that month, net of the marketplace fee, the same
--     rule as CompanyRevenue (dashboard.sql), converted at the rate for the month's last day;
--   payroll: the pay HR or the CEO recorded for that month (salary, bonuses, deductions),
--     each at the rate kept on it. Nothing is counted for a month until it is recorded.
-- Project earnings in a currency with no rate are left out and named in earnings_missing_rates.
-- The month count is an integer so the planner knows how many rows there are: a date
-- series is estimated at 1000, which makes Postgres JIT-compile it for over a second.
WITH months AS (
    SELECT (sqlc.arg('from_month')::text::date + make_interval(months => i))::date AS month_start,
           (sqlc.arg('from_month')::text::date + make_interval(months => i + 1) - interval '1 day')::date AS month_end
    FROM generate_series(0, sqlc.arg('month_count')::int - 1) i
),
ledger AS (
    SELECT date_trunc('month', e.entry_date)::date AS month_start,
           sum(e.amount_bdt) FILTER (WHERE c.kind = 'income')  AS income,
           sum(e.amount_bdt) FILTER (WHERE c.kind = 'expense') AS expense,
           count(*) AS entries
    FROM finance_entries e
    JOIN finance_categories c ON c.id = e.category_id
    GROUP BY 1
),
earned AS (
    SELECT date_trunc('month', p.completed_at AT TIME ZONE org_timezone())::date AS month_start,
           btrim(p.currency) AS currency,
           sum(round(p.agreed_price * (100 - p.platform_fee_percent) / 100, 2)) AS net,
           count(*) AS projects
    FROM projects p
    WHERE p.status = 'completed' AND p.agreed_price IS NOT NULL AND p.completed_at IS NOT NULL
    GROUP BY 1, 2
),
earnings AS (
    SELECT m.month_start,
           sum(round(er.net * finance_rate(er.currency, m.month_end), 2)) AS amount,
           sum(er.projects) AS projects,
           array_agg(DISTINCT er.currency) FILTER (WHERE finance_rate(er.currency, m.month_end) IS NULL) AS missing
    FROM months m
    JOIN earned er ON er.month_start = m.month_start
    GROUP BY 1
),
payroll AS (
    SELECT pp.month AS month_start, sum(pp.net_bdt) AS amount, count(*) AS people
    FROM payroll_payments pp
    GROUP BY 1
)
SELECT to_char(m.month_start, 'YYYY-MM')::text AS month,
       coalesce(l.income, 0)::text   AS ledger_income,
       coalesce(l.expense, 0)::text  AS ledger_expense,
       coalesce(er.amount, 0)::text  AS project_earnings,
       coalesce(pa.amount, 0)::text  AS payroll,
       (coalesce(l.income, 0) + coalesce(er.amount, 0))::text  AS total_income,
       (coalesce(l.expense, 0) + coalesce(pa.amount, 0))::text AS total_expense,
       (coalesce(l.income, 0) + coalesce(er.amount, 0) - coalesce(l.expense, 0) - coalesce(pa.amount, 0))::text AS profit,
       coalesce(l.entries, 0)::bigint      AS entries,
       coalesce(er.projects, 0)::bigint    AS projects,
       coalesce(pa.people, 0)::bigint      AS paid_people,
       coalesce(er.missing, '{}')::text[]  AS earnings_missing_rates
FROM months m
LEFT JOIN ledger l    ON l.month_start = m.month_start
LEFT JOIN earnings er ON er.month_start = m.month_start
LEFT JOIN payroll pa  ON pa.month_start = m.month_start
ORDER BY m.month_start;

-- name: FinanceByCategory :many
-- Ledger totals per category for a date range, largest first within each kind.
SELECT c.id, c.name::text AS name, c.kind, sum(e.amount_bdt)::text AS total, count(*) AS entries
FROM finance_entries e
JOIN finance_categories c ON c.id = e.category_id
WHERE e.entry_date BETWEEN sqlc.arg('from_day')::text::date AND sqlc.arg('to_day')::text::date
GROUP BY c.id, c.name, c.kind
ORDER BY c.kind DESC, sum(e.amount_bdt) DESC;

-- ── Payroll (HR + CEO) ─────────────────────────────────────

-- name: PayrollPeople :many
-- Everyone employed in the month (joined by its last day, active or deactivated on or after
-- its first day) with the fixed salary in effect on its last day.
SELECT u.id AS user_id, u.name, u.is_active, e.designation, coalesce(dp.name::text, '')::text AS department_name,
       s.amount::text AS salary_amount, s.currency AS salary_currency
FROM users u
JOIN employees e ON e.user_id = u.id
LEFT JOIN departments dp ON dp.id = e.department_id
LEFT JOIN user_departures d ON d.user_id = u.id
JOIN LATERAL (
    SELECT sa.amount, sa.currency FROM salaries sa
    WHERE sa.user_id = u.id AND sa.effective_from <= sqlc.arg('month_end')::text::date
    ORDER BY sa.effective_from DESC LIMIT 1
) s ON true
WHERE (e.join_date IS NULL OR e.join_date <= sqlc.arg('month_end')::text::date)
  AND (u.is_active OR (d.left_at AT TIME ZONE org_timezone())::date >= sqlc.arg('month_start')::text::date)
ORDER BY u.name;

-- name: ListPayrollPayments :many
SELECT p.id, p.user_id, u.name AS user_name, u.is_active,
       p.base_amount::text AS base_amount, p.currency, p.rate_to_bdt::text AS rate_to_bdt,
       p.net_amount::text AS net_amount, p.net_bdt::text AS net_bdt, p.paid_on::text AS paid_on, p.note,
       ub.name AS updated_by_name, p.updated_at
FROM payroll_payments p
JOIN users u ON u.id = p.user_id
JOIN users ub ON ub.id = p.updated_by
WHERE p.month = sqlc.arg('month')::text::date
ORDER BY u.name;

-- name: ListPayrollAdjustments :many
SELECT a.payment_id, a.kind, a.reason, a.amount::text AS amount
FROM payroll_adjustments a
JOIN payroll_payments p ON p.id = a.payment_id
WHERE p.month = sqlc.arg('month')::text::date
ORDER BY a.payment_id, a.position;

-- name: FindPayrollPayment :one
SELECT id FROM payroll_payments WHERE user_id = sqlc.arg('user_id') AND month = sqlc.arg('month')::text::date;

-- name: GetPayrollPayment :one
SELECT id, user_id, to_char(month, 'YYYY-MM')::text AS month FROM payroll_payments WHERE id = $1;

-- name: CreatePayrollPayment :one
INSERT INTO payroll_payments (user_id, month, base_amount, currency, rate_to_bdt, net_amount, paid_on, note, created_by, updated_by)
VALUES (sqlc.arg('user_id'), sqlc.arg('month')::text::date, sqlc.arg('base_amount')::text::numeric, sqlc.arg('currency'),
        sqlc.arg('rate_to_bdt')::text::numeric, sqlc.arg('net_amount')::text::numeric, sqlc.arg('paid_on')::text::date,
        sqlc.arg('note'), sqlc.arg('actor_id'), sqlc.arg('actor_id'))
RETURNING id;

-- name: UpdatePayrollPayment :exec
UPDATE payroll_payments
SET base_amount = sqlc.arg('base_amount')::text::numeric,
    currency    = sqlc.arg('currency'),
    rate_to_bdt = sqlc.arg('rate_to_bdt')::text::numeric,
    net_amount  = sqlc.arg('net_amount')::text::numeric,
    paid_on     = sqlc.arg('paid_on')::text::date,
    note        = sqlc.arg('note'),
    updated_by  = sqlc.arg('actor_id'),
    updated_at  = now()
WHERE id = sqlc.arg('id');

-- name: DeletePayrollAdjustments :exec
DELETE FROM payroll_adjustments WHERE payment_id = $1;

-- name: CreatePayrollAdjustment :exec
INSERT INTO payroll_adjustments (payment_id, kind, reason, amount, position)
VALUES (sqlc.arg('payment_id'), sqlc.arg('kind'), sqlc.arg('reason'), sqlc.arg('amount')::text::numeric, sqlc.arg('position'));

-- name: DeletePayrollPayment :exec
DELETE FROM payroll_payments WHERE id = $1;

-- ── Expense invoices (raised, approved by HR, then the CEO) ─

-- name: ListInvoices :many
SELECT * FROM invoice_details
WHERE (sqlc.narg('statuses')::text[] IS NULL OR status = ANY (sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('requested_by')::uuid IS NULL OR requested_by = sqlc.narg('requested_by'))
  AND (sqlc.narg('from_day')::text IS NULL OR expense_date::date >= sqlc.narg('from_day')::text::date)
  AND (sqlc.narg('to_day')::text IS NULL OR expense_date::date <= sqlc.narg('to_day')::text::date)
  -- The search covers what the invoice is for and what is on it.
  AND (sqlc.narg('q')::text IS NULL OR description ILIKE '%' || sqlc.narg('q') || '%'
       OR EXISTS (SELECT 1 FROM invoice_items it WHERE it.invoice_id = invoice_details.id
                    AND it.description ILIKE '%' || sqlc.narg('q') || '%'))
ORDER BY expense_date::date DESC, number DESC
LIMIT 500;

-- name: GetInvoiceDetail :one
SELECT * FROM invoice_details WHERE id = $1;

-- name: GetInvoiceForUpdate :one
SELECT id, number, status, requested_by, currency, expense_date::text AS expense_date, description
FROM invoices WHERE id = $1 FOR UPDATE;

-- name: ListInvoiceItems :many
-- The lines of one or more invoices, in the order they were typed.
SELECT it.id, it.invoice_id, it.position, it.category_id, c.name::text AS category_name,
       it.description, it.quantity::text AS quantity, it.unit_price::text AS unit_price, it.amount::text AS amount
FROM invoice_items it
JOIN finance_categories c ON c.id = it.category_id
WHERE it.invoice_id = ANY (sqlc.arg('invoice_ids')::uuid[])
ORDER BY it.invoice_id, it.position;

-- name: CreateInvoice :one
-- hr_reviewed_by / ceo_reviewed_by are set when the raiser's own approval counts for that
-- step: HR raising one skips the HR step, the CEO's is approved on the spot.
INSERT INTO invoices (currency, rate_to_bdt, expense_date, description, status,
                      requested_by, hr_reviewed_by, hr_reviewed_at, ceo_reviewed_by, ceo_reviewed_at)
VALUES (sqlc.arg('currency'), sqlc.arg('rate_to_bdt')::text::numeric,
        sqlc.arg('expense_date')::text::date, sqlc.arg('description'),
        sqlc.arg('status'), sqlc.arg('requested_by'),
        sqlc.narg('hr_reviewed_by'),
        CASE WHEN sqlc.narg('hr_reviewed_by')::uuid IS NULL THEN NULL ELSE now() END,
        sqlc.narg('ceo_reviewed_by'),
        CASE WHEN sqlc.narg('ceo_reviewed_by')::uuid IS NULL THEN NULL ELSE now() END)
RETURNING id, number;

-- name: UpdateInvoice :exec
UPDATE invoices
SET currency     = sqlc.arg('currency'),
    rate_to_bdt  = sqlc.arg('rate_to_bdt')::text::numeric,
    expense_date = sqlc.arg('expense_date')::text::date,
    description  = sqlc.arg('description'),
    updated_at   = now()
WHERE id = sqlc.arg('id');

-- name: CreateInvoiceItem :exec
INSERT INTO invoice_items (invoice_id, position, category_id, description, quantity, unit_price)
VALUES (sqlc.arg('invoice_id'), sqlc.arg('position'), sqlc.arg('category_id'), sqlc.arg('description'),
        sqlc.arg('quantity')::text::numeric, sqlc.arg('unit_price')::text::numeric);

-- name: DeleteInvoiceItems :exec
DELETE FROM invoice_items WHERE invoice_id = $1;

-- name: SetInvoiceHRReview :exec
-- HR's decision: 'awaiting_ceo' hands it on, 'rejected' ends it.
UPDATE invoices
SET status = sqlc.arg('status'), hr_reviewed_by = sqlc.arg('reviewed_by'), hr_reviewed_at = now(),
    hr_note = sqlc.arg('note'), updated_at = now()
WHERE id = sqlc.arg('id');

-- name: SetInvoiceCEOReview :exec
UPDATE invoices
SET status = sqlc.arg('status'), ceo_reviewed_by = sqlc.arg('reviewed_by'), ceo_reviewed_at = now(),
    ceo_note = sqlc.arg('note'), updated_at = now()
WHERE id = sqlc.arg('id');

-- name: CancelInvoice :exec
UPDATE invoices SET status = 'cancelled', updated_at = now() WHERE id = $1;

-- name: InvoiceTotalsByCategory :many
-- What an approved invoice owes each category, and the lines it covers there: one ledger
-- entry per category, so a mixed purchase still counts under the right headings.
SELECT it.category_id, sum(it.amount)::text AS amount,
       string_agg(it.description, ', ' ORDER BY it.position)::text AS lines
FROM invoice_items it
WHERE it.invoice_id = sqlc.arg('invoice_id')
GROUP BY it.category_id;

-- name: PostInvoiceLine :one
-- One ledger line of an approved invoice: the amount and rate come from the invoice itself,
-- so what is booked is exactly what was approved.
INSERT INTO finance_entries (category_id, amount, currency, rate_to_bdt, entry_date, description,
                             created_by, updated_by, invoice_id)
SELECT sqlc.arg('category_id'), sqlc.arg('amount')::text::numeric, i.currency, i.rate_to_bdt,
       i.expense_date, sqlc.arg('description'), i.requested_by, sqlc.arg('actor_id'), i.id
FROM invoices i WHERE i.id = sqlc.arg('invoice_id')
RETURNING id;

-- name: CountInvoicesWaiting :one
-- What is waiting on this person: HR sees the HR step, the CEO the final one; everyone sees
-- how many of their own are still open.
SELECT
    count(*) FILTER (WHERE status = 'pending')                                      AS pending_hr,
    count(*) FILTER (WHERE status = 'awaiting_ceo')                                 AS awaiting_ceo,
    count(*) FILTER (WHERE requested_by = sqlc.arg('viewer')
                       AND status IN ('pending', 'awaiting_ceo'))                   AS mine_open
FROM invoices;
