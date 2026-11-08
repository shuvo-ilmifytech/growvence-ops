-- +goose Up
-- +goose StatementBegin
-- Leave is approved in two steps: the person's manager (usually their Team Lead), then HR.
-- pending = waiting for the manager; awaiting_hr = manager approved, waiting for HR's final decision.
ALTER TABLE leave_requests DROP CONSTRAINT leave_requests_status_check;
ALTER TABLE leave_requests ADD CONSTRAINT leave_requests_status_check
    CHECK (status IN ('pending', 'awaiting_hr', 'approved', 'rejected', 'cancelled'));
ALTER TABLE leave_requests
    ADD COLUMN manager_reviewed_by uuid REFERENCES users (id),
    ADD COLUMN manager_reviewed_at timestamptz,
    ADD COLUMN manager_note        text NOT NULL DEFAULT '' CHECK (length(manager_note) <= 1000);

DROP INDEX leave_requests_dates_idx;
CREATE INDEX leave_requests_dates_idx ON leave_requests (start_date, end_date)
    WHERE status IN ('pending', 'awaiting_hr', 'approved');

DROP VIEW leave_request_details;
CREATE VIEW leave_request_details AS
SELECT
    l.id, l.user_id, u.name AS user_name, l.leave_type_id, t.name::text AS leave_type_name, t.is_paid,
    l.start_date::text AS start_date, l.end_date::text AS end_date, l.half_day, l.days::text AS days,
    l.reason, l.status, l.reviewed_by, r.name AS reviewed_by_name, l.reviewed_at, l.review_note,
    l.cancelled_by, l.created_at, l.updated_at,
    l.manager_reviewed_by, mr.name AS manager_reviewed_by_name, l.manager_reviewed_at, l.manager_note
FROM leave_requests l
JOIN users u ON u.id = l.user_id
JOIN leave_types t ON t.id = l.leave_type_id
LEFT JOIN users r ON r.id = l.reviewed_by
LEFT JOIN users mr ON mr.id = l.manager_reviewed_by;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW leave_request_details;
CREATE VIEW leave_request_details AS
SELECT
    l.id, l.user_id, u.name AS user_name, l.leave_type_id, t.name::text AS leave_type_name, t.is_paid,
    l.start_date::text AS start_date, l.end_date::text AS end_date, l.half_day, l.days::text AS days,
    l.reason, l.status, l.reviewed_by, r.name AS reviewed_by_name, l.reviewed_at, l.review_note,
    l.cancelled_by, l.created_at, l.updated_at
FROM leave_requests l
JOIN users u ON u.id = l.user_id
JOIN leave_types t ON t.id = l.leave_type_id
LEFT JOIN users r ON r.id = l.reviewed_by;
UPDATE leave_requests SET status = 'pending' WHERE status = 'awaiting_hr';
DROP INDEX leave_requests_dates_idx;
CREATE INDEX leave_requests_dates_idx ON leave_requests (start_date, end_date) WHERE status IN ('pending', 'approved');
ALTER TABLE leave_requests DROP COLUMN manager_reviewed_by, DROP COLUMN manager_reviewed_at, DROP COLUMN manager_note;
ALTER TABLE leave_requests DROP CONSTRAINT leave_requests_status_check;
ALTER TABLE leave_requests ADD CONSTRAINT leave_requests_status_check
    CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled'));
-- +goose StatementEnd
