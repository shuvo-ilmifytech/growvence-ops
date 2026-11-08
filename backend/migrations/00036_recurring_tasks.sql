-- +goose Up
-- +goose StatementBegin
-- Recurring tasks (2026-11-01): scheduled tasks that auto-create board cards
-- on a daily/weekly/monthly cadence. Processed by the hourly recurring job
-- in cmd/api/main.go (httpapi.ProcessDueRecurringTasks).
CREATE TABLE recurring_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '',
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    frequency TEXT NOT NULL CHECK (frequency IN ('daily', 'weekly', 'monthly')),
    day_of_week INT CHECK (day_of_week BETWEEN 0 AND 6),
    day_of_month INT CHECK (day_of_month BETWEEN 1 AND 31),
    next_run DATE NOT NULL,
    last_run DATE,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_recurring_tasks_due ON recurring_tasks (next_run) WHERE is_active = true;
CREATE INDEX idx_recurring_tasks_project ON recurring_tasks (project_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS recurring_tasks;
-- +goose StatementEnd
