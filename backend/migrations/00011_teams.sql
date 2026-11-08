-- +goose Up
-- +goose StatementBegin
-- Ops teams. A team has one Team Lead and its members, and owns many projects.
-- Each project type is routed to one team; a submitted project goes to its type's team.
CREATE TABLE teams (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       citext NOT NULL UNIQUE CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    lead_id    uuid REFERENCES users (id),
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX teams_lead_idx ON teams (lead_id);

-- One team per member for now (open question Q3: multi-team membership).
CREATE TABLE team_members (
    user_id  uuid PRIMARY KEY REFERENCES users (id),
    team_id  uuid NOT NULL REFERENCES teams (id),
    added_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX team_members_team_idx ON team_members (team_id);

ALTER TABLE project_types ADD COLUMN team_id uuid REFERENCES teams (id);
ALTER TABLE projects ADD COLUMN team_id uuid REFERENCES teams (id);
CREATE INDEX projects_team_status_idx ON projects (team_id, status);

-- team_lead_id is the team's lead only while they are an active Team Leader. A project
-- without a team, or whose team has no active lead, is open to every Team Leader.
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
    tl.id                                 AS team_lead_id
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

-- +goose Down
-- +goose StatementBegin
DROP VIEW update_details;
DROP VIEW project_details;
ALTER TABLE projects DROP COLUMN team_id;
ALTER TABLE project_types DROP COLUMN team_id;
DROP TABLE team_members, teams;

CREATE VIEW project_details AS
SELECT
    p.id, p.number, p.name, p.profile_id, mp.name::text AS profile_name, p.client_name, p.order_id,
    p.client_contact, p.project_type_id, pt.name::text AS project_type_name, p.scope,
    p.budget::text AS budget, p.agreed_price::text AS agreed_price, p.currency::text AS currency,
    p.start_date::text AS start_date, p.deadline::text AS deadline, p.priority, p.special_notes,
    p.status, p.created_by, cu.name AS created_by_name, p.assigned_to, au.name AS assigned_to_name,
    p.due_date::text AS due_date, coalesce(p.due_date, p.deadline)::text AS effective_due,
    p.revision_count, p.first_submitted_at, p.first_assigned_at, p.completed_at, p.cancelled_at,
    p.last_activity_at, p.lock_version, p.created_at, p.updated_at
FROM projects p
JOIN users cu ON cu.id = p.created_by
LEFT JOIN users au ON au.id = p.assigned_to
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id
LEFT JOIN project_types pt ON pt.id = p.project_type_id;

CREATE VIEW update_details AS
SELECT
    u.id, u.number, u.project_id, p.number AS project_number, p.name AS project_name, p.order_id,
    mp.name::text AS profile_name, p.client_name, p.status AS project_status,
    p.created_by AS project_sales_id, p.assigned_to AS project_assignee_id, u.author_id,
    au.name AS author_name, u.message, u.destination, u.status, u.revision_number, u.submitted_at,
    u.checked_by, cu.name AS checked_by_name, u.checked_at, u.auto_approved, u.sent_by,
    su.name AS sent_by_name, u.sent_at, u.lock_version, u.created_at, u.updated_at,
    (SELECT count(*) FROM files f WHERE f.update_id = u.id AND f.removed_at IS NULL) AS attachment_count,
    (SELECT count(*) FROM update_comments c WHERE c.update_id = u.id AND c.thread = 'ops')   AS ops_comment_count,
    (SELECT count(*) FROM update_comments c WHERE c.update_id = u.id AND c.thread = 'sales') AS sales_comment_count
FROM updates u
JOIN projects p ON p.id = u.project_id
JOIN users au ON au.id = u.author_id
LEFT JOIN users cu ON cu.id = u.checked_by
LEFT JOIN users su ON su.id = u.sent_by
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id;
-- +goose StatementEnd
