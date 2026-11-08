-- +goose Up
-- +goose StatementBegin
-- Client collaboration (2026-11-07, Phase 4): ClickUp-style client work.
-- Business clients (CL-0001…) linked to projects, per-card client-visible
-- comments + approvals, per-project message threads and shared files.
--
-- Two links exist on purpose: project_clients is the BUSINESS link (a company
-- works on a project); client_project_access is the LOGIN link (a client-portal
-- user may open it). A client sees a project only when both agree — the portal
-- checks client_project_access, the team links the business client here.

CREATE SEQUENCE IF NOT EXISTS client_code_seq START WITH 1;

CREATE TABLE IF NOT EXISTS clients (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_code   text NOT NULL UNIQUE,
    company_name  text NOT NULL CHECK (length(btrim(company_name)) BETWEEN 1 AND 200),
    contact_person text,
    email         text,
    phone         text,
    address       text,
    type          text NOT NULL DEFAULT 'B2B' CHECK (type IN ('B2B', 'Individual')),
    status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS clients_code_idx ON clients (client_code);
CREATE INDEX IF NOT EXISTS clients_status_idx ON clients (status);

CREATE TABLE IF NOT EXISTS client_contacts (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id  uuid NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    name       text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    email      text,
    phone      text,
    role       text,
    is_primary boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS client_contacts_client_idx ON client_contacts (client_id);

CREATE TABLE IF NOT EXISTS project_clients (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    client_id  uuid NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, client_id)
);
CREATE INDEX IF NOT EXISTS project_clients_project_idx ON project_clients (project_id);
CREATE INDEX IF NOT EXISTS project_clients_client_idx ON project_clients (client_id);

-- Task comments shared with the client. is_internal=true stays team-only;
-- everything else the linked client can read on a visible card.
CREATE TABLE IF NOT EXISTS task_comments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id    uuid NOT NULL REFERENCES project_cards (id) ON DELETE CASCADE,
    author_id  uuid REFERENCES users (id) ON DELETE SET NULL,
    body       text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 5000),
    is_internal boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    edited_at  timestamptz
);
CREATE INDEX IF NOT EXISTS task_comments_card_idx ON task_comments (card_id, created_at);

-- One pending approval per card: the team asks the client to sign off a task,
-- the client approves or asks for changes with a note.
CREATE TABLE IF NOT EXISTS task_approvals (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id      uuid NOT NULL REFERENCES project_cards (id) ON DELETE CASCADE,
    requested_by uuid REFERENCES users (id) ON DELETE SET NULL,
    requested_at timestamptz NOT NULL DEFAULT now(),
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'changes_requested')),
    reviewed_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    reviewed_at  timestamptz,
    review_note  text NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS task_approvals_one_pending_idx ON task_approvals (card_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS task_approvals_card_idx ON task_approvals (card_id, requested_at DESC);

-- Shared files: deliverables from the team (is_client_upload=false) and
-- briefs/assets the client uploads (is_client_upload=true). file_url is the
-- S3 key recorded through the project's existing presigned-upload flow.
CREATE TABLE IF NOT EXISTS client_files (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    client_id        uuid REFERENCES clients (id) ON DELETE CASCADE,
    uploaded_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    file_name        text NOT NULL CHECK (length(btrim(file_name)) BETWEEN 1 AND 255),
    file_url         text NOT NULL,
    file_size        bigint NOT NULL DEFAULT 0 CHECK (file_size >= 0),
    is_client_upload boolean NOT NULL DEFAULT false,
    uploaded_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS client_files_project_idx ON client_files (project_id, uploaded_at DESC);

-- Per-project message thread: the team and the linked client talk here.
CREATE TABLE IF NOT EXISTS client_messages (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    author_id  uuid REFERENCES users (id) ON DELETE SET NULL,
    body       text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 5000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS client_messages_project_idx ON client_messages (project_id, created_at);

-- Cards are hidden from clients by default; the team opts a task in.
ALTER TABLE project_cards ADD COLUMN IF NOT EXISTS visible_to_client boolean NOT NULL DEFAULT false;

-- card_details gains the visibility flag (latest definition is 00042's).
DROP VIEW IF EXISTS card_details;
CREATE VIEW card_details AS
SELECT c.id, c.project_id, c.client_request_id, c.container_id, c.number, c.title, c.description, c.source,
       c.column_key, c.position, c.visible_to_client,
       c.due_date::text AS due_date, c.created_by, cu.name AS created_by_name, c.created_at, c.updated_at, c.done_at,
       coalesce((SELECT array_agg(a.user_id ORDER BY au.name) FROM card_assignees a JOIN users au ON au.id = a.user_id
                 WHERE a.card_id = c.id), '{}')::uuid[] AS assignee_ids,
       coalesce((SELECT array_agg(au.name ORDER BY au.name) FROM card_assignees a JOIN users au ON au.id = a.user_id
                 WHERE a.card_id = c.id), '{}')::text[] AS assignee_names,
       (SELECT count(*) FROM card_comments cc WHERE cc.card_id = c.id) AS comment_count,
       (SELECT count(*) FROM files f WHERE f.card_id = c.id AND f.removed_at IS NULL) AS attachment_count
FROM project_cards c
JOIN users cu ON cu.id = c.created_by
WHERE c.removed_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW IF EXISTS card_details;
CREATE VIEW card_details AS
SELECT c.id, c.project_id, c.client_request_id, c.container_id, c.number, c.title, c.description, c.source,
       c.column_key, c.position,
       c.due_date::text AS due_date, c.created_by, cu.name AS created_by_name, c.created_at, c.updated_at, c.done_at,
       coalesce((SELECT array_agg(a.user_id ORDER BY au.name) FROM card_assignees a JOIN users au ON au.id = a.user_id
                 WHERE a.card_id = c.id), '{}')::uuid[] AS assignee_ids,
       coalesce((SELECT array_agg(au.name ORDER BY au.name) FROM card_assignees a JOIN users au ON au.id = a.user_id
                 WHERE a.card_id = c.id), '{}')::text[] AS assignee_names,
       (SELECT count(*) FROM card_comments cc WHERE cc.card_id = c.id) AS comment_count,
       (SELECT count(*) FROM files f WHERE f.card_id = c.id AND f.removed_at IS NULL) AS attachment_count
FROM project_cards c
JOIN users cu ON cu.id = c.created_by
WHERE c.removed_at IS NULL;
ALTER TABLE project_cards DROP COLUMN IF EXISTS visible_to_client;
DROP TABLE IF EXISTS client_messages;
DROP TABLE IF EXISTS client_files;
DROP TABLE IF EXISTS task_approvals;
DROP TABLE IF EXISTS task_comments;
DROP TABLE IF EXISTS project_clients;
DROP TABLE IF EXISTS client_contacts;
DROP TABLE IF EXISTS clients;
DROP SEQUENCE IF EXISTS client_code_seq;
-- +goose StatementEnd
