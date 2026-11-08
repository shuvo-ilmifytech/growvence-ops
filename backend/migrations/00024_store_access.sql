-- +goose Up
-- +goose StatementBegin
-- Store access: the team needs access to the client's store (Shopify collaborator,
-- WordPress login…) before it can work. Each project tracks whether it has been asked
-- for, and keeps the link the client gave. "Not requested" is the state to chase.
ALTER TABLE projects
    ADD COLUMN store_access text NOT NULL DEFAULT 'not_requested'
        CHECK (store_access IN ('not_requested', 'requested', 'given')),
    ADD COLUMN store_access_link text CHECK (length(store_access_link) <= 1000),
    ADD COLUMN store_access_changed_at timestamptz;

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
    th.id                                 AS team_head_id,
    -- What the marketplace keeps and what reaches Growvence, from the agreed price.
    p.platform_fee_percent::text          AS platform_fee_percent,
    -- Empty, not NULL, until there is an agreed price (the Go row types are plain strings).
    coalesce(round(p.agreed_price * p.platform_fee_percent / 100, 2)::text, '')::text         AS platform_fee,
    coalesce(round(p.agreed_price * (100 - p.platform_fee_percent) / 100, 2)::text, '')::text AS net_amount,
    p.store_access,
    p.store_access_link,
    p.store_access_changed_at
FROM projects p
JOIN users cu ON cu.id = p.created_by
LEFT JOIN users au ON au.id = p.assigned_to
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id
LEFT JOIN project_types pt ON pt.id = p.project_type_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users tl ON tl.id = t.lead_id AND tl.is_active AND tl.role = 'tl'
LEFT JOIN departments td ON td.id = t.department_id AND td.is_active
LEFT JOIN users th ON th.id = td.head_id AND th.is_active AND th.role = 'tl';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW project_details;
ALTER TABLE projects DROP COLUMN store_access, DROP COLUMN store_access_link, DROP COLUMN store_access_changed_at;

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
    th.id                                 AS team_head_id,
    -- What the marketplace keeps and what reaches Growvence, from the agreed price.
    p.platform_fee_percent::text          AS platform_fee_percent,
    -- Empty, not NULL, until there is an agreed price (the Go row types are plain strings).
    coalesce(round(p.agreed_price * p.platform_fee_percent / 100, 2)::text, '')::text         AS platform_fee,
    coalesce(round(p.agreed_price * (100 - p.platform_fee_percent) / 100, 2)::text, '')::text AS net_amount
FROM projects p
JOIN users cu ON cu.id = p.created_by
LEFT JOIN users au ON au.id = p.assigned_to
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id
LEFT JOIN project_types pt ON pt.id = p.project_type_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users tl ON tl.id = t.lead_id AND tl.is_active AND tl.role = 'tl'
LEFT JOIN departments td ON td.id = t.department_id AND td.is_active
LEFT JOIN users th ON th.id = td.head_id AND th.is_active AND th.role = 'tl';
-- +goose StatementEnd
