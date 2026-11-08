-- +goose Up
-- +goose StatementBegin
-- Analytics access (2026-10-31): the CEO always sees the Analytics page; anyone else only
-- when the CEO switches this on for them (e.g. the Operations head and the Sales leader).
ALTER TABLE users ADD COLUMN can_view_analytics boolean NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN can_view_analytics;
-- +goose StatementEnd
