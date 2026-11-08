-- Investor Finance Forum queries (Phase 3).
-- Money is returned as text (numeric::text) like the finance and milestone
-- queries, so JSON carries exact decimals and the frontend formats them.

-- name: ListInvestors :many
SELECT i.id, i.user_id, i.name, i.email, i.phone, i.join_date::text AS join_date,
       i.status, i.notes, i.created_at, i.updated_at,
       coalesce((SELECT sum(amount_bdt) FROM investments WHERE investor_id = i.id), 0)::text AS invested_bdt,
       coalesce((SELECT sum(amount_bdt) FROM profit_distributions WHERE investor_id = i.id), 0)::text AS earned_bdt,
       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE investor_id = i.id AND status = 'paid'), 0)::text AS withdrawn_bdt,
       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE investor_id = i.id AND status IN ('pending', 'approved')), 0)::text AS pending_withdrawals_bdt,
       coalesce((SELECT share_percent FROM investor_shares WHERE investor_id = i.id AND effective_from <= CURRENT_DATE ORDER BY effective_from DESC, created_at DESC LIMIT 1), 0)::text AS share_percent
FROM investors i
ORDER BY i.status, i.name;

-- name: GetInvestor :one
SELECT i.id, i.user_id, i.name, i.email, i.phone, i.join_date::text AS join_date,
       i.status, i.notes, i.created_at, i.updated_at,
       coalesce((SELECT sum(amount_bdt) FROM investments WHERE investor_id = i.id), 0)::text AS invested_bdt,
       coalesce((SELECT sum(amount_bdt) FROM profit_distributions WHERE investor_id = i.id), 0)::text AS earned_bdt,
       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE investor_id = i.id AND status = 'paid'), 0)::text AS withdrawn_bdt,
       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE investor_id = i.id AND status IN ('pending', 'approved')), 0)::text AS pending_withdrawals_bdt,
       coalesce((SELECT share_percent FROM investor_shares WHERE investor_id = i.id AND effective_from <= CURRENT_DATE ORDER BY effective_from DESC, created_at DESC LIMIT 1), 0)::text AS share_percent
FROM investors i
WHERE i.id = $1;

-- name: GetInvestorByUserID :one
SELECT i.id, i.user_id, i.name, i.email, i.phone, i.join_date::text AS join_date,
       i.status, i.notes, i.created_at, i.updated_at,
       coalesce((SELECT sum(amount_bdt) FROM investments WHERE investor_id = i.id), 0)::text AS invested_bdt,
       coalesce((SELECT sum(amount_bdt) FROM profit_distributions WHERE investor_id = i.id), 0)::text AS earned_bdt,
       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE investor_id = i.id AND status = 'paid'), 0)::text AS withdrawn_bdt,
       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE investor_id = i.id AND status IN ('pending', 'approved')), 0)::text AS pending_withdrawals_bdt,
       coalesce((SELECT share_percent FROM investor_shares WHERE investor_id = i.id AND effective_from <= CURRENT_DATE ORDER BY effective_from DESC, created_at DESC LIMIT 1), 0)::text AS share_percent
FROM investors i
WHERE i.user_id = $1;

-- name: InvestorTotals :one
SELECT coalesce((SELECT sum(amount_bdt) FROM investments WHERE investments.investor_id = $1), 0)::text AS invested_bdt,
       coalesce((SELECT sum(amount_bdt) FROM profit_distributions WHERE profit_distributions.investor_id = $1), 0)::text AS earned_bdt,
       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE withdrawals.investor_id = $1 AND status = 'paid'), 0)::text AS withdrawn_bdt,
       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE withdrawals.investor_id = $1 AND status IN ('pending', 'approved')), 0)::text AS pending_withdrawals_bdt;

-- name: CreateInvestor :one
INSERT INTO investors (user_id, name, email, phone, join_date, notes)
VALUES (sqlc.arg('user_id'), sqlc.arg('name'), sqlc.arg('email'), sqlc.arg('phone'),
        sqlc.arg('join_date')::text::date, sqlc.arg('notes'))
RETURNING id;

-- name: UpdateInvestor :exec
UPDATE investors
SET user_id = sqlc.arg('user_id'), name = sqlc.arg('name'), email = sqlc.arg('email'),
    phone = sqlc.arg('phone'), join_date = sqlc.arg('join_date')::text::date,
    status = sqlc.arg('status'), notes = sqlc.arg('notes'), updated_at = now()
WHERE id = sqlc.arg('id');

-- name: CreateInvestment :one
INSERT INTO investments (investor_id, amount, currency, rate_to_bdt, method, invested_at, note, created_by)
VALUES (sqlc.arg('investor_id'), sqlc.arg('amount')::text::numeric, sqlc.arg('currency'),
        sqlc.arg('rate_to_bdt')::text::numeric, sqlc.arg('method'),
        sqlc.arg('invested_at')::text::date, sqlc.arg('note'), sqlc.arg('created_by'))
RETURNING id;

-- name: ListInvestmentsByInvestor :many
SELECT id, investor_id, amount::text AS amount, currency, rate_to_bdt::text AS rate_to_bdt,
       amount_bdt::text AS amount_bdt, method, invested_at::text AS invested_at, note,
       created_by, created_at
FROM investments
WHERE investor_id = $1
ORDER BY invested_at DESC, created_at DESC;

-- name: GetLatestShare :one
SELECT share_percent::text AS share_percent, effective_from::text AS effective_from, created_at
FROM investor_shares
WHERE investor_id = $1 AND effective_from <= CURRENT_DATE
ORDER BY effective_from DESC, created_at DESC LIMIT 1;

-- name: ListLatestShares :many
-- The current share of every active investor: one row each, for validation
-- (shares + reserve must stay <= 100) and for distribution.
SELECT DISTINCT ON (s.investor_id) s.investor_id,
       s.share_percent::text AS share_percent, s.effective_from::text AS effective_from
FROM investor_shares s
JOIN investors i ON i.id = s.investor_id
WHERE i.status = 'active' AND s.effective_from <= CURRENT_DATE
ORDER BY s.investor_id, s.effective_from DESC, s.created_at DESC;

-- name: CreateShare :one
INSERT INTO investor_shares (investor_id, share_percent, effective_from, created_by)
VALUES (sqlc.arg('investor_id'), sqlc.arg('share_percent')::text::numeric,
        sqlc.arg('effective_from')::text::date, sqlc.arg('created_by'))
RETURNING id;

-- name: ListSharesByInvestor :many
SELECT s.id, s.investor_id, s.share_percent::text AS share_percent,
       s.effective_from::text AS effective_from, u.name AS created_by_name, s.created_at
FROM investor_shares s
LEFT JOIN users u ON u.id = s.created_by
WHERE s.investor_id = $1
ORDER BY s.effective_from DESC, s.created_at DESC;

-- name: GetProfitSettings :one
SELECT id, reserve_percent::text AS reserve_percent, updated_by, updated_at
FROM profit_settings WHERE id = 1;

-- name: UpdateProfitSettings :exec
UPDATE profit_settings
SET reserve_percent = sqlc.arg('reserve_percent')::text::numeric,
    updated_by = sqlc.arg('updated_by'), updated_at = now()
WHERE id = 1;

-- name: CreateDistribution :one
-- Idempotent: a retry for the same income event returns no row (sql.ErrNoRows),
-- never a second payout.
INSERT INTO profit_distributions (source_type, source_id, investor_id, amount, currency, rate_to_bdt, amount_bdt, share_percent, note)
VALUES (sqlc.arg('source_type'), sqlc.arg('source_id'), sqlc.arg('investor_id'),
        sqlc.arg('amount')::text::numeric, sqlc.arg('currency'), sqlc.arg('rate_to_bdt')::text::numeric,
        sqlc.arg('amount_bdt')::text::numeric, sqlc.arg('share_percent')::text::numeric, sqlc.arg('note'))
ON CONFLICT (source_type, source_id, investor_id) DO NOTHING
RETURNING id;

-- name: CreateReserveDistribution :one
-- The reserve's cut: investor_id NULL, deduped by the partial unique index.
INSERT INTO profit_distributions (source_type, source_id, investor_id, amount, currency, rate_to_bdt, amount_bdt, share_percent, note)
VALUES (sqlc.arg('source_type'), sqlc.arg('source_id'), NULL,
        sqlc.arg('amount')::text::numeric, sqlc.arg('currency'), sqlc.arg('rate_to_bdt')::text::numeric,
        sqlc.arg('amount_bdt')::text::numeric, sqlc.arg('share_percent')::text::numeric, sqlc.arg('note'))
ON CONFLICT (source_type, source_id) WHERE investor_id IS NULL DO NOTHING
RETURNING id;

-- name: ListDistributionsByInvestor :many
SELECT id, source_type, source_id, investor_id, amount::text AS amount, currency,
       rate_to_bdt::text AS rate_to_bdt, amount_bdt::text AS amount_bdt,
       share_percent::text AS share_percent, distributed_at, note
FROM profit_distributions
WHERE investor_id = $1
ORDER BY distributed_at DESC;

-- name: ListDistributionsByMonth :many
-- One row per month an investor earned, for the "income breakdown" chart.
SELECT to_char(distributed_at, 'YYYY-MM') AS month,
       sum(amount_bdt)::text AS earned_bdt
FROM profit_distributions
WHERE investor_id = $1
GROUP BY to_char(distributed_at, 'YYYY-MM')
ORDER BY month DESC;

-- name: CreateWithdrawal :one
INSERT INTO withdrawals (investor_id, amount, currency, rate_to_bdt, method, note, requested_by)
VALUES (sqlc.arg('investor_id'), sqlc.arg('amount')::text::numeric, sqlc.arg('currency'),
        sqlc.arg('rate_to_bdt')::text::numeric, sqlc.arg('method'), sqlc.arg('note'), sqlc.arg('requested_by'))
RETURNING id;

-- name: GetWithdrawal :one
SELECT w.id, w.investor_id, i.name AS investor_name, w.amount::text AS amount, w.currency,
       w.rate_to_bdt::text AS rate_to_bdt, w.amount_bdt::text AS amount_bdt, w.method, w.status,
       w.requested_at, w.requested_by, w.reviewed_by, w.reviewed_at, w.paid_at, w.note,
       w.created_at, w.updated_at
FROM withdrawals w
JOIN investors i ON i.id = w.investor_id
WHERE w.id = $1;

-- name: ListWithdrawalsByInvestor :many
SELECT w.id, w.investor_id, i.name AS investor_name, w.amount::text AS amount, w.currency,
       w.rate_to_bdt::text AS rate_to_bdt, w.amount_bdt::text AS amount_bdt, w.method, w.status,
       w.requested_at, w.requested_by, w.reviewed_by, w.reviewed_at, w.paid_at, w.note,
       w.created_at, w.updated_at
FROM withdrawals w
JOIN investors i ON i.id = w.investor_id
WHERE w.investor_id = $1
ORDER BY w.requested_at DESC;

-- name: ListAllWithdrawals :many
SELECT w.id, w.investor_id, i.name AS investor_name, w.amount::text AS amount, w.currency,
       w.rate_to_bdt::text AS rate_to_bdt, w.amount_bdt::text AS amount_bdt, w.method, w.status,
       w.requested_at, w.requested_by, w.reviewed_by, w.reviewed_at, w.paid_at, w.note,
       w.created_at, w.updated_at
FROM withdrawals w
JOIN investors i ON i.id = w.investor_id
WHERE (sqlc.arg('status')::text = '' OR w.status = sqlc.arg('status'))
ORDER BY w.requested_at DESC;

-- name: UpdateWithdrawalStatus :exec
-- The caller passes the exact status transition; reviewed/paid stamps are set
-- by the handler through the nullable args (NULL leaves the column alone).
UPDATE withdrawals
SET status = sqlc.arg('status'),
    reviewed_by = coalesce(sqlc.narg('reviewed_by'), reviewed_by),
    reviewed_at = coalesce(sqlc.narg('reviewed_at'), reviewed_at),
    paid_at = coalesce(sqlc.narg('paid_at'), paid_at),
    updated_at = now()
WHERE id = sqlc.arg('id');

-- name: CreateInvestorExpense :one
INSERT INTO investor_expenses (category, description, amount, currency, rate_to_bdt, expense_date, receipt_url, added_by)
VALUES (sqlc.arg('category'), sqlc.arg('description'), sqlc.arg('amount')::text::numeric,
        sqlc.arg('currency'), sqlc.arg('rate_to_bdt')::text::numeric,
        sqlc.arg('expense_date')::text::date, sqlc.arg('receipt_url'), sqlc.arg('added_by'))
RETURNING id;

-- name: ListInvestorExpenses :many
SELECT e.id, e.category, e.description, e.amount::text AS amount, e.currency,
       e.rate_to_bdt::text AS rate_to_bdt, e.amount_bdt::text AS amount_bdt,
       e.expense_date::text AS expense_date, e.receipt_url, u.name AS added_by_name, e.created_at
FROM investor_expenses e
LEFT JOIN users u ON u.id = e.added_by
WHERE (sqlc.arg('category')::text = '' OR e.category = sqlc.arg('category'))
ORDER BY e.expense_date DESC, e.created_at DESC;

-- name: DeleteInvestorExpense :exec
DELETE FROM investor_expenses WHERE id = $1;

-- name: InvestorExpenseTotals :many
SELECT category, sum(amount_bdt)::text AS total_bdt, count(*)::bigint AS count
FROM investor_expenses
GROUP BY category
ORDER BY sum(amount_bdt) DESC;

-- name: CompanyTotals :one
-- The company account, all in BDT: ledger income, investor-tracked expenses,
-- what was distributed to investors, what sits in the reserve, what was paid out.
SELECT coalesce((SELECT sum(fe.amount_bdt) FROM finance_entries fe JOIN finance_categories fc ON fc.id = fe.category_id WHERE fc.kind = 'income'), 0)::text AS total_income_bdt,
       coalesce((SELECT sum(amount_bdt) FROM investor_expenses), 0)::text AS total_expenses_bdt,
       coalesce((SELECT sum(amount_bdt) FROM profit_distributions WHERE investor_id IS NOT NULL), 0)::text AS total_distributed_bdt,
       coalesce((SELECT sum(amount_bdt) FROM profit_distributions WHERE investor_id IS NULL), 0)::text AS reserve_bdt,
       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE status = 'paid'), 0)::text AS total_withdrawn_bdt;
