-- +goose Up
-- +goose StatementBegin
-- Expenses are raised as invoices and approved in two steps, so nobody books company money
-- alone: the person who needs something writes the invoice, HR approves it, then the CEO.
-- The CEO's approval is what posts it to the ledger, so the amount is never re-typed.

-- Office Admin: buys what the office needs and raises the invoice for it. No project access,
-- no ledger, no payroll — only their own invoices, plus the usual self-service HR.
INSERT INTO roles (code, name) VALUES ('office_admin', 'Office Admin');

-- One invoice is one purchase, whatever it covers: a fan, four chairs and a box of tea are
-- three lines on it. The money lives on the lines (invoice_items), so the invoice itself
-- keeps only what the whole purchase shares: its currency, the rate, the date and what it
-- is for. Its total is the lines' sum (invoice_details), never a second copy of it.
--
-- pending      = waiting for HR (raised by an Office Admin)
-- awaiting_ceo = HR approved, or HR raised it themselves; waiting for the CEO
-- approved     = the CEO approved it and it is in the ledger
CREATE TABLE invoices (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number          bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    currency        text NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    rate_to_bdt     numeric(12,4) NOT NULL CHECK (rate_to_bdt > 0 AND (currency <> 'BDT' OR rate_to_bdt = 1)),
    expense_date    date NOT NULL,
    description     text NOT NULL CHECK (length(btrim(description)) BETWEEN 1 AND 200),
    status          text NOT NULL CHECK (status IN ('pending', 'awaiting_ceo', 'approved', 'rejected', 'cancelled')),
    requested_by    uuid NOT NULL REFERENCES users (id),
    hr_reviewed_by  uuid REFERENCES users (id),
    hr_reviewed_at  timestamptz,
    hr_note         text NOT NULL DEFAULT '' CHECK (length(hr_note) <= 1000),
    ceo_reviewed_by uuid REFERENCES users (id),
    ceo_reviewed_at timestamptz,
    ceo_note        text NOT NULL DEFAULT '' CHECK (length(ceo_note) <= 1000),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    -- A step can never be skipped: the trail has to show who let it through. The CEO may
    -- raise an invoice themselves, which is approved on the spot with no HR step.
    CONSTRAINT invoices_review_trail CHECK (
        (status <> 'awaiting_ceo' OR hr_reviewed_by IS NOT NULL) AND
        (status <> 'approved' OR ceo_reviewed_by IS NOT NULL)
    )
);
CREATE INDEX invoices_open_idx ON invoices (status, expense_date DESC) WHERE status IN ('pending', 'awaiting_ceo');
CREATE INDEX invoices_requested_by_idx ON invoices (requested_by, created_at DESC);
CREATE INDEX invoices_date_idx ON invoices (expense_date DESC);

-- A line of an invoice, in the invoice's currency. The category is per line, because one
-- purchase can mix a fan (Equipment) with tea (Food & refreshments), and the ledger should
-- say so. position keeps the order they were typed in.
CREATE TABLE invoice_items (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id  uuid NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,
    position    int NOT NULL CHECK (position >= 0),
    category_id uuid NOT NULL REFERENCES finance_categories (id),
    description text NOT NULL CHECK (length(btrim(description)) BETWEEN 1 AND 200),
    quantity    numeric(12,3) NOT NULL CHECK (quantity > 0),
    unit_price  numeric(14,2) NOT NULL CHECK (unit_price > 0),
    amount      numeric(16,2) GENERATED ALWAYS AS (round(quantity * unit_price, 2)) STORED,
    UNIQUE (invoice_id, position)
);
CREATE INDEX invoice_items_invoice_idx ON invoice_items (invoice_id);
CREATE INDEX invoice_items_category_idx ON invoice_items (category_id);

-- The ledger lines an approved invoice created: one per category it covers, so a mixed
-- purchase still counts under the right headings. Deleting a line (a correction only the
-- CEO can make) leaves the invoice as the record of what was approved.
ALTER TABLE finance_entries ADD COLUMN invoice_id uuid REFERENCES invoices (id);
CREATE INDEX finance_entries_invoice_idx ON finance_entries (invoice_id) WHERE invoice_id IS NOT NULL;

CREATE VIEW invoice_details AS
SELECT
    i.id, i.number, i.currency, i.rate_to_bdt::text AS rate_to_bdt,
    coalesce(t.total, 0)::text AS amount,
    round(coalesce(t.total, 0) * i.rate_to_bdt, 2)::text AS amount_bdt,
    coalesce(t.items, 0)::int AS items,
    i.expense_date::text AS expense_date, i.description, i.status,
    i.requested_by, rq.name AS requested_by_name, rq.role AS requested_by_role,
    i.hr_reviewed_by, hrv.name AS hr_reviewed_by_name, i.hr_reviewed_at, i.hr_note,
    i.ceo_reviewed_by, ceo.name AS ceo_reviewed_by_name, i.ceo_reviewed_at, i.ceo_note,
    (SELECT count(*) FROM finance_entries e WHERE e.invoice_id = i.id)::int AS ledger_lines,
    i.created_at, i.updated_at
FROM invoices i
JOIN users rq ON rq.id = i.requested_by
LEFT JOIN users hrv ON hrv.id = i.hr_reviewed_by
LEFT JOIN users ceo ON ceo.id = i.ceo_reviewed_by
LEFT JOIN (
    SELECT invoice_id, sum(amount) AS total, count(*) AS items FROM invoice_items GROUP BY invoice_id
) t ON t.invoice_id = i.id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW invoice_details;
DROP INDEX finance_entries_invoice_idx;
ALTER TABLE finance_entries DROP COLUMN invoice_id;
DROP TABLE invoice_items;
DROP TABLE invoices;
UPDATE users SET role = 'member' WHERE role = 'office_admin';
DELETE FROM roles WHERE code = 'office_admin';
-- +goose StatementEnd
