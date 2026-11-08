-- +goose Up
-- +goose StatementBegin
-- Payroll entered by hand (HR + CEO): what each person was actually paid for a month. The
-- fixed salary is the starting point; bonuses and deductions (unpaid absence, lateness,
-- advances, …) are added as lines. Finance counts these payments as payroll, replacing the
-- automatic full-month count from salary records.
CREATE TABLE payroll_payments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id),
    month       date NOT NULL CHECK (extract(day FROM month) = 1), -- the month the pay is for
    base_amount numeric(14,2) NOT NULL CHECK (base_amount >= 0),
    currency    text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    rate_to_bdt numeric(12,4) NOT NULL CHECK (rate_to_bdt > 0 AND (currency <> 'BDT' OR rate_to_bdt = 1)),
    -- base + bonuses − deductions, kept by the API together with the lines.
    net_amount  numeric(16,2) NOT NULL CHECK (net_amount >= 0),
    net_bdt     numeric(18,2) GENERATED ALWAYS AS (round(net_amount * rate_to_bdt, 2)) STORED,
    paid_on     date NOT NULL,
    note        text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    created_by  uuid NOT NULL REFERENCES users (id),
    updated_by  uuid NOT NULL REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, month)
);
CREATE INDEX payroll_payments_month_idx ON payroll_payments (month);

CREATE TABLE payroll_adjustments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id uuid NOT NULL REFERENCES payroll_payments (id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('bonus', 'deduction')),
    reason     text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 120),
    amount     numeric(14,2) NOT NULL CHECK (amount > 0),
    position   int NOT NULL
);
CREATE INDEX payroll_adjustments_payment_idx ON payroll_adjustments (payment_id, position);
-- +goose StatementEnd

-- +goose Down
DROP TABLE payroll_adjustments;
DROP TABLE payroll_payments;
