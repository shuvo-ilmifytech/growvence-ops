-- +goose Up
-- +goose StatementBegin
-- A project can have several Ops members. project_members holds all of them;
-- projects.assigned_to stays as the first member (lists, reports, the active-assignee check).
CREATE TABLE project_members (
    project_id uuid NOT NULL REFERENCES projects (id),
    user_id    uuid NOT NULL REFERENCES users (id),
    added_by   uuid NOT NULL REFERENCES users (id),
    added_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX project_members_user_idx ON project_members (user_id);

INSERT INTO project_members (project_id, user_id, added_by, added_at)
SELECT p.id, p.assigned_to, coalesce(a.assigned_by, p.created_by), coalesce(a.assigned_at, p.first_assigned_at, now())
FROM projects p
LEFT JOIN project_assignments a ON a.project_id = p.id AND a.user_id = p.assigned_to AND a.unassigned_at IS NULL
WHERE p.assigned_to IS NOT NULL;

-- Assignment history: one open row per member instead of one per project.
DROP INDEX project_assignments_one_current;
CREATE UNIQUE INDEX project_assignments_one_current ON project_assignments (project_id, user_id)
    WHERE unassigned_at IS NULL;

-- ── Board: cards on a project (To do → In progress → Review → Done) ──
-- Sales add client requests (source 'client'); the Team Leader adds internal tasks.
CREATE TABLE project_cards (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects (id),
    number      int NOT NULL,
    title       text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 20000),
    source      text NOT NULL CHECK (source IN ('client', 'internal')),
    column_key  text NOT NULL DEFAULT 'todo' CHECK (column_key IN ('todo', 'in_progress', 'review', 'done')),
    position    int NOT NULL DEFAULT 0,
    due_date    date,
    created_by  uuid NOT NULL REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    done_at     timestamptz,
    removed_at  timestamptz,
    removed_by  uuid REFERENCES users (id),
    UNIQUE (project_id, number)
);
CREATE INDEX project_cards_board_idx ON project_cards (project_id, column_key, position) WHERE removed_at IS NULL;

CREATE TABLE card_assignees (
    card_id  uuid NOT NULL REFERENCES project_cards (id),
    user_id  uuid NOT NULL REFERENCES users (id),
    added_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (card_id, user_id)
);
CREATE INDEX card_assignees_user_idx ON card_assignees (user_id);

CREATE TABLE card_comments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id    uuid NOT NULL REFERENCES project_cards (id),
    author_id  uuid NOT NULL REFERENCES users (id),
    body       text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 10000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX card_comments_card_idx ON card_comments (card_id, created_at);

-- Read model for the board: names and dates as text, assignees in name order.
CREATE VIEW card_details AS
SELECT c.id, c.project_id, c.number, c.title, c.description, c.source, c.column_key, c.position,
       c.due_date::text AS due_date, c.created_by, cu.name AS created_by_name, c.created_at, c.updated_at, c.done_at,
       coalesce((SELECT array_agg(a.user_id ORDER BY au.name) FROM card_assignees a JOIN users au ON au.id = a.user_id
                 WHERE a.card_id = c.id), '{}')::uuid[] AS assignee_ids,
       coalesce((SELECT array_agg(au.name ORDER BY au.name) FROM card_assignees a JOIN users au ON au.id = a.user_id
                 WHERE a.card_id = c.id), '{}')::text[] AS assignee_names,
       (SELECT count(*) FROM card_comments cc WHERE cc.card_id = c.id) AS comment_count
FROM project_cards c
JOIN users cu ON cu.id = c.created_by
WHERE c.removed_at IS NULL;

CREATE OR REPLACE VIEW project_details AS
SELECT
    p.id,
    p.number,
    p.name,
    p.profile_id,
    mp.name::text                         AS profile_name,
    p.client_name,
    p.order_id,
    p.client_contact,
    p.project_type_id,
    pt.name::text                         AS project_type_name,
    p.scope,
    p.budget::text                        AS budget,
    p.agreed_price::text                  AS agreed_price,
    p.currency::text                      AS currency,
    p.start_date::text                    AS start_date,
    p.deadline::text                      AS deadline,
    p.priority,
    p.special_notes,
    p.status,
    p.created_by,
    cu.name                               AS created_by_name,
    p.assigned_to,
    au.name                               AS assigned_to_name,
    p.due_date::text                      AS due_date,
    coalesce(p.due_date, p.deadline)::text AS effective_due,
    p.revision_count,
    p.first_submitted_at,
    p.first_assigned_at,
    p.completed_at,
    p.cancelled_at,
    p.last_activity_at,
    p.lock_version,
    p.created_at,
    p.updated_at,
    p.team_id,
    t.name::text                          AS team_name,
    tl.id                                 AS team_lead_id,
    -- Everyone assigned to the project; assigned_to is the first of them.
    coalesce((SELECT array_agg(m.user_id ORDER BY m.added_at, m.user_id) FROM project_members m WHERE m.project_id = p.id),
             '{}')::uuid[]               AS member_ids,
    coalesce((SELECT array_agg(mu.name ORDER BY m.added_at, m.user_id) FROM project_members m JOIN users mu ON mu.id = m.user_id
              WHERE m.project_id = p.id), '{}')::text[] AS member_names
FROM projects p
JOIN users cu ON cu.id = p.created_by
LEFT JOIN users au ON au.id = p.assigned_to
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id
LEFT JOIN project_types pt ON pt.id = p.project_type_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users tl ON tl.id = t.lead_id AND tl.is_active AND tl.role = 'tl';


CREATE OR REPLACE VIEW update_details AS
SELECT
    u.id,
    u.number,
    u.project_id,
    p.number                AS project_number,
    p.name                  AS project_name,
    p.order_id,
    mp.name::text           AS profile_name,
    p.client_name,
    p.status                AS project_status,
    p.created_by            AS project_sales_id,
    p.assigned_to           AS project_assignee_id,
    u.author_id,
    au.name                 AS author_name,
    u.message,
    u.destination,
    u.status,
    u.revision_number,
    u.submitted_at,
    u.checked_by,
    cu.name                 AS checked_by_name,
    u.checked_at,
    u.auto_approved,
    u.sent_by,
    su.name                 AS sent_by_name,
    u.sent_at,
    u.lock_version,
    u.created_at,
    u.updated_at,
    (SELECT count(*) FROM files f WHERE f.update_id = u.id AND f.removed_at IS NULL) AS attachment_count,
    (SELECT count(*) FROM update_comments c WHERE c.update_id = u.id AND c.thread = 'ops')   AS ops_comment_count,
    (SELECT count(*) FROM update_comments c WHERE c.update_id = u.id AND c.thread = 'sales') AS sales_comment_count,
    p.team_id               AS project_team_id,
    tl.id                   AS project_team_lead_id,
    coalesce((SELECT array_agg(m.user_id) FROM project_members m WHERE m.project_id = p.id), '{}')::uuid[] AS project_member_ids
FROM updates u
JOIN projects p ON p.id = u.project_id
JOIN users au ON au.id = u.author_id
LEFT JOIN users cu ON cu.id = u.checked_by
LEFT JOIN users su ON su.id = u.sent_by
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users tl ON tl.id = t.lead_id AND tl.is_active AND tl.role = 'tl';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW update_details;
DROP VIEW project_details;
DROP VIEW card_details;
DROP TABLE card_comments, card_assignees, project_cards;
DROP INDEX project_assignments_one_current;
CREATE UNIQUE INDEX project_assignments_one_current ON project_assignments (project_id) WHERE unassigned_at IS NULL;
DROP TABLE project_members;
CREATE VIEW project_details AS
SELECT
    p.id,
    p.number,
    p.name,
    p.profile_id,
    mp.name::text                         AS profile_name,
    p.client_name,
    p.order_id,
    p.client_contact,
    p.project_type_id,
    pt.name::text                         AS project_type_name,
    p.scope,
    p.budget::text                        AS budget,
    p.agreed_price::text                  AS agreed_price,
    p.currency::text                      AS currency,
    p.start_date::text                    AS start_date,
    p.deadline::text                      AS deadline,
    p.priority,
    p.special_notes,
    p.status,
    p.created_by,
    cu.name                               AS created_by_name,
    p.assigned_to,
    au.name                               AS assigned_to_name,
    p.due_date::text                      AS due_date,
    coalesce(p.due_date, p.deadline)::text AS effective_due,
    p.revision_count,
    p.first_submitted_at,
    p.first_assigned_at,
    p.completed_at,
    p.cancelled_at,
    p.last_activity_at,
    p.lock_version,
    p.created_at,
    p.updated_at,
    p.team_id,
    t.name::text                          AS team_name,
    tl.id                                 AS team_lead_id
FROM projects p
JOIN users cu ON cu.id = p.created_by
LEFT JOIN users au ON au.id = p.assigned_to
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id
LEFT JOIN project_types pt ON pt.id = p.project_type_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users tl ON tl.id = t.lead_id AND tl.is_active AND tl.role = 'tl';


CREATE VIEW update_details AS
SELECT
    u.id,
    u.number,
    u.project_id,
    p.number                AS project_number,
    p.name                  AS project_name,
    p.order_id,
    mp.name::text           AS profile_name,
    p.client_name,
    p.status                AS project_status,
    p.created_by            AS project_sales_id,
    p.assigned_to           AS project_assignee_id,
    u.author_id,
    au.name                 AS author_name,
    u.message,
    u.destination,
    u.status,
    u.revision_number,
    u.submitted_at,
    u.checked_by,
    cu.name                 AS checked_by_name,
    u.checked_at,
    u.auto_approved,
    u.sent_by,
    su.name                 AS sent_by_name,
    u.sent_at,
    u.lock_version,
    u.created_at,
    u.updated_at,
    (SELECT count(*) FROM files f WHERE f.update_id = u.id AND f.removed_at IS NULL) AS attachment_count,
    (SELECT count(*) FROM update_comments c WHERE c.update_id = u.id AND c.thread = 'ops')   AS ops_comment_count,
    (SELECT count(*) FROM update_comments c WHERE c.update_id = u.id AND c.thread = 'sales') AS sales_comment_count,
    p.team_id               AS project_team_id,
    tl.id                   AS project_team_lead_id
FROM updates u
JOIN projects p ON p.id = u.project_id
JOIN users au ON au.id = u.author_id
LEFT JOIN users cu ON cu.id = u.checked_by
LEFT JOIN users su ON su.id = u.sent_by
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users tl ON tl.id = t.lead_id AND tl.is_active AND tl.role = 'tl';
-- +goose StatementEnd
