-- +goose Up
-- +goose StatementBegin
-- Office location check for attendance: when HR switches it on, check-in and check-out
-- are accepted only within radius_m metres of the office (lat/lng). Off until HR sets it.
INSERT INTO settings (key, value) VALUES
    ('office_location', '{"enabled": false, "lat": null, "lng": null, "radius_m": 200}')
ON CONFLICT (key) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM settings WHERE key = 'office_location';
-- +goose StatementEnd
