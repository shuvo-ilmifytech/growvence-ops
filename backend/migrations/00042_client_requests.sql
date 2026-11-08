-- +goose Up
-- +goose StatementBegin
-- Client requests without a project (2026-11-02).
--
-- Not every client the team works for has a project in the portal: some are on monthly
-- management, and others were delivered before the portal existed. A project can't stand in
-- for them — it needs an order ID, a type, a price and a deadline, and Sales shouldn't
-- invent those just to log a change the client asked for.
--
-- So the request itself is the container. Sales raise it with the profile it goes out on,
-- the client's name, the order ID if there still is one, what the client asked for, and the
-- team that should do it. From there everything is the normal flow: the team sees its cards
-- on the board, writes the client updates on it, the Team Leader checks them and Sales post
-- them. A request is never a project: it has no price or deadline and stays out of the
-- project list, the reports, analytics and revenue.
CREATE TABLE client_requests (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number           bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    -- The marketplace profile the update will go out on: that's what Sales pick first.
    profile_id       uuid NOT NULL REFERENCES marketplace_profiles (id),
    client_name      text NOT NULL CHECK (length(btrim(client_name)) BETWEEN 1 AND 200),
    -- Optional: an old order that is long closed, or a client with no order at all.
    order_id         text CHECK (length(order_id) <= 100),
    title            text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    details          text NOT NULL DEFAULT '' CHECK (length(details) <= 20000),
    -- Sales choose the team, because there is no project type to route it by.
    team_id          uuid NOT NULL REFERENCES teams (id),
    status           text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done', 'cancelled')),
    created_by       uuid NOT NULL REFERENCES users (id),
    closed_by        uuid REFERENCES users (id),
    closed_at        timestamptz,
    last_activity_at timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT client_requests_closed_fields CHECK (
        status = 'open' OR (closed_by IS NOT NULL AND closed_at IS NOT NULL)
    )
);
CREATE INDEX client_requests_team_idx ON client_requests (team_id, status);
CREATE INDEX client_requests_sales_idx ON client_requests (created_by, created_at DESC);
CREATE INDEX client_requests_open_idx ON client_requests (last_activity_at DESC) WHERE status = 'open';

-- ── Cards, updates and files hang on either container ──────
-- A card, a client update and a file now belong to a project or to a client request,
-- never to both and never to neither.

ALTER TABLE project_cards
    ALTER COLUMN project_id DROP NOT NULL,
    ADD COLUMN client_request_id uuid REFERENCES client_requests (id),
    ADD CONSTRAINT project_cards_one_container CHECK (num_nonnulls(project_id, client_request_id) = 1);
-- container_id is whichever of the two it is, so the board's numbering, ordering and
-- queries stay one set of SQL instead of one per container.
ALTER TABLE project_cards
    ADD COLUMN container_id uuid GENERATED ALWAYS AS (coalesce(project_id, client_request_id)) STORED;
ALTER TABLE project_cards DROP CONSTRAINT project_cards_project_id_number_key;
CREATE UNIQUE INDEX project_cards_container_number ON project_cards (container_id, number);
DROP INDEX project_cards_board_idx;
CREATE INDEX project_cards_board_idx ON project_cards (container_id, column_key, position) WHERE removed_at IS NULL;

ALTER TABLE updates
    ALTER COLUMN project_id DROP NOT NULL,
    ADD COLUMN client_request_id uuid REFERENCES client_requests (id),
    ADD CONSTRAINT updates_one_container CHECK (num_nonnulls(project_id, client_request_id) = 1),
    -- A request has no order to deliver on, so its updates go to the inbox or the order
    -- page. Deliveries (and the revision count that goes with them) stay with projects.
    ADD CONSTRAINT updates_request_destination CHECK (
        client_request_id IS NULL OR destination <> 'delivery'
    );
CREATE INDEX updates_request_idx ON updates (client_request_id) WHERE client_request_id IS NOT NULL;

ALTER TABLE files
    ALTER COLUMN project_id DROP NOT NULL,
    ADD COLUMN client_request_id uuid REFERENCES client_requests (id),
    ADD CONSTRAINT files_one_container CHECK (num_nonnulls(project_id, client_request_id) = 1);
CREATE INDEX files_request_idx ON files (client_request_id) WHERE client_request_id IS NOT NULL;

-- Notifications and the activity log name the request the same way they name a project,
-- so a request's news reaches people and its trail reads back on its own page.
ALTER TABLE notifications ADD COLUMN client_request_id uuid REFERENCES client_requests (id);
ALTER TABLE activity_log ADD COLUMN client_request_id uuid REFERENCES client_requests (id);
CREATE INDEX activity_log_request_idx ON activity_log (client_request_id, id DESC) WHERE client_request_id IS NOT NULL;

-- ── Read models ────────────────────────────────────────────

-- One row per client request, shaped like project_details so the same rules can read it:
-- who may see it (its team's lead, its department's head), and who works it.
CREATE VIEW client_request_details AS
SELECT
    r.id,
    r.number,
    r.profile_id,
    mp.name::text   AS profile_name,
    r.client_name,
    r.order_id,
    r.title,
    r.details,
    r.status,
    r.team_id,
    t.name::text    AS team_name,
    tl.id           AS team_lead_id,
    th.id           AS team_head_id,
    r.created_by,
    cu.name         AS created_by_name,
    r.closed_by,
    xu.name         AS closed_by_name,
    r.closed_at,
    r.last_activity_at,
    r.created_at,
    r.updated_at,
    -- Whoever the Team Leader gave the work to. These are the request's members: like a
    -- project's, they are the people who write its client updates.
    coalesce((SELECT array_agg(DISTINCT a.user_id) FROM card_assignees a
              JOIN project_cards c ON c.id = a.card_id
              WHERE c.client_request_id = r.id AND c.removed_at IS NULL), '{}')::uuid[] AS member_ids,
    coalesce((SELECT array_agg(DISTINCT au.name) FROM card_assignees a
              JOIN project_cards c ON c.id = a.card_id
              JOIN users au ON au.id = a.user_id
              WHERE c.client_request_id = r.id AND c.removed_at IS NULL), '{}')::text[] AS member_names,
    (SELECT count(*) FROM project_cards c
      WHERE c.client_request_id = r.id AND c.removed_at IS NULL) AS card_count,
    (SELECT count(*) FROM project_cards c
      WHERE c.client_request_id = r.id AND c.removed_at IS NULL AND c.column_key = 'done') AS done_card_count,
    (SELECT count(*) FROM updates u WHERE u.client_request_id = r.id) AS update_count,
    (SELECT count(*) FROM updates u
      WHERE u.client_request_id = r.id AND u.status = 'approved') AS unsent_update_count
FROM client_requests r
JOIN marketplace_profiles mp ON mp.id = r.profile_id
JOIN teams t ON t.id = r.team_id
JOIN users cu ON cu.id = r.created_by
LEFT JOIN users xu ON xu.id = r.closed_by
LEFT JOIN users tl ON tl.id = t.lead_id AND tl.is_active AND tl.role = 'tl'
LEFT JOIN departments td ON td.id = t.department_id AND td.is_active
LEFT JOIN users th ON th.id = td.head_id AND th.is_active AND th.role = 'tl';

-- card_details gains its container.
DROP VIEW card_details;
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

-- update_details carries whichever container the update is on. The project columns stay as
-- they are for a project's update; for a request's they are filled from the request, so
-- every rule about updates (who sees, writes, approves and sends them) reads one shape:
--   · client_name / order_id / profile_name — what the update says it is going to
--   · project_status — an open request is work under way, a closed one is delivered work
--   · project_sales_id / project_member_ids / project_team_* — who it belongs to
DROP VIEW update_details;
CREATE VIEW update_details AS
SELECT
    u.id,
    u.number,
    u.project_id,
    u.client_request_id,
    coalesce(p.number, r.number)                 AS project_number,
    coalesce(p.name, r.title)                    AS project_name,
    coalesce(p.order_id, r.order_id)             AS order_id,
    coalesce(pmp.name, rmp.name)::text           AS profile_name,
    coalesce(p.client_name, r.client_name)       AS client_name,
    coalesce(p.status, CASE r.status WHEN 'open' THEN 'in_progress'
                                    WHEN 'done' THEN 'delivered'
                                    ELSE 'cancelled' END) AS project_status,
    coalesce(p.created_by, r.created_by)         AS project_sales_id,
    p.assigned_to                                AS project_assignee_id,
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
    coalesce(p.team_id, r.team_id)   AS project_team_id,
    coalesce(ptl.id, rv.team_lead_id) AS project_team_lead_id,
    coalesce((SELECT array_agg(m.user_id) FROM project_members m WHERE m.project_id = p.id),
             rv.member_ids, '{}')::uuid[] AS project_member_ids,
    coalesce(pth.id, rv.team_head_id) AS project_team_head_id
FROM updates u
JOIN users au ON au.id = u.author_id
LEFT JOIN projects p ON p.id = u.project_id
LEFT JOIN client_requests r ON r.id = u.client_request_id
LEFT JOIN client_request_details rv ON rv.id = u.client_request_id
LEFT JOIN users cu ON cu.id = u.checked_by
LEFT JOIN users su ON su.id = u.sent_by
LEFT JOIN marketplace_profiles pmp ON pmp.id = p.profile_id
LEFT JOIN marketplace_profiles rmp ON rmp.id = r.profile_id
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users ptl ON ptl.id = t.lead_id AND ptl.is_active AND ptl.role = 'tl'
LEFT JOIN departments td ON td.id = t.department_id AND td.is_active
LEFT JOIN users pth ON pth.id = td.head_id AND pth.is_active AND pth.role = 'tl';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW update_details;
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

DROP VIEW card_details;
CREATE VIEW card_details AS
SELECT c.id, c.project_id, c.number, c.title, c.description, c.source, c.column_key, c.position,
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

DROP VIEW client_request_details;

-- Anything that hung on a request goes with it.
DELETE FROM file_versions WHERE file_id IN (SELECT id FROM files WHERE client_request_id IS NOT NULL);
UPDATE files SET current_version_id = NULL WHERE client_request_id IS NOT NULL;
DELETE FROM files WHERE client_request_id IS NOT NULL;
DELETE FROM update_comments WHERE update_id IN (SELECT id FROM updates WHERE client_request_id IS NOT NULL);
DELETE FROM update_status_history WHERE update_id IN (SELECT id FROM updates WHERE client_request_id IS NOT NULL);
DELETE FROM updates WHERE client_request_id IS NOT NULL;
DELETE FROM card_comments WHERE card_id IN (SELECT id FROM project_cards WHERE client_request_id IS NOT NULL);
DELETE FROM card_assignees WHERE card_id IN (SELECT id FROM project_cards WHERE client_request_id IS NOT NULL);
DELETE FROM card_views WHERE card_id IN (SELECT id FROM project_cards WHERE client_request_id IS NOT NULL);
DELETE FROM project_cards WHERE client_request_id IS NOT NULL;

DELETE FROM notifications WHERE client_request_id IS NOT NULL;
DELETE FROM activity_log WHERE client_request_id IS NOT NULL;
DROP INDEX activity_log_request_idx;
ALTER TABLE activity_log DROP COLUMN client_request_id;
ALTER TABLE notifications DROP COLUMN client_request_id;

ALTER TABLE files
    DROP CONSTRAINT files_one_container,
    DROP COLUMN client_request_id,
    ALTER COLUMN project_id SET NOT NULL;

ALTER TABLE updates
    DROP CONSTRAINT updates_request_destination,
    DROP CONSTRAINT updates_one_container,
    DROP COLUMN client_request_id,
    ALTER COLUMN project_id SET NOT NULL;

DROP INDEX project_cards_board_idx;
DROP INDEX project_cards_container_number;
ALTER TABLE project_cards
    DROP COLUMN container_id,
    DROP CONSTRAINT project_cards_one_container,
    DROP COLUMN client_request_id,
    ALTER COLUMN project_id SET NOT NULL,
    ADD CONSTRAINT project_cards_project_id_number_key UNIQUE (project_id, number);
CREATE INDEX project_cards_board_idx ON project_cards (project_id, column_key, position) WHERE removed_at IS NULL;

DROP TABLE client_requests;
-- +goose StatementEnd
