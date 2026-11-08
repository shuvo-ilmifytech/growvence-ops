-- +goose Up
-- +goose StatementBegin
-- Salary history: one row per change, the latest with effective_from <= today is current.
-- Managed and seen only by HR and the CEO, and never by the person it belongs to.
CREATE TABLE salaries (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users (id),
    amount         numeric(12,2) NOT NULL CHECK (amount > 0), -- monthly gross
    currency       text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    effective_from date NOT NULL,
    note           text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    created_by     uuid NOT NULL REFERENCES users (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, effective_from)
);
-- +goose StatementEnd

-- +goose Down
DROP TABLE salaries;
