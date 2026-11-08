-- +goose Up
-- Days before this are "not tracked" rather than absent (attendance started with the HRM release).
INSERT INTO settings (key, value)
VALUES ('attendance_start', to_jsonb(to_char(now() AT TIME ZONE 'Asia/Dhaka', 'YYYY-MM-DD')))
ON CONFLICT (key) DO NOTHING;

-- +goose Down
DELETE FROM settings WHERE key = 'attendance_start';
