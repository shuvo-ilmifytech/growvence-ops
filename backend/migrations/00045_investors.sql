-- +goose Up
-- +goose StatementBegin
-- Investor Finance Forum (2026-11-07, Phase 3): investors, their capital,
-- profit shares, distributions, withdrawals, and investor-tracked expenses.
--
-- Currency rates are NOT duplicated here: the finance_rates table (00026) is
-- already the single source of truth for USD->BDT rates, with the
-- finance_rate(cur, on_day) helper. Investor endpoints read/write that table.
INSERT INTO roles (code, name) VALUES ('investor', 'Investor') ON CONFLICT (code) DO NOTHING;

-- An investor is a person with capital in the company. user_id links their
-- login when they have one; NULL until then (they can still be tracked).
CREATE TABLE IF NOT EXISTS investors (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    name       text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    email      text,
    phone      text,
    join_date  date NOT NULL DEFAULT CURRENT_DATE,
    status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    notes      text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS investors_status_idx ON investors (status);

-- Manual capital: every amount an investor put in, in its own currency.
-- amount_bdt is derived from the rate recorded at the time, so later rate
-- changes never rewrite history.
CREATE TABLE IF NOT EXISTS investments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    investor_id uuid NOT NULL REFERENCES investors (id) ON DELETE CASCADE,
    amount      numeric(16,2) NOT NULL CHECK (amount > 0),
    currency    text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    rate_to_bdt numeric(12,4) NOT NULL DEFAULT 1 CHECK (rate_to_bdt > 0 AND (currency <> 'BDT' OR rate_to_bdt = 1)),
    amount_bdt  numeric(16,2) GENERATED ALWAYS AS (round(amount * rate_to_bdt, 2)) STORED,
    method      text,
    invested_at date NOT NULL DEFAULT CURRENT_DATE,
    note        text NOT NULL DEFAULT '',
    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS investments_investor_idx ON investments (investor_id, invested_at DESC);

-- Profit settings: exactly one row (id = 1). reserve_percent is the share of
-- every distribution the company keeps as its reserve fund.
CREATE TABLE IF NOT EXISTS profit_settings (
    id              smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    reserve_percent numeric(5,2) NOT NULL DEFAULT 10 CHECK (reserve_percent >= 0 AND reserve_percent <= 100),
    updated_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at      timestamptz NOT NULL DEFAULT now()
);
INSERT INTO profit_settings (id, reserve_percent) VALUES (1, 10) ON CONFLICT (id) DO NOTHING;

-- Share history: never UPDATE, always INSERT a new row. The latest row with
-- effective_from <= today is the investor's current share. Keeping history
-- means past distributions can always be explained.
CREATE TABLE IF NOT EXISTS investor_shares (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    investor_id    uuid NOT NULL REFERENCES investors (id) ON DELETE CASCADE,
    share_percent  numeric(5,2) NOT NULL CHECK (share_percent >= 0 AND share_percent <= 100),
    effective_from date NOT NULL DEFAULT CURRENT_DATE,
    created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS investor_shares_investor_idx ON investor_shares (investor_id, effective_from DESC, created_at DESC);

-- One row per investor per income event. The reserve's cut is a row with
-- investor_id NULL. UNIQUE keeps a retry (or a double-click) from paying twice.
CREATE TABLE IF NOT EXISTS profit_distributions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_type    text NOT NULL CHECK (source_type IN ('milestone', 'income', 'manual')),
    source_id      uuid,
    investor_id    uuid REFERENCES investors (id) ON DELETE CASCADE,
    amount         numeric(16,2) NOT NULL CHECK (amount >= 0),
    currency       text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    rate_to_bdt    numeric(12,4) NOT NULL DEFAULT 1 CHECK (rate_to_bdt > 0),
    amount_bdt     numeric(16,2) NOT NULL CHECK (amount_bdt >= 0),
    share_percent  numeric(5,2) NOT NULL CHECK (share_percent >= 0 AND share_percent <= 100),
    distributed_at timestamptz NOT NULL DEFAULT now(),
    note           text NOT NULL DEFAULT '',
    UNIQUE (source_type, source_id, investor_id)
);
CREATE INDEX IF NOT EXISTS profit_distributions_investor_idx ON profit_distributions (investor_id, distributed_at DESC);
-- The reserve row has investor_id NULL, which the UNIQUE above can't dedupe
-- (NULLs never conflict), so a retry could record the reserve twice. This
-- partial index closes that hole: one reserve row per income event.
CREATE UNIQUE INDEX IF NOT EXISTS profit_distributions_reserve_unique_idx
    ON profit_distributions (source_type, source_id) WHERE investor_id IS NULL;

-- Tools & other expenses, deducted from income before anyone counts profit.
-- These are company costs tracked for the investors; office purchases still
-- go through expense invoices (00041).
CREATE TABLE IF NOT EXISTS investor_expenses (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    category    text NOT NULL CHECK (category IN ('Tools', 'Software', 'Office', 'Marketing', 'Other')),
    description text NOT NULL CHECK (length(btrim(description)) BETWEEN 1 AND 500),
    amount      numeric(16,2) NOT NULL CHECK (amount > 0),
    currency    text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    rate_to_bdt numeric(12,4) NOT NULL DEFAULT 1 CHECK (rate_to_bdt > 0 AND (currency <> 'BDT' OR rate_to_bdt = 1)),
    amount_bdt  numeric(16,2) GENERATED ALWAYS AS (round(amount * rate_to_bdt, 2)) STORED,
    expense_date date NOT NULL DEFAULT CURRENT_DATE,
    receipt_url text,
    added_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS investor_expenses_date_idx ON investor_expenses (expense_date DESC, category);

-- Withdrawals: an investor takes money out of their earned balance.
-- pending -> approved -> paid (or rejected). Only 'paid' moves money.
CREATE TABLE IF NOT EXISTS withdrawals (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    investor_id  uuid NOT NULL REFERENCES investors (id) ON DELETE CASCADE,
    amount       numeric(16,2) NOT NULL CHECK (amount > 0),
    currency     text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    rate_to_bdt  numeric(12,4) NOT NULL DEFAULT 1 CHECK (rate_to_bdt > 0 AND (currency <> 'BDT' OR rate_to_bdt = 1)),
    amount_bdt   numeric(16,2) GENERATED ALWAYS AS (round(amount * rate_to_bdt, 2)) STORED,
    method       text NOT NULL,
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'paid', 'rejected')),
    requested_at timestamptz NOT NULL DEFAULT now(),
    requested_by uuid REFERENCES users (id) ON DELETE SET NULL,
    reviewed_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    reviewed_at  timestamptz,
    paid_at      timestamptz,
    note         text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS withdrawals_investor_idx ON withdrawals (investor_id, status, requested_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS withdrawals;
DROP TABLE IF EXISTS investor_expenses;
DROP TABLE IF EXISTS profit_distributions;
DROP TABLE IF EXISTS investor_shares;
DROP TABLE IF EXISTS profit_settings;
DROP TABLE IF EXISTS investments;
DROP TABLE IF EXISTS investors;
DELETE FROM roles WHERE code = 'investor';
-- +goose StatementEnd
