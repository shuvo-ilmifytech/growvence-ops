-- +goose Up
-- +goose StatementBegin
-- Work shifts. People without a shift follow the default office hours in settings.
-- A shift whose end is at or before its start runs overnight; its work date is the day it starts.
CREATE TABLE shifts (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          citext NOT NULL UNIQUE CHECK (length(btrim(name)) BETWEEN 1 AND 60),
    start_time    time NOT NULL,
    end_time      time NOT NULL CHECK (end_time <> start_time),
    grace_minutes int NOT NULL DEFAULT 15 CHECK (grace_minutes BETWEEN 0 AND 240),
    off_days      int[] NOT NULL DEFAULT '{5,6}' CHECK (off_days <@ '{0,1,2,3,4,5,6}' AND cardinality(off_days) <= 6),
    is_active     boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE employees ADD COLUMN shift_id uuid REFERENCES shifts (id);
CREATE INDEX employees_shift_idx ON employees (shift_id);

DROP VIEW employee_details;
CREATE VIEW employee_details AS
SELECT
    u.id                       AS user_id,
    u.name,
    u.email,
    u.role,
    u.is_active,
    e.employee_code::text      AS employee_code,
    e.designation,
    e.department_id,
    d.name::text               AS department_name,
    e.manager_id,
    m.name                     AS manager_name,
    e.employment_type,
    e.join_date::text          AS join_date,
    e.phone,
    e.date_of_birth::text      AS date_of_birth,
    e.address,
    e.emergency_contact_name,
    e.emergency_contact_phone,
    e.national_id,
    e.updated_at,
    e.shift_id,
    s.name::text               AS shift_name
FROM users u
JOIN employees e ON e.user_id = u.id
LEFT JOIN departments d ON d.id = e.department_id
LEFT JOIN users m ON m.id = e.manager_id
LEFT JOIN shifts s ON s.id = e.shift_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW employee_details;
CREATE VIEW employee_details AS
SELECT
    u.id AS user_id, u.name, u.email, u.role, u.is_active,
    e.employee_code::text AS employee_code, e.designation, e.department_id, d.name::text AS department_name,
    e.manager_id, m.name AS manager_name, e.employment_type, e.join_date::text AS join_date, e.phone,
    e.date_of_birth::text AS date_of_birth, e.address, e.emergency_contact_name, e.emergency_contact_phone,
    e.national_id, e.updated_at
FROM users u
JOIN employees e ON e.user_id = u.id
LEFT JOIN departments d ON d.id = e.department_id
LEFT JOIN users m ON m.id = e.manager_id;
ALTER TABLE employees DROP COLUMN shift_id;
DROP TABLE shifts;
-- +goose StatementEnd
