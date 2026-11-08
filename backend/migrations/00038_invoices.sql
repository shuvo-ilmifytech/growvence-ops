-- +goose Up
-- +goose StatementBegin
-- Invoice generator (2026-11-01): client invoices with line items, tax,
-- status workflow (draft -> sent -> paid/overdue).
CREATE TABLE IF NOT EXISTS client_invoices (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_number text NOT NULL UNIQUE,
    project_id     uuid REFERENCES projects(id) ON DELETE SET NULL,
    client_name    text NOT NULL DEFAULT '',
    client_email   text NOT NULL DEFAULT '',
    items          jsonb NOT NULL DEFAULT '[]',
    subtotal       numeric(12,2) NOT NULL DEFAULT 0,
    tax_rate       numeric(5,2) NOT NULL DEFAULT 0,
    tax_amount     numeric(12,2) NOT NULL DEFAULT 0,
    total          numeric(12,2) NOT NULL DEFAULT 0,
    currency       text NOT NULL DEFAULT 'USD',
    status         text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','sent','paid','overdue')),
    due_date       date,
    notes          text NOT NULL DEFAULT '',
    created_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS client_invoices_status_idx ON client_invoices (status);
CREATE INDEX IF NOT EXISTS client_invoices_project_idx ON client_invoices (project_id);
CREATE INDEX IF NOT EXISTS client_invoices_number_idx ON client_invoices (invoice_number);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE client_invoices;
-- +goose StatementEnd
