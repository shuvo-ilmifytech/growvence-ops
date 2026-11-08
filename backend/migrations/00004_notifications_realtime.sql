-- +goose Up
-- +goose StatementBegin
-- Real-time delivery: NOTIFY fires only when the inserting/updating transaction commits,
-- so browsers never hear about rolled-back changes. Payload: "<recipient_id>:<notification_id>".
CREATE FUNCTION notify_notification() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('notifications', NEW.recipient_id::text || ':' || NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER notifications_notify
AFTER INSERT OR UPDATE OF body ON notifications
FOR EACH ROW EXECUTE FUNCTION notify_notification();

-- Finding an open batch (same recipient, project and type, still unread).
CREATE INDEX notifications_batch_idx ON notifications (recipient_id, project_id, type, created_at)
    WHERE read_at IS NULL;

-- Scheduled checks.
CREATE INDEX projects_unassigned_idx ON projects (status) WHERE status = 'submitted' AND unassigned_alert_at IS NULL;
-- +goose StatementEnd

-- +goose Down
DROP INDEX projects_unassigned_idx;
DROP INDEX notifications_batch_idx;
DROP TRIGGER notifications_notify ON notifications;
DROP FUNCTION notify_notification();
