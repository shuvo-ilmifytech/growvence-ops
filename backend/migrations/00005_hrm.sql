-- +goose Up
-- +goose StatementBegin
-- ── HRM: employee records ──────────────────────────────────
CREATE TABLE departments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       citext NOT NULL UNIQUE CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- One row per user (created by trigger), holding the HR side of the person.
-- manager_id is the reporting line: that person approves leave and attendance corrections.
CREATE TABLE employees (
    user_id                 uuid PRIMARY KEY REFERENCES users (id),
    employee_code           citext UNIQUE CHECK (length(btrim(employee_code)) BETWEEN 1 AND 30),
    designation             text NOT NULL DEFAULT '' CHECK (length(designation) <= 120),
    department_id           uuid REFERENCES departments (id),
    manager_id              uuid REFERENCES users (id) CHECK (manager_id <> user_id),
    employment_type         text NOT NULL DEFAULT 'full_time'
                            CHECK (employment_type IN ('full_time', 'part_time', 'intern', 'contract')),
    join_date               date,
    phone                   text NOT NULL DEFAULT '' CHECK (length(phone) <= 40),
    -- Restricted: visible to the person, admins and Management only.
    date_of_birth           date,
    address                 text NOT NULL DEFAULT '' CHECK (length(address) <= 500),
    emergency_contact_name  text NOT NULL DEFAULT '' CHECK (length(emergency_contact_name) <= 120),
    emergency_contact_phone text NOT NULL DEFAULT '' CHECK (length(emergency_contact_phone) <= 40),
    national_id             text NOT NULL DEFAULT '' CHECK (length(national_id) <= 40),
    updated_by              uuid REFERENCES users (id),
    updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX employees_manager_idx ON employees (manager_id);

CREATE FUNCTION create_employee_row() RETURNS trigger AS $$
BEGIN
    INSERT INTO employees (user_id) VALUES (NEW.id) ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_create_employee
AFTER INSERT ON users
FOR EACH ROW EXECUTE FUNCTION create_employee_row();

INSERT INTO employees (user_id) SELECT id FROM users;

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
    e.updated_at
FROM users u
JOIN employees e ON e.user_id = u.id
LEFT JOIN departments d ON d.id = e.department_id
LEFT JOIN users m ON m.id = e.manager_id;

-- ── HRM: attendance ────────────────────────────────────────
-- One row per person per working day (dates are in the organisation's timezone).
CREATE TABLE attendance (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id),
    work_date    date NOT NULL,
    check_in_at  timestamptz NOT NULL,
    check_out_at timestamptz CHECK (check_out_at > check_in_at),
    source       text NOT NULL DEFAULT 'self' CHECK (source IN ('self', 'correction', 'admin')),
    note         text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, work_date)
);
CREATE INDEX attendance_date_idx ON attendance (work_date);

-- "I forgot to check in/out": the person asks, their manager (or an admin) approves.
CREATE TABLE attendance_corrections (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id),
    work_date    date NOT NULL,
    check_in_at  timestamptz NOT NULL,
    check_out_at timestamptz CHECK (check_out_at > check_in_at),
    reason       text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    reviewed_by  uuid REFERENCES users (id),
    reviewed_at  timestamptz,
    review_note  text NOT NULL DEFAULT '' CHECK (length(review_note) <= 1000),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX attendance_corrections_user_idx ON attendance_corrections (user_id, created_at DESC);
CREATE UNIQUE INDEX attendance_corrections_one_pending_idx ON attendance_corrections (user_id, work_date)
    WHERE status = 'pending';

CREATE VIEW attendance_correction_details AS
SELECT
    c.id, c.user_id, u.name AS user_name, c.work_date::text AS work_date,
    c.check_in_at, c.check_out_at, c.reason, c.status,
    c.reviewed_by, r.name AS reviewed_by_name, c.reviewed_at, c.review_note, c.created_at
FROM attendance_corrections c
JOIN users u ON u.id = c.user_id
LEFT JOIN users r ON r.id = c.reviewed_by;

-- ── HRM: leave ─────────────────────────────────────────────
CREATE TABLE leave_types (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name         citext NOT NULL UNIQUE CHECK (length(btrim(name)) BETWEEN 1 AND 60),
    annual_quota numeric(5,1) NOT NULL DEFAULT 0 CHECK (annual_quota >= 0),
    is_limited   boolean NOT NULL DEFAULT true, -- false: no balance check (e.g. unpaid leave)
    is_paid      boolean NOT NULL DEFAULT true,
    is_active    boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now()
);

INSERT INTO leave_types (name, annual_quota, is_limited, is_paid) VALUES
    ('Casual', 10, true, true),
    ('Sick', 14, true, true),
    ('Annual', 15, true, true),
    ('Unpaid', 0, false, false);

-- Per-person, per-year override of a leave type's quota.
CREATE TABLE leave_allocations (
    user_id       uuid NOT NULL REFERENCES users (id),
    leave_type_id uuid NOT NULL REFERENCES leave_types (id),
    year          int NOT NULL CHECK (year BETWEEN 2000 AND 2100),
    days          numeric(5,1) NOT NULL CHECK (days >= 0),
    updated_by    uuid REFERENCES users (id),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, leave_type_id, year)
);

CREATE TABLE holidays (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    date       date NOT NULL UNIQUE,
    name       text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE leave_requests (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users (id),
    leave_type_id uuid NOT NULL REFERENCES leave_types (id),
    start_date    date NOT NULL,
    end_date      date NOT NULL,
    half_day      boolean NOT NULL DEFAULT false,
    days          numeric(5,1) NOT NULL CHECK (days > 0), -- working days, excluding weekly off days and holidays
    reason        text NOT NULL DEFAULT '' CHECK (length(reason) <= 1000),
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    reviewed_by   uuid REFERENCES users (id),
    reviewed_at   timestamptz,
    review_note   text NOT NULL DEFAULT '' CHECK (length(review_note) <= 1000),
    cancelled_by  uuid REFERENCES users (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (end_date >= start_date),
    CHECK (extract(year FROM start_date) = extract(year FROM end_date)),
    CHECK (NOT half_day OR start_date = end_date)
);
CREATE INDEX leave_requests_user_idx ON leave_requests (user_id, start_date);
CREATE INDEX leave_requests_dates_idx ON leave_requests (start_date, end_date) WHERE status IN ('pending', 'approved');

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

INSERT INTO settings (key, value) VALUES
    ('work_start',         '"09:00"'),
    ('work_end',           '"18:00"'),
    ('late_grace_minutes', '15'),
    ('weekly_off_days',    '[5, 6]'); -- 0 = Sunday … 6 = Saturday; default Friday + Saturday
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM settings WHERE key IN ('work_start', 'work_end', 'late_grace_minutes', 'weekly_off_days');
DROP VIEW leave_request_details, attendance_correction_details, employee_details;
DROP TABLE leave_requests, holidays, leave_allocations, leave_types,
    attendance_corrections, attendance;
DROP TRIGGER users_create_employee ON users;
DROP FUNCTION create_employee_row();
DROP TABLE employees, departments;
-- +goose StatementEnd
