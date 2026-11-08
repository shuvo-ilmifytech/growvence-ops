-- +goose Up
-- +goose StatementBegin
-- Deadline alerts (2026-11-01): track when the "due soon" (within 3 days)
-- notification was last sent per project, so the hourly checker fires once
-- and re-arms when the due date or status changes.
ALTER TABLE projects ADD COLUMN IF NOT EXISTS due_soon_alert_at timestamptz;
CREATE INDEX IF NOT EXISTS projects_due_soon_idx ON projects (status)
    WHERE status NOT IN ('completed', 'cancelled') AND due_soon_alert_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS projects_due_soon_idx;
ALTER TABLE projects DROP COLUMN IF EXISTS due_soon_alert_at;
-- +goose StatementEnd
