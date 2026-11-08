-- +goose Up
-- +goose StatementBegin
-- The Fiverr-style project flow (2026-10-30):
--   New → In Progress → In Review (TL check) → Ready to Deliver → Delivered → Completed,
--   with Blocked, revisions back to In Progress, and Cancelled from any open step.
-- A project starts as New (no drafts: Sales adds the order and it goes straight to its team),
-- and assigning it starts it. Status history keeps the old values as they were.
ALTER TABLE projects
    DROP CONSTRAINT projects_status_check,
    DROP CONSTRAINT projects_required_when_submitted,
    DROP CONSTRAINT projects_assignee_when_active;

-- When it was last marked Delivered: "delivered" counts on the dashboards use this.
ALTER TABLE projects ADD COLUMN delivered_at timestamptz;
UPDATE projects SET delivered_at = completed_at WHERE status = 'completed';

-- Projects on the old statuses: drafts missing a required field are cancelled (they could
-- never be valid New projects); other drafts, submitted and needs-info become New; assigned
-- becomes In Progress.
CREATE TEMP TABLE flow_moves ON COMMIT DROP AS
SELECT id, created_by, status AS from_status,
       CASE
           WHEN status = 'draft' AND NOT (
               name IS NOT NULL AND btrim(name) <> '' AND profile_id IS NOT NULL AND
               client_name IS NOT NULL AND btrim(client_name) <> '' AND
               order_id IS NOT NULL AND btrim(order_id) <> '' AND project_type_id IS NOT NULL AND
               scope IS NOT NULL AND btrim(scope) <> '' AND agreed_price IS NOT NULL AND deadline IS NOT NULL
           ) THEN 'cancelled'
           WHEN status IN ('draft', 'submitted', 'needs_info') THEN 'new'
           ELSE 'in_progress'
       END AS to_status
FROM projects
WHERE status IN ('draft', 'submitted', 'needs_info', 'assigned');

INSERT INTO project_status_history (project_id, from_status, to_status, changed_by, comment)
SELECT id, from_status, to_status, created_by,
       CASE WHEN to_status = 'cancelled' THEN 'Incomplete draft closed when the project flow changed'
            ELSE 'Moved to the new project flow' END
FROM flow_moves;

UPDATE projects p SET
    status = m.to_status,
    cancelled_at = CASE WHEN m.to_status = 'cancelled' THEN now() ELSE p.cancelled_at END,
    first_submitted_at = CASE WHEN m.to_status = 'cancelled' THEN p.first_submitted_at
                              ELSE coalesce(p.first_submitted_at, now()) END
FROM flow_moves m
WHERE m.id = p.id;

ALTER TABLE projects ALTER COLUMN status SET DEFAULT 'new';

ALTER TABLE projects
    ADD CONSTRAINT projects_status_check CHECK (status IN (
        'new', 'in_progress', 'blocked', 'ready_for_review', 'ready_to_deliver', 'delivered', 'completed', 'cancelled')),
    -- Every project but a cancelled one has its required fields (the API validates first).
    ADD CONSTRAINT projects_required_when_submitted CHECK (
        status = 'cancelled' OR (
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
    ADD CONSTRAINT projects_assignee_when_active CHECK (
        status NOT IN ('in_progress', 'blocked', 'ready_for_review', 'ready_to_deliver')
        OR assigned_to IS NOT NULL
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE projects
    DROP CONSTRAINT projects_status_check,
    DROP CONSTRAINT projects_required_when_submitted,
    DROP CONSTRAINT projects_assignee_when_active;
UPDATE projects SET status = CASE status
    WHEN 'new' THEN CASE WHEN assigned_to IS NULL THEN 'submitted' ELSE 'assigned' END
    WHEN 'ready_to_deliver' THEN 'ready_for_review'
    WHEN 'delivered' THEN 'ready_for_review'
    ELSE status END;
ALTER TABLE projects ALTER COLUMN status SET DEFAULT 'draft';
ALTER TABLE projects DROP COLUMN delivered_at;
ALTER TABLE projects
    ADD CONSTRAINT projects_status_check CHECK (status IN ('draft', 'submitted', 'needs_info', 'assigned', 'in_progress',
        'blocked', 'ready_for_review', 'completed', 'cancelled')),
    ADD CONSTRAINT projects_required_when_submitted CHECK (
        status IN ('draft', 'cancelled') OR (
            name IS NOT NULL AND btrim(name) <> '' AND profile_id IS NOT NULL AND
            client_name IS NOT NULL AND btrim(client_name) <> '' AND
            order_id IS NOT NULL AND btrim(order_id) <> '' AND project_type_id IS NOT NULL AND
            scope IS NOT NULL AND btrim(scope) <> '' AND agreed_price IS NOT NULL AND deadline IS NOT NULL
        )
    ),
    ADD CONSTRAINT projects_assignee_when_active CHECK (
        status NOT IN ('assigned', 'in_progress', 'blocked', 'ready_for_review') OR assigned_to IS NOT NULL
    );
-- +goose StatementEnd
