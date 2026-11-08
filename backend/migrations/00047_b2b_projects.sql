-- +goose Up
-- +goose StatementBegin
-- B2B project features (2026-11-07, Phase 5): contracts, retainers, per-
-- milestone teams + SLA, and SLA breach tracking.
--
-- The existing payment_milestones (00044) already serve as project milestones,
-- so no new milestone table: team_id + sla_response_hours are added to it.
-- Client health is computed on the fly by the B2B report, not stored.

CREATE SEQUENCE IF NOT EXISTS contract_number_seq START WITH 1;

CREATE TABLE IF NOT EXISTS contracts (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    contract_number  text NOT NULL UNIQUE DEFAULT ('CTR-' || lpad(nextval('contract_number_seq')::text, 4, '0')),
    client_id        uuid NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    project_id       uuid REFERENCES projects (id) ON DELETE SET NULL,
    title            text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 300),
    value            numeric(16,2) NOT NULL CHECK (value >= 0),
    currency         text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    start_date       date NOT NULL,
    end_date         date CHECK (end_date IS NULL OR end_date >= start_date),
    terms            text NOT NULL DEFAULT '',
    document_url     text,
    status           text NOT NULL DEFAULT 'draft'
                     CHECK (status IN ('draft', 'active', 'completed', 'terminated', 'expired')),
    signed_at        timestamptz,
    created_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS contracts_client_idx ON contracts (client_id, status);
CREATE INDEX IF NOT EXISTS contracts_number_idx ON contracts (contract_number);
CREATE INDEX IF NOT EXISTS contracts_end_idx ON contracts (end_date) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS retainers (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id         uuid NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    title             text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 300),
    monthly_fee       numeric(16,2) NOT NULL CHECK (monthly_fee > 0),
    currency          text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    start_date        date NOT NULL,
    billing_day       int NOT NULL DEFAULT 1 CHECK (billing_day BETWEEN 1 AND 28),
    status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'cancelled')),
    auto_renew        boolean NOT NULL DEFAULT true,
    next_billing_date date,
    created_by        uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS retainers_client_idx ON retainers (client_id, status);
CREATE INDEX IF NOT EXISTS retainers_due_idx ON retainers (next_billing_date) WHERE status = 'active';

-- Multi-team milestones + SLA: a milestone can belong to a different team
-- than the project's own, with its own expected response time in hours.
ALTER TABLE payment_milestones
    ADD COLUMN IF NOT EXISTS team_id uuid REFERENCES teams (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS sla_response_hours int CHECK (sla_response_hours IS NULL OR sla_response_hours > 0);
CREATE INDEX IF NOT EXISTS payment_milestones_team_idx ON payment_milestones (team_id);

CREATE TABLE IF NOT EXISTS sla_breaches (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    milestone_id uuid NOT NULL REFERENCES payment_milestones (id) ON DELETE CASCADE,
    breach_type  text NOT NULL CHECK (breach_type IN ('response', 'delivery')),
    detected_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at  timestamptz,
    note         text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS sla_breaches_milestone_idx ON sla_breaches (milestone_id, detected_at DESC);

-- Read views: the nullable dates are cast to text here (not in the queries),
-- so sqlc's overrides on the view columns give *string fields and a contract
-- without an end date (or a retainer without a next billing date) scans
-- cleanly. Same pattern as card_details / employee_details.
CREATE OR REPLACE VIEW contract_details AS
SELECT ct.id, ct.contract_number, ct.client_id, ct.project_id, ct.title,
       ct.value, ct.currency,
       ct.start_date::text AS start_date, ct.end_date::text AS end_date,
       ct.terms, ct.document_url, ct.status, ct.signed_at, ct.created_by,
       ct.created_at, ct.updated_at,
       c.company_name AS client_name, c.client_code, p.name AS project_name
FROM contracts ct
JOIN clients c ON c.id = ct.client_id
LEFT JOIN projects p ON p.id = ct.project_id;

CREATE OR REPLACE VIEW retainer_details AS
SELECT r.id, r.client_id, r.title, r.monthly_fee, r.currency,
       r.start_date::text AS start_date, r.billing_day, r.status, r.auto_renew,
       r.next_billing_date::text AS next_billing_date,
       r.created_by, r.created_at, r.updated_at,
       c.company_name AS client_name, c.client_code, c.email AS client_email
FROM retainers r
JOIN clients c ON c.id = r.client_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW IF EXISTS retainer_details;
DROP VIEW IF EXISTS contract_details;
DROP TABLE IF EXISTS sla_breaches;
ALTER TABLE payment_milestones DROP COLUMN IF EXISTS sla_response_hours;
ALTER TABLE payment_milestones DROP COLUMN IF EXISTS team_id;
DROP TABLE IF EXISTS retainers;
DROP TABLE IF EXISTS contracts;
DROP SEQUENCE IF EXISTS contract_number_seq;
-- +goose StatementEnd
