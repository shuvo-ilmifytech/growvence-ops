-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS citext;

-- ── Identity ───────────────────────────────────────────────
CREATE TABLE roles (
    code text PRIMARY KEY,
    name text NOT NULL
);

INSERT INTO roles (code, name) VALUES
    ('sales',      'Sales'),
    ('tl',         'Ops Team Leader'),
    ('member',     'Ops Team Member'),
    ('management', 'Management');

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         citext NOT NULL UNIQUE CHECK (length(email) BETWEEN 3 AND 254),
    name          text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    password_hash text NOT NULL,
    role          text NOT NULL REFERENCES roles (code),
    is_admin      boolean NOT NULL DEFAULT false,
    is_active     boolean NOT NULL DEFAULT true,
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX users_role_active_idx ON users (role) WHERE is_active;

-- ── Lookups (admin-managed) ────────────────────────────────
CREATE TABLE marketplace_profiles (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       citext NOT NULL UNIQUE CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    platform   text NOT NULL DEFAULT '' CHECK (length(platform) <= 60),
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE project_types (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       citext NOT NULL UNIQUE CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_by uuid REFERENCES users (id),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO settings (key, value) VALUES
    ('inbox_requires_approval', 'true'),
    ('org_timezone',            '"Asia/Dhaka"'),
    ('batch_window_minutes',    '10');

-- ── Projects ───────────────────────────────────────────────
CREATE TABLE projects (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number              bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    name                text CHECK (length(name) <= 200),
    profile_id          uuid REFERENCES marketplace_profiles (id),
    client_name         text CHECK (length(client_name) <= 200),
    order_id            text CHECK (length(order_id) <= 100),
    client_contact      text CHECK (length(client_contact) <= 500),
    project_type_id     uuid REFERENCES project_types (id),
    scope               text CHECK (length(scope) <= 50000),
    budget              numeric(12, 2) CHECK (budget >= 0),
    agreed_price        numeric(12, 2) CHECK (agreed_price >= 0),
    currency            char(3) NOT NULL DEFAULT 'USD',
    start_date          date,
    deadline            date,
    priority            text NOT NULL DEFAULT 'medium'
                        CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
    special_notes       text CHECK (length(special_notes) <= 20000),
    status              text NOT NULL DEFAULT 'draft'
                        CHECK (status IN ('draft', 'submitted', 'needs_info', 'assigned', 'in_progress',
                                          'blocked', 'ready_for_review', 'completed', 'cancelled')),
    created_by          uuid NOT NULL REFERENCES users (id),
    assigned_to         uuid REFERENCES users (id),
    due_date            date,
    revision_count      int NOT NULL DEFAULT 0 CHECK (revision_count >= 0),
    first_submitted_at  timestamptz,
    first_assigned_at   timestamptz,
    completed_at        timestamptz,
    cancelled_at        timestamptz,
    last_activity_at    timestamptz NOT NULL DEFAULT now(),
    unassigned_alert_at timestamptz,
    overdue_alert_at    timestamptz,
    lock_version        int NOT NULL DEFAULT 1,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    -- Backstop for "required fields before submit" (the API validates first).
    CONSTRAINT projects_required_when_submitted CHECK (
        status IN ('draft', 'cancelled') OR (
            name IS NOT NULL AND btrim(name) <> '' AND
            profile_id IS NOT NULL AND
            client_name IS NOT NULL AND btrim(client_name) <> '' AND
            order_id IS NOT NULL AND btrim(order_id) <> '' AND
            project_type_id IS NOT NULL AND
            scope IS NOT NULL AND btrim(scope) <> '' AND
            agreed_price IS NOT NULL AND
            deadline IS NOT NULL
        )
    ),
    CONSTRAINT projects_assignee_when_active CHECK (
        status NOT IN ('assigned', 'in_progress', 'blocked', 'ready_for_review')
        OR assigned_to IS NOT NULL
    ),
    CONSTRAINT projects_dates_order CHECK (
        start_date IS NULL OR deadline IS NULL OR start_date <= deadline
    )
);
CREATE UNIQUE INDEX projects_profile_order_uniq ON projects (profile_id, lower(order_id))
    WHERE order_id IS NOT NULL;
CREATE INDEX projects_status_idx ON projects (status);
CREATE INDEX projects_assignee_status_idx ON projects (assigned_to, status);
CREATE INDEX projects_creator_status_idx ON projects (created_by, status);
CREATE INDEX projects_open_due_idx ON projects ((coalesce(due_date, deadline)))
    WHERE status NOT IN ('completed', 'cancelled');

CREATE TABLE project_assignments (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    uuid NOT NULL REFERENCES projects (id),
    user_id       uuid NOT NULL REFERENCES users (id),
    assigned_by   uuid NOT NULL REFERENCES users (id),
    priority      text NOT NULL CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
    due_date      date,
    assigned_at   timestamptz NOT NULL DEFAULT now(),
    unassigned_at timestamptz
);
CREATE INDEX project_assignments_project_idx ON project_assignments (project_id, assigned_at);
CREATE INDEX project_assignments_user_idx ON project_assignments (user_id);
CREATE UNIQUE INDEX project_assignments_one_current ON project_assignments (project_id)
    WHERE unassigned_at IS NULL;

CREATE TABLE project_status_history (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id  uuid NOT NULL REFERENCES projects (id),
    from_status text,
    to_status   text NOT NULL,
    changed_by  uuid NOT NULL REFERENCES users (id),
    comment     text CHECK (length(comment) <= 5000),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX project_status_history_project_idx ON project_status_history (project_id, created_at);

CREATE TABLE project_comments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects (id),
    author_id  uuid NOT NULL REFERENCES users (id),
    body       text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 10000),
    created_at timestamptz NOT NULL DEFAULT now(),
    edited_at  timestamptz
);
CREATE INDEX project_comments_project_idx ON project_comments (project_id, created_at);

CREATE TABLE project_views (
    user_id        uuid NOT NULL REFERENCES users (id),
    project_id     uuid NOT NULL REFERENCES projects (id),
    last_viewed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, project_id)
);

-- ── Sales edit tracking & acknowledgements ────────────────
CREATE TABLE project_changes (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid NOT NULL REFERENCES projects (id),
    changed_by   uuid NOT NULL REFERENCES users (id),
    field        text NOT NULL,
    old_value    text,
    new_value    text,
    is_important boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX project_changes_project_idx ON project_changes (project_id, created_at);

CREATE TABLE acknowledgements (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_change_id uuid NOT NULL REFERENCES project_changes (id),
    user_id           uuid NOT NULL REFERENCES users (id),
    acknowledged_at   timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_change_id, user_id)
);
CREATE INDEX acknowledgements_pending_idx ON acknowledgements (user_id) WHERE acknowledged_at IS NULL;

-- ── Updates ────────────────────────────────────────────────
CREATE TABLE updates (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number             bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    project_id         uuid NOT NULL REFERENCES projects (id),
    author_id          uuid NOT NULL REFERENCES users (id),
    message            text NOT NULL DEFAULT '' CHECK (length(message) <= 50000),
    destination        text NOT NULL CHECK (destination IN ('inbox', 'order_page', 'delivery')),
    status             text NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft', 'pending_approval', 'changes_requested', 'approved', 'sent')),
    revision_number    int CHECK (revision_number >= 0),
    submitted_at       timestamptz,
    checked_by         uuid REFERENCES users (id),
    checked_at         timestamptz,
    auto_approved      boolean NOT NULL DEFAULT false,
    sent_by            uuid REFERENCES users (id),
    sent_at            timestamptz,
    unsent_reminder_at timestamptz,
    lock_version       int NOT NULL DEFAULT 1,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT updates_sent_fields CHECK (
        status <> 'sent' OR (sent_by IS NOT NULL AND sent_at IS NOT NULL)
    ),
    CONSTRAINT updates_checked_fields CHECK (
        status NOT IN ('approved', 'sent') OR checked_at IS NOT NULL OR auto_approved
    ),
    CONSTRAINT updates_revision_delivery_only CHECK (
        revision_number IS NULL OR destination = 'delivery'
    )
);
CREATE INDEX updates_project_idx ON updates (project_id, created_at);
CREATE INDEX updates_status_destination_idx ON updates (status, destination);
CREATE INDEX updates_unsent_idx ON updates (checked_at) WHERE status = 'approved';

CREATE TABLE update_status_history (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    update_id   uuid NOT NULL REFERENCES updates (id),
    from_status text,
    to_status   text NOT NULL,
    changed_by  uuid NOT NULL REFERENCES users (id),
    comment     text CHECK (length(comment) <= 5000),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX update_status_history_update_idx ON update_status_history (update_id, created_at);

CREATE TABLE update_comments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    update_id  uuid NOT NULL REFERENCES updates (id),
    thread     text NOT NULL CHECK (thread IN ('ops', 'sales')),
    author_id  uuid NOT NULL REFERENCES users (id),
    body       text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 10000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX update_comments_thread_idx ON update_comments (update_id, thread, created_at);

-- ── Files (project files and update attachments) ──────────
CREATE TABLE files (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         uuid NOT NULL REFERENCES projects (id),
    update_id          uuid REFERENCES updates (id),
    original_name      text NOT NULL CHECK (length(original_name) BETWEEN 1 AND 255),
    current_version_id uuid,
    uploaded_by        uuid NOT NULL REFERENCES users (id),
    removed_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX files_project_idx ON files (project_id);
CREATE INDEX files_update_idx ON files (update_id) WHERE update_id IS NOT NULL;

CREATE TABLE file_versions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id     uuid NOT NULL REFERENCES files (id),
    version_no  int NOT NULL CHECK (version_no >= 1),
    storage_key text NOT NULL UNIQUE,
    size_bytes  bigint NOT NULL CHECK (size_bytes >= 0),
    mime_type   text NOT NULL,
    sha256      text NOT NULL,
    uploaded_by uuid NOT NULL REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (file_id, version_no)
);

ALTER TABLE files
    ADD CONSTRAINT files_current_version_fk FOREIGN KEY (current_version_id)
    REFERENCES file_versions (id) DEFERRABLE INITIALLY DEFERRED;

-- ── Notifications & audit ──────────────────────────────────
CREATE TABLE notifications (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    recipient_id uuid NOT NULL REFERENCES users (id),
    type         text NOT NULL,
    project_id   uuid REFERENCES projects (id),
    update_id    uuid REFERENCES updates (id),
    title        text NOT NULL,
    body         text NOT NULL DEFAULT '',
    data         jsonb NOT NULL DEFAULT '{}',
    is_important boolean NOT NULL DEFAULT false,
    read_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_recipient_idx ON notifications (recipient_id, created_at DESC);
CREATE INDEX notifications_unread_idx ON notifications (recipient_id) WHERE read_at IS NULL;

CREATE TABLE activity_log (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id    uuid REFERENCES users (id),
    action      text NOT NULL,
    entity_type text NOT NULL,
    entity_id   uuid,
    project_id  uuid REFERENCES projects (id),
    metadata    jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX activity_log_project_idx ON activity_log (project_id, created_at);
CREATE INDEX activity_log_created_idx ON activity_log (created_at DESC);
CREATE INDEX activity_log_actor_idx ON activity_log (actor_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE activity_log, notifications, file_versions, files, update_comments,
    update_status_history, updates, acknowledgements, project_changes, project_views,
    project_comments, project_status_history, project_assignments, projects,
    settings, project_types, marketplace_profiles, users, roles CASCADE;
-- +goose StatementEnd
