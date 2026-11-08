-- +goose Up
-- +goose StatementBegin
-- Time tracking (2026-11-01): per-user timers against projects. ended_at NULL
-- means the timer is currently running; duration_minutes is computed on stop.
CREATE TABLE IF NOT EXISTS time_entries (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id       uuid REFERENCES projects(id) ON DELETE SET NULL,
    description      text NOT NULL DEFAULT '',
    started_at       timestamptz NOT NULL DEFAULT now(),
    ended_at         timestamptz,
    duration_minutes int,
    created_at       timestamptz NOT NULL DEFAULT now(),
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);
CREATE INDEX IF NOT EXISTS time_entries_user_idx ON time_entries (user_id);
CREATE INDEX IF NOT EXISTS time_entries_project_idx ON time_entries (project_id);
CREATE INDEX IF NOT EXISTS time_entries_started_at_idx ON time_entries (started_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE time_entries;
-- +goose StatementEnd
