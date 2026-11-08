-- +goose Up
-- +goose StatementBegin
-- Monthly revenue targets per team (2026-10-31), set by the CEO. A target is what the team
-- should deliver in that month, after the marketplace's fee, in one currency (USD for
-- Fiverr). Progress counts projects of that currency delivered in the month.
CREATE TABLE team_targets (
    team_id    uuid NOT NULL REFERENCES teams (id),
    month      date NOT NULL CHECK (extract(day FROM month) = 1),
    amount     numeric(12, 2) NOT NULL CHECK (amount > 0),
    currency   char(3) NOT NULL DEFAULT 'USD' CHECK (currency ~ '^[A-Z]{3}$'),
    set_by     uuid NOT NULL REFERENCES users (id),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, month)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE team_targets;
-- +goose StatementEnd
