-- +goose Up
-- +goose StatementBegin
-- The delivery deadline is a moment, not just a day: marketplace orders are due at a time.
-- The Team Leader's internal due date stays a plain date, taken as the end of that day.

CREATE FUNCTION org_timezone() RETURNS text LANGUAGE sql STABLE AS $$
    SELECT coalesce((SELECT value #>> '{}' FROM settings WHERE key = 'org_timezone'), 'UTC')
$$;

-- The last minute of a day in the organisation's timezone.
CREATE FUNCTION end_of_day(d date) RETURNS timestamptz LANGUAGE sql STABLE AS $$
    SELECT CASE WHEN d IS NULL THEN NULL ELSE (d + time '23:59') AT TIME ZONE org_timezone() END
$$;

-- Timestamps leave the database as ISO 8601 in UTC, which browsers and Go both parse.
CREATE FUNCTION iso(t timestamptz) RETURNS text LANGUAGE sql IMMUTABLE AS $$
    SELECT to_char(t AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
$$;

DROP VIEW project_details;
DROP INDEX projects_open_due_idx;
-- Dates and times are validated by the API; the backstop can't call a timezone-aware function.
ALTER TABLE projects DROP CONSTRAINT projects_dates_order;

-- Existing deadlines were days: keep them meaning "by the end of that day".
ALTER TABLE projects ALTER COLUMN deadline TYPE timestamptz
    USING ((deadline + time '23:59') AT TIME ZONE org_timezone());

CREATE INDEX projects_open_deadline_idx ON projects (deadline)
    WHERE status NOT IN ('completed', 'cancelled');

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
              WHERE m.project_id = p.id), '{}')::text[] AS member_names
FROM projects p
JOIN users cu ON cu.id = p.created_by
LEFT JOIN users au ON au.id = p.assigned_to
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id
LEFT JOIN project_types pt ON pt.id = p.project_type_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users tl ON tl.id = t.lead_id AND tl.is_active AND tl.role = 'tl';


-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW project_details;
DROP INDEX projects_open_deadline_idx;
ALTER TABLE projects ALTER COLUMN deadline TYPE date
    USING ((deadline AT TIME ZONE org_timezone())::date);
ALTER TABLE projects ADD CONSTRAINT projects_dates_order CHECK (
    start_date IS NULL OR deadline IS NULL OR start_date <= deadline);
CREATE INDEX projects_open_due_idx ON projects ((coalesce(due_date, deadline)))
    WHERE status NOT IN ('completed', 'cancelled');
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


DROP FUNCTION iso(timestamptz);
DROP FUNCTION end_of_day(date);
DROP FUNCTION org_timezone();
-- +goose StatementEnd
