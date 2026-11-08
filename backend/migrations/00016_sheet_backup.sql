-- +goose Up
-- Google Sheet backup: one row per project, kept in step with the app.
--
-- A database trigger marks a project dirty whenever it changes, so nothing is missed
-- (CLI writes and manual SQL included) and a rolled-back transaction queues nothing.
-- The worker in the API drains the queue; with no sheet configured the queue simply
-- waits, so configuring one later backs up everything that happened in the meantime.

CREATE TABLE sheet_sync (
    project_id      uuid PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    row_number      integer,                            -- where it sits in the sheet, once written
    dirty           boolean NOT NULL DEFAULT true,
    queued_at       timestamptz NOT NULL DEFAULT now(),
    next_attempt_at timestamptz NOT NULL DEFAULT now(), -- backoff after a failure
    attempts        integer NOT NULL DEFAULT 0,
    last_error      text NOT NULL DEFAULT '',
    synced_at       timestamptz
);

CREATE INDEX sheet_sync_pending_idx ON sheet_sync (next_attempt_at) WHERE dirty;

-- +goose StatementBegin
CREATE FUNCTION mark_sheet_dirty() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- Drafts stay out of the sheet; a project joins it when Sales submit it.
    IF NEW.status = 'draft' THEN
        RETURN NEW;
    END IF;
    INSERT INTO sheet_sync (project_id) VALUES (NEW.id)
    ON CONFLICT (project_id) DO UPDATE SET
        dirty = true, queued_at = now(), next_attempt_at = now(), attempts = 0, last_error = '';
    RETURN NEW;
END $$;
-- +goose StatementEnd

-- Two triggers: a WHEN clause can't look at OLD on insert, and on update we only
-- care when something actually changed (not every touch of last_activity_at).
CREATE TRIGGER projects_sheet_sync_insert AFTER INSERT ON projects
    FOR EACH ROW EXECUTE FUNCTION mark_sheet_dirty();

CREATE TRIGGER projects_sheet_sync_update AFTER UPDATE ON projects
    FOR EACH ROW WHEN (OLD.* IS DISTINCT FROM NEW.*)
    EXECUTE FUNCTION mark_sheet_dirty();

-- Members are exported too, and they live in their own table.
-- +goose StatementBegin
CREATE FUNCTION mark_member_sheet_dirty() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    pid uuid := coalesce(NEW.project_id, OLD.project_id);
BEGIN
    INSERT INTO sheet_sync (project_id)
    SELECT pid FROM projects p WHERE p.id = pid AND p.status <> 'draft'
    ON CONFLICT (project_id) DO UPDATE SET
        dirty = true, queued_at = now(), next_attempt_at = now(), attempts = 0, last_error = '';
    RETURN NULL;
END $$;
-- +goose StatementEnd

CREATE TRIGGER project_members_sheet_sync AFTER INSERT OR DELETE ON project_members
    FOR EACH ROW EXECUTE FUNCTION mark_member_sheet_dirty();

-- Every project that already exists belongs in the sheet.
INSERT INTO sheet_sync (project_id)
SELECT id FROM projects WHERE status <> 'draft'
ON CONFLICT (project_id) DO NOTHING;

-- +goose Down
DROP TRIGGER project_members_sheet_sync ON project_members;
DROP FUNCTION mark_member_sheet_dirty();
DROP TRIGGER projects_sheet_sync_update ON projects;
DROP TRIGGER projects_sheet_sync_insert ON projects;
DROP FUNCTION mark_sheet_dirty();
DROP TABLE sheet_sync;
