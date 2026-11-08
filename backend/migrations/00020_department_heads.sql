-- +goose Up
-- +goose StatementBegin
-- A department head runs every team in their department with a Team Leader's
-- powers: they see, assign, approve and complete those teams' projects. The head
-- is an active Team Leader; one person may head several departments.
ALTER TABLE departments ADD COLUMN head_id uuid REFERENCES users(id);
CREATE INDEX departments_head_idx ON departments (head_id);

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
    iso(p.deadline)                       AS deadline,
    p.priority,
    p.special_notes,
    p.status,
    p.created_by,
    cu.name                               AS created_by_name,
    p.assigned_to,
    au.name                               AS assigned_to_name,
    p.due_date::text                      AS due_date,
    iso(coalesce(end_of_day(p.due_date), p.deadline)) AS effective_due,
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
              WHERE m.project_id = p.id), '{}')::text[] AS member_names,
    th.id                                 AS team_head_id
FROM projects p
JOIN users cu ON cu.id = p.created_by
LEFT JOIN users au ON au.id = p.assigned_to
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id
LEFT JOIN project_types pt ON pt.id = p.project_type_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users tl ON tl.id = t.lead_id AND tl.is_active AND tl.role = 'tl'
LEFT JOIN departments td ON td.id = t.department_id AND td.is_active
LEFT JOIN users th ON th.id = td.head_id AND th.is_active AND th.role = 'tl';



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
    coalesce((SELECT array_agg(m.user_id) FROM project_members m WHERE m.project_id = p.id), '{}')::uuid[] AS project_member_ids,
    th.id                   AS project_team_head_id
FROM updates u
JOIN projects p ON p.id = u.project_id
JOIN users au ON au.id = u.author_id
LEFT JOIN users cu ON cu.id = u.checked_by
LEFT JOIN users su ON su.id = u.sent_by
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users tl ON tl.id = t.lead_id AND tl.is_active AND tl.role = 'tl'
LEFT JOIN departments td ON td.id = t.department_id AND td.is_active
LEFT JOIN users th ON th.id = td.head_id AND th.is_active AND th.role = 'tl';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Dropping columns from a view needs DROP + CREATE; restore the 00015 / 00013 shapes.
DROP VIEW update_details;
DROP VIEW project_details;
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
    iso(p.deadline)                       AS deadline,
    p.priority,
    p.special_notes,
    p.status,
    p.created_by,
    cu.name                               AS created_by_name,
    p.assigned_to,
    au.name                               AS assigned_to_name,
    p.due_date::text                      AS due_date,
    iso(coalesce(end_of_day(p.due_date), p.deadline)) AS effective_due,
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
DROP INDEX departments_head_idx;
ALTER TABLE departments DROP COLUMN head_id;
-- +goose StatementEnd
