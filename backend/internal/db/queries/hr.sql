-- ── Departments ────────────────────────────────────────────

-- name: ListDepartments :many
SELECT * FROM departments
WHERE (NOT sqlc.arg('active_only')::boolean OR is_active)
ORDER BY name;

-- name: GetDepartment :one
SELECT * FROM departments WHERE id = $1;

-- name: CreateDepartment :one
INSERT INTO departments (name) VALUES ($1) RETURNING *;

-- name: UpdateDepartment :one
UPDATE departments SET
    name      = coalesce(sqlc.narg('name'), name),
    is_active = coalesce(sqlc.narg('is_active'), is_active),
    head_id   = CASE WHEN sqlc.arg('set_head')::boolean THEN sqlc.narg('head_id') ELSE head_id END
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: TeamsRunBy :many
-- Active teams a Team Leader runs: the ones they lead and every team in a department they head.
SELECT t.id, t.name FROM teams t
LEFT JOIN departments d ON d.id = t.department_id AND d.is_active
WHERE t.is_active AND (t.lead_id = sqlc.arg('user_id')::uuid OR d.head_id = sqlc.arg('user_id')::uuid)
ORDER BY t.name;

-- ── Employees ──────────────────────────────────────────────

-- name: GetEmployee :one
SELECT * FROM employee_details WHERE user_id = $1;

-- name: ListEmployees :many
SELECT * FROM employee_details
WHERE (sqlc.narg('is_active')::boolean IS NULL OR is_active = sqlc.narg('is_active'))
ORDER BY name;

-- name: LockEmployee :exec
-- Serialises leave/correction writes per person (overlap and balance checks).
SELECT 1 FROM employees WHERE user_id = $1 FOR UPDATE;

-- name: UpdateEmployeeHR :exec
UPDATE employees SET
    employee_code           = sqlc.narg('employee_code'),
    designation             = sqlc.arg('designation'),
    department_id           = sqlc.narg('department_id'),
    manager_id              = sqlc.narg('manager_id'),
    employment_type         = sqlc.arg('employment_type'),
    join_date               = sqlc.narg('join_date')::text::date,
    phone                   = sqlc.arg('phone'),
    date_of_birth           = sqlc.narg('date_of_birth')::text::date,
    address                 = sqlc.arg('address'),
    emergency_contact_name  = sqlc.arg('emergency_contact_name'),
    emergency_contact_phone = sqlc.arg('emergency_contact_phone'),
    national_id             = sqlc.arg('national_id'),
    shift_id                = sqlc.narg('shift_id'),
    updated_by              = sqlc.arg('updated_by'),
    updated_at              = now()
WHERE user_id = sqlc.arg('user_id')::uuid;

-- name: UpdateEmployeePersonal :exec
UPDATE employees SET
    phone                   = sqlc.arg('phone'),
    address                 = sqlc.arg('address'),
    emergency_contact_name  = sqlc.arg('emergency_contact_name'),
    emergency_contact_phone = sqlc.arg('emergency_contact_phone'),
    updated_by              = sqlc.arg('user_id')::uuid,
    updated_at              = now()
WHERE user_id = sqlc.arg('user_id')::uuid;

-- name: ManagerChain :many
-- Everyone above the person in the reporting line (UNION stops on a cycle).
WITH RECURSIVE up AS (
    SELECT e.manager_id FROM employees e WHERE e.user_id = $1
    UNION
    SELECT e.manager_id FROM employees e JOIN up ON e.user_id = up.manager_id
)
SELECT manager_id::uuid FROM up WHERE manager_id IS NOT NULL;

-- name: ReportIDs :many
-- Everyone below the person in the reporting line, direct and indirect.
WITH RECURSIVE down AS (
    SELECT e.user_id FROM employees e WHERE e.manager_id = sqlc.arg('manager_id')::uuid
    UNION
    SELECT e.user_id FROM employees e JOIN down d ON e.manager_id = d.user_id
)
SELECT user_id FROM down;

-- ── Attendance ─────────────────────────────────────────────

-- name: CheckIn :one
INSERT INTO attendance (user_id, work_date, check_in_at, note)
VALUES (sqlc.arg('user_id'), sqlc.arg('work_date')::text::date, now(), sqlc.arg('note'))
ON CONFLICT (user_id, work_date) DO NOTHING
RETURNING id;

-- name: CheckOut :one
-- Closes the person's open day, if it started within the last 20 hours (covers late shifts).
UPDATE attendance SET check_out_at = now(), updated_at = now(),
    note = CASE WHEN sqlc.arg('note')::text = '' THEN note ELSE sqlc.arg('note')::text END
WHERE id = (
    SELECT a.id FROM attendance a
    WHERE a.user_id = sqlc.arg('user_id') AND a.check_out_at IS NULL
      AND a.check_in_at > now() - interval '20 hours'
    ORDER BY a.check_in_at DESC LIMIT 1
)
RETURNING id;

-- name: UpsertAttendance :exec
INSERT INTO attendance (user_id, work_date, check_in_at, check_out_at, source, note)
VALUES (sqlc.arg('user_id'), sqlc.arg('work_date')::text::date, sqlc.arg('check_in_at'),
        sqlc.narg('check_out_at'), sqlc.arg('source'), sqlc.arg('note'))
ON CONFLICT (user_id, work_date) DO UPDATE SET
    check_in_at = EXCLUDED.check_in_at, check_out_at = EXCLUDED.check_out_at,
    source = EXCLUDED.source, note = EXCLUDED.note, updated_at = now();

-- name: ListAttendance :many
SELECT a.id, a.user_id, u.name AS user_name, a.work_date::text AS work_date,
       a.check_in_at, a.check_out_at, a.source, a.note
FROM attendance a
JOIN users u ON u.id = a.user_id
WHERE a.user_id = ANY (sqlc.arg('user_ids')::uuid[])
  AND a.work_date BETWEEN sqlc.arg('from_date')::text::date AND sqlc.arg('to_date')::text::date
ORDER BY a.work_date, u.name;

-- ── Attendance corrections ─────────────────────────────────

-- name: CreateCorrection :one
INSERT INTO attendance_corrections (user_id, work_date, check_in_at, check_out_at, reason)
VALUES (sqlc.arg('user_id'), sqlc.arg('work_date')::text::date, sqlc.arg('check_in_at'),
        sqlc.narg('check_out_at'), sqlc.arg('reason'))
RETURNING id;

-- name: GetCorrection :one
SELECT * FROM attendance_correction_details WHERE id = $1;

-- name: ListCorrections :many
SELECT * FROM attendance_correction_details
WHERE (sqlc.narg('user_ids')::uuid[] IS NULL OR user_id = ANY (sqlc.narg('user_ids')::uuid[]))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT 200;

-- name: SetCorrectionStatus :exec
UPDATE attendance_corrections SET
    status = sqlc.arg('status'), reviewed_by = sqlc.narg('reviewed_by'),
    reviewed_at = CASE WHEN sqlc.narg('reviewed_by')::uuid IS NULL THEN NULL ELSE now() END,
    review_note = sqlc.arg('review_note')
WHERE id = sqlc.arg('id');

-- ── Leave types, allocations, holidays ─────────────────────

-- name: ListLeaveTypes :many
SELECT id, name::text AS name, annual_quota::text AS annual_quota, is_limited, is_paid, is_active, created_at
FROM leave_types
WHERE (NOT sqlc.arg('active_only')::boolean OR is_active)
ORDER BY name;

-- name: GetLeaveType :one
SELECT id, name::text AS name, annual_quota::text AS annual_quota, is_limited, is_paid, is_active, created_at
FROM leave_types WHERE id = $1;

-- name: CreateLeaveType :one
INSERT INTO leave_types (name, annual_quota, is_limited, is_paid)
VALUES (sqlc.arg('name'), sqlc.arg('annual_quota')::text::numeric, sqlc.arg('is_limited'), sqlc.arg('is_paid'))
RETURNING id;

-- name: UpdateLeaveType :one
UPDATE leave_types SET
    name         = coalesce(sqlc.narg('name'), name),
    annual_quota = coalesce(sqlc.narg('annual_quota')::text::numeric, annual_quota),
    is_limited   = coalesce(sqlc.narg('is_limited'), is_limited),
    is_paid      = coalesce(sqlc.narg('is_paid'), is_paid),
    is_active    = coalesce(sqlc.narg('is_active'), is_active)
WHERE id = sqlc.arg('id')
RETURNING id;

-- name: UpsertAllocation :exec
INSERT INTO leave_allocations (user_id, leave_type_id, year, days, updated_by)
VALUES (sqlc.arg('user_id'), sqlc.arg('leave_type_id'), sqlc.arg('year'), sqlc.arg('days')::text::numeric, sqlc.arg('updated_by'))
ON CONFLICT (user_id, leave_type_id, year) DO UPDATE SET
    days = EXCLUDED.days, updated_by = EXCLUDED.updated_by, updated_at = now();

-- name: DeleteAllocation :exec
DELETE FROM leave_allocations WHERE user_id = $1 AND leave_type_id = $2 AND year = $3;

-- name: LeaveBalances :many
-- Per active leave type for one person and year: entitlement (override or quota), approved and pending days.
SELECT t.id AS leave_type_id, t.name::text AS leave_type_name, t.is_limited, t.is_paid,
       coalesce(a.days, t.annual_quota)::text AS entitled,
       (a.days IS NOT NULL)::boolean AS is_override,
       coalesce(sum(l.days) FILTER (WHERE l.status = 'approved'), 0)::text AS approved,
       coalesce(sum(l.days) FILTER (WHERE l.status IN ('pending', 'awaiting_hr')), 0)::text AS pending
FROM leave_types t
LEFT JOIN leave_allocations a ON a.leave_type_id = t.id AND a.user_id = sqlc.arg('user_id') AND a.year = sqlc.arg('year')
LEFT JOIN leave_requests l ON l.leave_type_id = t.id AND l.user_id = sqlc.arg('user_id')
     AND extract(year FROM l.start_date) = sqlc.arg('year')::int AND l.status IN ('approved', 'pending', 'awaiting_hr')
WHERE t.is_active
GROUP BY t.id, a.days
ORDER BY t.name;

-- name: ListHolidays :many
SELECT id, date::text AS date, name, created_at FROM holidays
WHERE date BETWEEN sqlc.arg('from_date')::text::date AND sqlc.arg('to_date')::text::date
ORDER BY date;

-- name: CreateHoliday :one
INSERT INTO holidays (date, name, created_by) VALUES (sqlc.arg('date')::text::date, sqlc.arg('name'), sqlc.arg('created_by'))
RETURNING id, date::text AS date, name, created_at;

-- name: DeleteHoliday :one
DELETE FROM holidays WHERE id = $1 RETURNING date::text AS date, name;

-- ── Leave requests ─────────────────────────────────────────

-- name: CreateLeaveRequest :one
INSERT INTO leave_requests (user_id, leave_type_id, start_date, end_date, half_day, days, reason, status)
VALUES (sqlc.arg('user_id'), sqlc.arg('leave_type_id'), sqlc.arg('start_date')::text::date,
        sqlc.arg('end_date')::text::date, sqlc.arg('half_day'), sqlc.arg('days')::text::numeric, sqlc.arg('reason'),
        sqlc.arg('status'))
RETURNING id;

-- name: SetLeaveManagerApproved :exec
-- Step 1 of 2: the manager approved; HR makes the final decision.
UPDATE leave_requests SET
    status = 'awaiting_hr', manager_reviewed_by = sqlc.arg('reviewed_by'), manager_reviewed_at = now(),
    manager_note = sqlc.arg('note'), updated_at = now()
WHERE id = sqlc.arg('id') AND status = 'pending';

-- name: GetLeaveRequest :one
SELECT * FROM leave_request_details WHERE id = $1;

-- name: ListLeaveRequests :many
SELECT * FROM leave_request_details
WHERE (sqlc.narg('user_ids')::uuid[] IS NULL OR user_id = ANY (sqlc.narg('user_ids')::uuid[]))
  AND (sqlc.narg('statuses')::text[] IS NULL OR status = ANY (sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('from_date')::text IS NULL OR end_date::date >= sqlc.narg('from_date')::text::date)
  AND (sqlc.narg('to_date')::text IS NULL OR start_date::date <= sqlc.narg('to_date')::text::date)
ORDER BY start_date::date DESC, created_at DESC
LIMIT 500;

-- name: CountOverlappingLeave :one
SELECT count(*) FROM leave_requests
WHERE user_id = sqlc.arg('user_id') AND status IN ('pending', 'awaiting_hr', 'approved')
  AND start_date <= sqlc.arg('end_date')::text::date AND end_date >= sqlc.arg('start_date')::text::date;

-- name: SetLeaveStatus :exec
UPDATE leave_requests SET
    status       = sqlc.arg('status'),
    reviewed_by  = coalesce(sqlc.narg('reviewed_by'), reviewed_by),
    reviewed_at  = CASE WHEN sqlc.narg('reviewed_by')::uuid IS NULL THEN reviewed_at ELSE now() END,
    review_note  = CASE WHEN sqlc.narg('reviewed_by')::uuid IS NULL THEN review_note ELSE sqlc.arg('review_note') END,
    cancelled_by = sqlc.narg('cancelled_by'),
    updated_at   = now()
WHERE id = sqlc.arg('id');

-- ── Shifts ─────────────────────────────────────────────────

-- name: ListShifts :many
SELECT s.id, s.name::text AS name, to_char(s.start_time, 'HH24:MI') AS start_time, to_char(s.end_time, 'HH24:MI') AS end_time,
       s.grace_minutes, s.off_days, s.is_active, s.created_at,
       (SELECT count(*) FROM employees e JOIN users u ON u.id = e.user_id WHERE e.shift_id = s.id AND u.is_active) AS member_count
FROM shifts s
WHERE (NOT sqlc.arg('active_only')::boolean OR s.is_active)
ORDER BY s.start_time, s.name;

-- name: CreateShift :one
INSERT INTO shifts (name, start_time, end_time, grace_minutes, off_days)
VALUES (sqlc.arg('name'), sqlc.arg('start_time')::text::time, sqlc.arg('end_time')::text::time,
        sqlc.arg('grace_minutes'), sqlc.arg('off_days')::int[])
RETURNING id;

-- name: UpdateShift :one
UPDATE shifts SET
    name          = coalesce(sqlc.narg('name'), name),
    start_time    = coalesce(sqlc.narg('start_time')::text::time, start_time),
    end_time      = coalesce(sqlc.narg('end_time')::text::time, end_time),
    grace_minutes = coalesce(sqlc.narg('grace_minutes'), grace_minutes),
    off_days      = coalesce(sqlc.narg('off_days')::int[], off_days),
    is_active     = coalesce(sqlc.narg('is_active'), is_active)
WHERE id = sqlc.arg('id')
RETURNING id;

-- name: AssignShift :execrows
-- Puts people on a shift (or back on the default office hours with a null shift).
UPDATE employees SET shift_id = sqlc.narg('shift_id'), updated_by = sqlc.arg('updated_by'), updated_at = now()
WHERE user_id = ANY (sqlc.arg('user_ids')::uuid[]);

-- ── Salaries (HR + CEO only, never the person's own) ───────

-- name: SalaryHistory :many
SELECT s.id, s.user_id, s.amount::text AS amount, s.currency, s.effective_from::text AS effective_from, s.note,
       s.created_at, c.name AS created_by_name
FROM salaries s
JOIN users c ON c.id = s.created_by
WHERE s.user_id = $1
ORDER BY s.effective_from DESC;

-- name: CurrentSalaries :many
-- Everyone's current salary (latest effective on or before the given day), active people only.
SELECT DISTINCT ON (s.user_id)
       s.user_id, u.name AS user_name, coalesce(d.name::text, '') AS department_name, e.designation,
       s.amount::text AS amount, s.currency, s.effective_from::text AS effective_from
FROM salaries s
JOIN users u ON u.id = s.user_id AND u.is_active
JOIN employees e ON e.user_id = s.user_id
LEFT JOIN departments d ON d.id = e.department_id
WHERE s.effective_from <= sqlc.arg('on_day')::text::date
ORDER BY s.user_id, s.effective_from DESC;

-- name: CreateSalary :one
INSERT INTO salaries (user_id, amount, currency, effective_from, note, created_by)
VALUES (sqlc.arg('user_id'), sqlc.arg('amount')::text::numeric, sqlc.arg('currency'),
        sqlc.arg('effective_from')::text::date, sqlc.arg('note'), sqlc.arg('created_by'))
RETURNING id;

-- name: GetSalary :one
SELECT id, user_id, effective_from::text AS effective_from FROM salaries WHERE id = $1;

-- name: DeleteSalary :exec
DELETE FROM salaries WHERE id = $1;
