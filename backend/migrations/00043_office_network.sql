-- +goose Up
-- +goose StatementBegin
-- Office network check for attendance: HR and the CEO list the office's public IPs
-- (addresses or CIDR ranges). Check-ins and outs record whether they came from one;
-- when switched on, others are refused. Off and empty until HR sets it.
INSERT INTO settings (key, value) VALUES
    ('office_network', '{"enabled": false, "ips": []}')
ON CONFLICT (key) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM settings WHERE key = 'office_network';
-- +goose StatementEnd
