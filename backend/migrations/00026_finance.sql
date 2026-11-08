-- +goose Up
-- +goose StatementBegin
-- Finance (HR + CEO): a ledger of income and expenses entered by hand, exchange rates to
-- BDT, and the monthly analytics built from the ledger plus two automatic lines: what
-- completed projects earned (net of the marketplace fee) and payroll from salary records.

-- Categories are kept once used: deactivate instead of delete. The kind never changes.
CREATE TABLE finance_categories (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       citext NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    kind       text NOT NULL CHECK (kind IN ('income', 'expense')),
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, name)
);

-- No "Salaries" or "Marketplace payouts" here: payroll and project earnings are counted
-- automatically, so entering them by hand would count them twice.
INSERT INTO finance_categories (kind, name) VALUES
    ('income',  'Direct client payment'),
    ('income',  'Other income'),
    ('expense', 'Office rent'),
    ('expense', 'Utilities'),
    ('expense', 'Internet & phone'),
    ('expense', 'Software & tools'),
    ('expense', 'Equipment'),
    ('expense', 'Marketing'),
    ('expense', 'Bonuses & incentives'),
    ('expense', 'Travel & transport'),
    ('expense', 'Food & refreshments'),
    ('expense', 'Bank & transfer fees'),
    ('expense', 'Taxes & licences'),
    ('expense', 'Other expense');

-- Rate history: 1 unit of currency = rate BDT, from effective_from on. The rate for a day
-- is the latest one effective on or before it; BDT is always 1.
CREATE TABLE finance_rates (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    currency       text NOT NULL CHECK (currency ~ '^[A-Z]{3}$' AND currency <> 'BDT'),
    rate           numeric(12,4) NOT NULL CHECK (rate > 0),
    effective_from date NOT NULL,
    created_by     uuid NOT NULL REFERENCES users (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (currency, effective_from)
);

CREATE FUNCTION finance_rate(cur text, on_day date) RETURNS numeric LANGUAGE sql STABLE AS $$
    SELECT CASE WHEN btrim(cur) = 'BDT' THEN 1::numeric ELSE
        (SELECT r.rate FROM finance_rates r
         WHERE r.currency = btrim(cur) AND r.effective_from <= on_day
         ORDER BY r.effective_from DESC LIMIT 1)
    END
$$;

-- Each entry keeps the rate it was converted at, so later rate changes don't rewrite it.
CREATE TABLE finance_entries (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id uuid NOT NULL REFERENCES finance_categories (id),
    amount      numeric(14,2) NOT NULL CHECK (amount > 0),
    currency    text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    rate_to_bdt numeric(12,4) NOT NULL CHECK (rate_to_bdt > 0 AND (currency <> 'BDT' OR rate_to_bdt = 1)),
    amount_bdt  numeric(16,2) GENERATED ALWAYS AS (round(amount * rate_to_bdt, 2)) STORED,
    entry_date  date NOT NULL,
    description text NOT NULL CHECK (length(btrim(description)) BETWEEN 1 AND 500),
    created_by  uuid NOT NULL REFERENCES users (id),
    updated_by  uuid NOT NULL REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX finance_entries_date_idx ON finance_entries (entry_date);
CREATE INDEX finance_entries_category_idx ON finance_entries (category_id);

-- When someone was deactivated, so past months' payroll still counts them. Kept by a
-- trigger; restoring the account clears it.
CREATE TABLE user_departures (
    user_id uuid PRIMARY KEY REFERENCES users (id),
    left_at timestamptz NOT NULL
);

CREATE FUNCTION track_user_departure() RETURNS trigger AS $$
BEGIN
    IF OLD.is_active AND NOT NEW.is_active THEN
        INSERT INTO user_departures (user_id, left_at) VALUES (NEW.id, now())
        ON CONFLICT (user_id) DO UPDATE SET left_at = excluded.left_at;
    ELSIF NOT OLD.is_active AND NEW.is_active THEN
        DELETE FROM user_departures WHERE user_id = NEW.id;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_track_departure
    AFTER UPDATE OF is_active ON users
    FOR EACH ROW EXECUTE FUNCTION track_user_departure();

-- People already deactivated: when their access was revoked, or failing that their last update.
INSERT INTO user_departures (user_id, left_at)
SELECT u.id, coalesce(
    (SELECT max(a.created_at) FROM activity_log a
     WHERE a.action = 'user.access_revoked' AND a.entity_type = 'user' AND a.entity_id = u.id),
    u.updated_at)
FROM users u
WHERE NOT u.is_active;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER users_track_departure ON users;
DROP FUNCTION track_user_departure();
DROP TABLE user_departures;
DROP TABLE finance_entries;
DROP FUNCTION finance_rate(text, date);
DROP TABLE finance_rates;
DROP TABLE finance_categories;
-- +goose StatementEnd
