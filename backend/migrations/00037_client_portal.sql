-- +goose Up
-- +goose StatementBegin
-- ── Client portal ──────────────────────────────────────────────
-- Clients get a limited login (role 'client') to view their own projects,
-- see sent updates, and submit feedback. They cannot see internal data
-- (financials, team members, internal notes).

INSERT INTO roles (code, name) VALUES ('client', 'Client')
ON CONFLICT (code) DO NOTHING;

-- Which projects a client user may see.
CREATE TABLE client_project_access (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (client_user_id, project_id)
);
CREATE INDEX client_project_access_user_idx ON client_project_access (client_user_id);
CREATE INDEX client_project_access_project_idx ON client_project_access (project_id);

-- Feedback submitted by clients on their projects.
CREATE TABLE client_feedback (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    client_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body           text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 10000),
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX client_feedback_project_idx ON client_feedback (project_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS client_feedback;
DROP TABLE IF EXISTS client_project_access;
DELETE FROM roles WHERE code = 'client';
-- +goose StatementEnd
