-- B2B project queries (Phase 5): contracts, retainers, milestone teams.
--
-- Reads go through contract_details / retainer_details (migration 00047):
-- the nullable dates are cast to text in the views, so the sqlc overrides on
-- the view columns give *string fields and a NULL date scans cleanly.

-- name: ListContracts :many
-- Every contract with its client and (optional) project. Pass '' for all statuses.
SELECT id, contract_number, client_id, project_id, title,
       value::text AS value, currency, start_date, end_date, terms, document_url, status,
       signed_at, created_by, created_at, updated_at,
       client_name, client_code, project_name
FROM contract_details
WHERE (sqlc.arg('status') = '' OR status = sqlc.arg('status'))
ORDER BY created_at DESC;

-- name: GetContract :one
SELECT id, contract_number, client_id, project_id, title,
       value::text AS value, currency, start_date, end_date, terms, document_url, status,
       signed_at, created_by, created_at, updated_at,
       client_name, client_code, project_name
FROM contract_details
WHERE id = $1;

-- name: CreateContract :one
-- contract_number comes from the sequence (CTR-0001, CTR-0002, …).
INSERT INTO contracts (client_id, project_id, title, value, currency, start_date, end_date, terms, document_url, created_by)
VALUES (sqlc.arg('client_id'), sqlc.arg('project_id'), sqlc.arg('title'),
        sqlc.arg('value')::text::numeric, sqlc.arg('currency'),
        sqlc.arg('start_date')::text::date, sqlc.narg('end_date')::text::date,
        sqlc.arg('terms'), sqlc.arg('document_url'), sqlc.arg('created_by'))
RETURNING id, contract_number;

-- name: UpdateContract :exec
UPDATE contracts
SET title = sqlc.arg('title'), value = sqlc.arg('value')::text::numeric,
    currency = sqlc.arg('currency'), start_date = sqlc.arg('start_date')::text::date,
    end_date = sqlc.narg('end_date')::text::date, terms = sqlc.arg('terms'),
    document_url = sqlc.arg('document_url'), status = sqlc.arg('status'),
    signed_at = sqlc.arg('signed_at'), updated_at = now()
WHERE id = sqlc.arg('id');

-- name: SetContractStatus :exec
UPDATE contracts SET status = $2, updated_at = now() WHERE id = $1;

-- name: ListExpiringContracts :many
-- Active contracts ending within the next $1 days (B2B dashboard).
SELECT id, contract_number, client_id, title,
       value::text AS value, currency, start_date, end_date, status,
       client_name
FROM contract_details
WHERE status = 'active' AND end_date IS NOT NULL
  AND end_date::date <= CURRENT_DATE + ($1::int)
ORDER BY end_date ASC;

-- name: ListRetainers :many
SELECT id, client_id, title, monthly_fee::text AS monthly_fee, currency,
       start_date, billing_day, status, auto_renew, next_billing_date,
       created_by, created_at, updated_at,
       client_name, client_code
FROM retainer_details
WHERE (sqlc.arg('status') = '' OR status = sqlc.arg('status'))
ORDER BY created_at DESC;

-- name: GetRetainer :one
SELECT id, client_id, title, monthly_fee::text AS monthly_fee, currency,
       start_date, billing_day, status, auto_renew, next_billing_date,
       created_by, created_at, updated_at,
       client_name, client_code
FROM retainer_details
WHERE id = $1;

-- name: CreateRetainer :one
-- next_billing_date is computed by the handler (first billing day on/after start).
INSERT INTO retainers (client_id, title, monthly_fee, currency, start_date, billing_day, auto_renew, next_billing_date, created_by)
VALUES (sqlc.arg('client_id'), sqlc.arg('title'), sqlc.arg('monthly_fee')::text::numeric,
        sqlc.arg('currency'), sqlc.arg('start_date')::text::date, sqlc.arg('billing_day'),
        sqlc.arg('auto_renew'), sqlc.arg('next_billing_date')::text::date, sqlc.arg('created_by'))
RETURNING id;

-- name: UpdateRetainer :exec
UPDATE retainers
SET title = sqlc.arg('title'), monthly_fee = sqlc.arg('monthly_fee')::text::numeric,
    currency = sqlc.arg('currency'), billing_day = sqlc.arg('billing_day'),
    status = sqlc.arg('status'), auto_renew = sqlc.arg('auto_renew'),
    next_billing_date = sqlc.arg('next_billing_date')::text::date, updated_at = now()
WHERE id = sqlc.arg('id');

-- name: ListDueRetainers :many
-- Active retainers whose billing date has arrived, with the client to bill.
SELECT id, client_id, title, monthly_fee::text AS monthly_fee, currency,
       start_date, billing_day, status, auto_renew, next_billing_date,
       created_by, created_at, updated_at,
       client_name, client_code, client_email
FROM retainer_details
WHERE status = 'active' AND next_billing_date IS NOT NULL
  AND next_billing_date::date <= CURRENT_DATE
ORDER BY next_billing_date ASC;

-- name: ListRetainersDueSoon :many
-- Active retainers billing within the next $1 days (B2B dashboard).
SELECT id, client_id, title, monthly_fee::text AS monthly_fee, currency,
       start_date, billing_day, status, auto_renew, next_billing_date,
       created_by, created_at, updated_at,
       client_name, client_code
FROM retainer_details
WHERE status = 'active' AND next_billing_date IS NOT NULL
  AND next_billing_date::date <= CURRENT_DATE + ($1::int)
ORDER BY next_billing_date ASC;

-- name: SetMilestoneTeam :exec
-- Assign a team (and an SLA response time) to one milestone of one project.
UPDATE payment_milestones
SET team_id = sqlc.arg('team_id'), sla_response_hours = sqlc.arg('sla_response_hours'),
    updated_at = now()
WHERE id = sqlc.arg('id') AND project_id = sqlc.arg('project_id');
