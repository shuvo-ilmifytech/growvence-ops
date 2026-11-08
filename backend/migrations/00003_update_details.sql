-- +goose Up
-- +goose StatementBegin
-- Read model for updates: one row per update with the columns of the old Google Sheet.
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
    (SELECT count(*) FROM update_comments c WHERE c.update_id = u.id AND c.thread = 'sales') AS sales_comment_count
FROM updates u
JOIN projects p ON p.id = u.project_id
JOIN users au ON au.id = u.author_id
LEFT JOIN users cu ON cu.id = u.checked_by
LEFT JOIN users su ON su.id = u.sent_by
LEFT JOIN marketplace_profiles mp ON mp.id = p.profile_id;
-- +goose StatementEnd

-- +goose Down
DROP VIEW update_details;
