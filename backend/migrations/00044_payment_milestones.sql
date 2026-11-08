-- +goose Up
-- +goose StatementBegin
-- Client milestone payments (2026-11-07, Phase 2): per-project payment schedule.
-- A milestone is one dated payment (starting amount, design complete, final
-- delivery...). On its due date the daily job generates a client invoice
-- (client_invoices, INV numbering) and notifies the client + the team.
CREATE TABLE IF NOT EXISTS payment_milestones (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    label            text NOT NULL CHECK (length(btrim(label)) BETWEEN 1 AND 200),
    amount           numeric(16,2) NOT NULL CHECK (amount > 0),
    currency         text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    due_date         date NOT NULL,
    status           text NOT NULL DEFAULT 'scheduled'
                     CHECK (status IN ('scheduled','invoiced','partially_paid','paid','overdue')),
    invoice_id       uuid REFERENCES client_invoices(id) ON DELETE SET NULL,
    paid_amount      numeric(16,2) NOT NULL DEFAULT 0 CHECK (paid_amount >= 0),
    paid_at          timestamptz,
    payment_method   text,
    payment_reference text,
    created_by       uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS payment_milestones_project_idx ON payment_milestones (project_id, due_date);
CREATE INDEX IF NOT EXISTS payment_milestones_due_idx ON payment_milestones (due_date, status);
CREATE INDEX IF NOT EXISTS payment_milestones_status_idx ON payment_milestones (status);

-- Every reminder sent about a milestone (manual "remind client" clicks and
-- the daily job's auto-reminders), so the team can see who was told what, when.
CREATE TABLE IF NOT EXISTS milestone_reminders (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    milestone_id uuid NOT NULL REFERENCES payment_milestones(id) ON DELETE CASCADE,
    sent_at      timestamptz NOT NULL DEFAULT now(),
    sent_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    channel      text NOT NULL DEFAULT 'app' CHECK (channel IN ('app','email','manual')),
    note         text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS milestone_reminders_milestone_idx ON milestone_reminders (milestone_id, sent_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS milestone_reminders;
DROP TABLE IF EXISTS payment_milestones;
-- +goose StatementEnd
