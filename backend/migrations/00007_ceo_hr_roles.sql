-- +goose Up
-- +goose StatementBegin
-- Management becomes the CEO (one per organisation) and HR becomes a role of its own.
DO $$
BEGIN
    IF (SELECT count(*) FROM users WHERE role = 'management' AND is_active) > 1 THEN
        RAISE EXCEPTION 'More than one active Management user: deactivate all but the CEO, or change their role, before migrating';
    END IF;
END $$;

INSERT INTO roles (code, name) VALUES ('ceo', 'CEO'), ('hr', 'HR');
UPDATE users SET role = 'ceo', updated_at = now() WHERE role = 'management';
DELETE FROM roles WHERE code = 'management';

-- Only one active CEO; handing over is done with `opsctl transfer-ceo`.
CREATE UNIQUE INDEX users_one_ceo_idx ON users (role) WHERE role = 'ceo' AND is_active;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX users_one_ceo_idx;
INSERT INTO roles (code, name) VALUES ('management', 'Management');
UPDATE users SET role = 'management' WHERE role = 'ceo';
UPDATE users SET role = 'member' WHERE role = 'hr';
DELETE FROM roles WHERE code IN ('ceo', 'hr');
-- +goose StatementEnd
