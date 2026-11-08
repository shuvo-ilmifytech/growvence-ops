-- Expense approval flow: entries submitted by non-HR/CEO users start as pending.
-- Existing entries are grandfathered in as approved.

-- +goose Up
ALTER TABLE finance_entries
    ADD COLUMN status TEXT NOT NULL DEFAULT 'approved'
        CHECK (status IN ('pending', 'approved', 'rejected')),
    ADD COLUMN approved_by UUID REFERENCES users (id),
    ADD COLUMN approved_at TIMESTAMPTZ,
    ADD COLUMN rejection_reason TEXT;

CREATE INDEX finance_entries_status_idx ON finance_entries (status);
