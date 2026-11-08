-- +goose Up
-- +goose StatementBegin
-- Sprint 1 (Money Safety, 2026-11-08): sequence-backed invoice numbers,
-- non-negative money CHECKs on client_invoices, one investor login per
-- investor record (P0-4, P0-5, P0-9).

-- Invoice numbers from a sequence, so two concurrent runs can never compute
-- the same MAX+1. Seeded so the next number is one above the highest numeric
-- suffix already in use (across all years); a fresh database starts at 1.
-- setval(..., false) makes the next nextval() return exactly that value.
CREATE SEQUENCE IF NOT EXISTS invoice_num_seq;
SELECT setval('invoice_num_seq', COALESCE(
    (SELECT MAX(CAST(SUBSTRING(invoice_number FROM 'INV-[0-9]+-([0-9]+)') AS int))
     FROM client_invoices
     WHERE invoice_number ~ '^INV-[0-9]+-[0-9]+$'), 0) + 1, false);

ALTER TABLE client_invoices
    ADD CONSTRAINT client_invoices_subtotal_nonneg CHECK (subtotal >= 0),
    ADD CONSTRAINT client_invoices_tax_amount_nonneg CHECK (tax_amount >= 0),
    ADD CONSTRAINT client_invoices_total_nonneg CHECK (total >= 0),
    ADD CONSTRAINT client_invoices_tax_rate_range CHECK (tax_rate >= 0 AND tax_rate <= 100);

-- One login may link to at most one investor record; NULL (no login yet)
-- stays allowed for any number of rows.
CREATE UNIQUE INDEX IF NOT EXISTS investors_user_id_unique
    ON investors (user_id) WHERE user_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS investors_user_id_unique;
ALTER TABLE client_invoices
    DROP CONSTRAINT IF EXISTS client_invoices_subtotal_nonneg,
    DROP CONSTRAINT IF EXISTS client_invoices_tax_amount_nonneg,
    DROP CONSTRAINT IF EXISTS client_invoices_total_nonneg,
    DROP CONSTRAINT IF EXISTS client_invoices_tax_rate_range;
DROP SEQUENCE IF EXISTS invoice_num_seq;
-- +goose StatementEnd
